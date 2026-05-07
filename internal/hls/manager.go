// Package hls runs ffmpeg as a long-lived process that ingests bilibili's
// DASH video+audio URLs and writes a fragmented-mp4 HLS bundle to disk. iOS
// Safari plays the resulting playlist natively in <video>.
package hls

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const (
	UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	Referer   = "https://www.bilibili.com"

	// PlaylistName is the m3u8 filename ffmpeg writes inside each session dir.
	PlaylistName = "playlist.m3u8"
)

// Session is one running (or finished) ffmpeg job for a (bvid, cid) pair.
type Session struct {
	Key     string // "bvid:cid"
	Dir     string // absolute directory containing playlist.m3u8 + segments
	cmd     *exec.Cmd
	started time.Time
	ready   chan struct{} // closed when the m3u8 file first appears

	mu       sync.Mutex
	finished bool
	err      error
}

// Manager keeps at most one active session at a time. Starting a new one
// replaces the previous (kills its ffmpeg and removes its files).
//
// This is sized for a single-user system. Don't reuse for multi-tenant.
type Manager struct {
	root string

	mu     sync.Mutex
	active *Session
}

func New(root string) (*Manager, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &Manager{root: root}, nil
}

// Get returns the active session if its key matches, nil otherwise.
func (m *Manager) Get(key string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != nil && m.active.Key == key {
		return m.active
	}
	return nil
}

// Start launches a new ffmpeg session for the given (bvid, cid). If a session
// for the same key is already active it is returned as-is.
func (m *Manager) Start(ctx context.Context, key, videoURL, audioURL string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.active != nil && m.active.Key == key {
		return m.active, nil
	}
	if m.active != nil {
		m.active.stop()
		_ = os.RemoveAll(m.active.Dir)
	}

	dir := filepath.Join(m.root, sanitize(key))
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	args := []string{
		"-loglevel", "warning",
		"-nostdin",
		"-user_agent", UserAgent,
		"-referer", Referer,
		"-i", videoURL,
		"-user_agent", UserAgent,
		"-referer", Referer,
		"-i", audioURL,
		"-map", "0:v:0",
		"-map", "1:a:0",
		"-c", "copy",
		"-f", "hls",
		"-hls_time", "6",
		"-hls_list_size", "0",
		"-hls_playlist_type", "event",
		"-hls_segment_type", "fmp4",
		"-hls_fmp4_init_filename", "init.mp4",
		"-hls_segment_filename", filepath.Join(dir, "seg%05d.m4s"),
		filepath.Join(dir, PlaylistName),
	}
	cmd := exec.Command("ffmpeg", args...)
	cmd.Stderr = newPrefixWriter("ffmpeg["+key+"] ")
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	sess := &Session{
		Key:     key,
		Dir:     dir,
		cmd:     cmd,
		started: time.Now(),
		ready:   make(chan struct{}),
	}
	m.active = sess

	go sess.watchReady()
	go sess.waitExit()

	return sess, nil
}

// WaitReady blocks until the playlist file has been written for the first time
// or the context is cancelled / ffmpeg exits early.
func (s *Session) WaitReady(ctx context.Context) error {
	select {
	case <-s.ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Err returns ffmpeg's exit error if it has finished with one. nil otherwise.
func (s *Session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.finished {
		return nil
	}
	return s.err
}

func (s *Session) watchReady() {
	playlist := filepath.Join(s.Dir, PlaylistName)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(playlist); err == nil {
			close(s.ready)
			return
		}
		s.mu.Lock()
		done := s.finished
		s.mu.Unlock()
		if done {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	// If we never saw the playlist, close ready anyway so callers don't hang
	// forever; they'll get a clear file-not-found when they try to read.
	close(s.ready)
}

func (s *Session) waitExit() {
	err := s.cmd.Wait()
	s.mu.Lock()
	s.finished = true
	if err != nil && !errors.Is(err, context.Canceled) {
		s.err = err
	}
	s.mu.Unlock()
}

func (s *Session) stop() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	_ = s.cmd.Process.Kill()
}

// Close kills any active session and clears its cache dir. Intended for
// shutdown; not currently wired in but kept so the API is symmetric.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != nil {
		m.active.stop()
		_ = os.RemoveAll(m.active.Dir)
		m.active = nil
	}
}

// sanitize maps an arbitrary key to a filesystem-safe directory name.
func sanitize(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

// newPrefixWriter pipes ffmpeg stderr into the standard logger with a tag.
type prefixWriter struct{ prefix string }

func newPrefixWriter(p string) *prefixWriter { return &prefixWriter{prefix: p} }

func (w *prefixWriter) Write(b []byte) (int, error) {
	log.Print(w.prefix + string(b))
	return len(b), nil
}

// keyOf is the canonical key used by callers so server + manager agree.
func KeyOf(bvid string, cid int64) string {
	return fmt.Sprintf("%s:%d", bvid, cid)
}
