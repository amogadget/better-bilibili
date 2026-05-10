package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"

	"bili-web-vps/internal/bili"
	"bili-web-vps/internal/config"
	"bili-web-vps/internal/hls"
)

type Server struct {
	cfg  *config.Config
	bili *bili.Client
	tpl  *template.Template
	root string // path to web/ directory
	hls  *hls.Manager
}

func New(cfg *config.Config, webRoot, cacheRoot string) (*Server, error) {
	tpl, err := template.ParseGlob(filepath.Join(webRoot, "templates", "*.html"))
	if err != nil {
		return nil, err
	}
	hlsRoot := filepath.Join(cacheRoot, "hls")
	hlsMgr, err := hls.New(hlsRoot)
	if err != nil {
		return nil, err
	}
	return &Server{
		cfg:  cfg,
		bili: bili.New(cfg),
		tpl:  tpl,
		root: webRoot,
		hls:  hlsMgr,
	}, nil
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("GET /login/start", s.handleLoginStart)
	mux.HandleFunc("GET /login/poll", s.handleLoginPoll)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.HandleFunc("GET /search", s.handleSearch)
	mux.HandleFunc("GET /img", s.handleImageProxy)
	mux.HandleFunc("GET /watch/{bvid}", s.handleWatch)
	mux.HandleFunc("GET /stream/{bvid}", s.handleStream)
	mux.HandleFunc("GET /stream/audio/{bvid}", s.handleStreamAudio)
	mux.HandleFunc("GET /hls/{bvid}/{file}", s.handleHLS)
	mux.HandleFunc("GET /danmaku/{bvid}", s.handleDanmaku)
	mux.HandleFunc("GET /subscriptions", s.handleSubscriptions)
	mux.HandleFunc("GET /favorites", s.handleFavorites)
	mux.HandleFunc("GET /favorites/{fid}", s.handleFavoriteFolder)
	mux.HandleFunc("GET /watch-later", s.handleWatchLater)
	mux.HandleFunc("GET /history", s.handleHistory)
	mux.HandleFunc("POST /heartbeat", s.handleHeartbeat)
	mux.HandleFunc("GET /", s.handleHome)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir(filepath.Join(s.root, "static")))))
	return mux
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if s.cfg.LoggedIn() {
		w.Write([]byte("ok (logged in)\n"))
	} else {
		w.Write([]byte("ok (not logged in)\n"))
	}
}

type listView struct {
	tplBase
	PageTitle string
	EmptyMsg  string
	Error     string
	Cards     []CardView
	Next      *NextLink
}

func (s *Server) renderList(w http.ResponseWriter, v listView) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, "list.html", v); err != nil {
		log.Printf("list template: %v", err)
	}
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if !s.cfg.LoggedIn() {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	freshIdx, _ := strconv.Atoi(r.URL.Query().Get("fresh"))
	if freshIdx < 1 {
		freshIdx = 1
	}
	page, err := s.bili.RecommendVideos(r.Context(), freshIdx)
	v := listView{
		tplBase:   tplBase{Section: "home"},
		PageTitle: "Recommended",
		EmptyMsg:  "No recommendations right now.",
	}
	if err != nil {
		v.Error = err.Error()
	} else {
		for _, it := range page.Items {
			v.Cards = append(v.Cards, cardFromRecommend(it))
		}
		if len(page.Items) > 0 {
			v.Next = &NextLink{URL: fmt.Sprintf("/?fresh=%d", freshIdx+1)}
		}
	}
	s.renderList(w, v)
}

func (s *Server) handleSubscriptions(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.LoggedIn() {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	offset := r.URL.Query().Get("offset")
	page, err := s.bili.VideoFeed(r.Context(), offset)
	v := listView{
		tplBase:   tplBase{Section: "subscriptions"},
		PageTitle: "Subscriptions",
		EmptyMsg:  "Nothing in your subscription feed.",
	}
	if err != nil {
		v.Error = err.Error()
	} else {
		for _, it := range page.Items {
			v.Cards = append(v.Cards, cardFromFeed(it))
		}
		if page.HasMore && page.Offset != "" {
			v.Next = &NextLink{URL: "/subscriptions?offset=" + url.QueryEscape(page.Offset)}
		}
	}
	s.renderList(w, v)
}

func (s *Server) handleFavorites(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.LoggedIn() {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	mid := s.cfg.GetCookies().DedeUserID
	folders, err := s.bili.FavoriteFolders(r.Context(), mid)
	data := struct {
		tplBase
		Folders []bili.FavFolder
		Error   string
	}{tplBase: tplBase{Section: "favorites"}, Folders: folders}
	if err != nil {
		data.Error = err.Error()
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, "folders.html", data); err != nil {
		log.Printf("folders template: %v", err)
	}
}

func (s *Server) handleFavoriteFolder(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.LoggedIn() {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	fid, err := strconv.ParseInt(r.PathValue("fid"), 10, 64)
	if err != nil {
		http.Error(w, "bad fid", http.StatusBadRequest)
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	mp, err := s.bili.FavoriteMedia(r.Context(), fid, page)
	v := listView{
		tplBase:   tplBase{Section: "favorites"},
		PageTitle: "Favorites",
		EmptyMsg:  "This folder is empty.",
	}
	if err != nil {
		v.Error = err.Error()
	} else {
		for _, m := range mp.Items {
			v.Cards = append(v.Cards, cardFromFavMedia(m))
		}
		if mp.HasMore {
			v.Next = &NextLink{URL: fmt.Sprintf("/favorites/%d?page=%d", fid, page+1)}
		}
	}
	s.renderList(w, v)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.LoggedIn() {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	var cursor bili.HistoryCursor
	if v := r.URL.Query().Get("max"); v != "" {
		cursor.Max, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := r.URL.Query().Get("view_at"); v != "" {
		cursor.ViewAt, _ = strconv.ParseInt(v, 10, 64)
	}
	page, err := s.bili.WatchHistory(r.Context(), cursor)
	v := listView{
		tplBase:   tplBase{Section: "history"},
		PageTitle: "History",
		EmptyMsg:  "No watch history yet.",
	}
	if err != nil {
		v.Error = err.Error()
	} else {
		for _, it := range page.Items {
			v.Cards = append(v.Cards, cardFromHistory(it))
		}
		if page.HasMore && page.Next.Max > 0 {
			v.Next = &NextLink{URL: fmt.Sprintf("/history?max=%d&view_at=%d", page.Next.Max, page.Next.ViewAt)}
		}
	}
	s.renderList(w, v)
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.LoggedIn() {
		http.Error(w, "not logged in", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	bvid := r.PostForm.Get("bvid")
	if !validBVID(bvid) {
		http.Error(w, "bad bvid", http.StatusBadRequest)
		return
	}
	aid, err := strconv.ParseInt(r.PostForm.Get("aid"), 10, 64)
	if err != nil || aid <= 0 {
		http.Error(w, "bad aid", http.StatusBadRequest)
		return
	}
	cid, err := strconv.ParseInt(r.PostForm.Get("cid"), 10, 64)
	if err != nil || cid <= 0 {
		http.Error(w, "bad cid", http.StatusBadRequest)
		return
	}
	t, err := strconv.Atoi(r.PostForm.Get("t"))
	if err != nil || t < 0 {
		http.Error(w, "bad t", http.StatusBadRequest)
		return
	}
	if err := s.bili.ReportProgress(r.Context(), aid, cid, bvid, t); err != nil {
		log.Printf("heartbeat %s t=%d: %v", bvid, t, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleWatchLater(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.LoggedIn() {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	items, err := s.bili.WatchLater(r.Context())
	v := listView{
		tplBase:   tplBase{Section: "watch-later"},
		PageTitle: "Watch later",
		EmptyMsg:  "Your watch-later queue is empty.",
	}
	if err != nil {
		v.Error = err.Error()
	} else {
		for _, it := range items {
			v.Cards = append(v.Cards, cardFromWatchLater(it))
		}
	}
	s.renderList(w, v)
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if s.cfg.LoggedIn() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, "login.html", nil); err != nil {
		log.Printf("login template: %v", err)
	}
}

func (s *Server) handleLoginStart(w http.ResponseWriter, r *http.Request) {
	gen, err := s.bili.QRGenerate(r.Context())
	if err != nil {
		http.Error(w, "qr generate: "+err.Error(), http.StatusBadGateway)
		return
	}
	png, err := qrcode.Encode(gen.URL, qrcode.Medium, 320)
	if err != nil {
		http.Error(w, "qr encode: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"key": gen.Key,
		"qr":  "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
	})
}

func (s *Server) handleLoginPoll(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "missing key", http.StatusBadRequest)
		return
	}
	st, err := s.bili.QRPoll(r.Context(), key)
	if err != nil {
		http.Error(w, "poll: "+err.Error(), http.StatusBadGateway)
		return
	}
	resp := map[string]any{"code": st.Code, "message": st.Message}
	if st.Cookies != nil {
		if err := s.cfg.SetCookies(*st.Cookies); err != nil {
			log.Printf("save cookies: %v", err)
			http.Error(w, "save cookies: "+err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("login successful (uid=%s)", st.Cookies.DedeUserID)
	}
	writeJSON(w, resp)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.cfg.SetCookies(config.Cookies{}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.LoggedIn() {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	data := struct {
		tplBase
		Cards []CardView
		Error string
		Next  *NextLink
	}{tplBase: tplBase{Query: q}}

	if q != "" {
		results, err := s.bili.SearchVideos(r.Context(), q, page)
		if err != nil {
			data.Error = err.Error()
		} else {
			for _, r := range results {
				data.Cards = append(data.Cards, cardFromSearch(r))
			}
			if len(results) >= 20 {
				data.Next = &NextLink{URL: fmt.Sprintf("/search?q=%s&page=%d", url.QueryEscape(q), page+1)}
			}
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, "search.html", data); err != nil {
		log.Printf("search template: %v", err)
	}
}

// handleImageProxy fetches a bilibili-hosted image with the right Referer/UA
// and streams it back to the browser. The host is allowlisted to prevent SSRF.
func (s *Server) handleImageProxy(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("u")
	if raw == "" {
		http.Error(w, "missing u", http.StatusBadRequest)
		return
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		http.Error(w, "bad url", http.StatusBadRequest)
		return
	}
	if !isBiliHost(u.Host) {
		http.Error(w, "host not allowed", http.StatusForbidden)
		return
	}
	req, err := s.bili.NewRequest(r.Context(), "GET", u.String(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp, err := s.bili.HTTP.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// defaultQuality requested when calling playurl. 64 = 720p (per bilibili docs).
const defaultQuality = 64

func (s *Server) handleWatch(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.LoggedIn() {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	bvid := r.PathValue("bvid")
	if !validBVID(bvid) {
		http.Error(w, "bad bvid", http.StatusBadRequest)
		return
	}

	info, err := s.bili.VideoInfo(r.Context(), bvid)
	if err != nil {
		http.Error(w, "video info: "+err.Error(), http.StatusBadGateway)
		return
	}

	selectedCID := info.CID
	if c := r.URL.Query().Get("cid"); c != "" {
		if n, err := strconv.ParseInt(c, 10, 64); err == nil {
			selectedCID = n
		}
	}

	stream, err := s.bili.PlayURLMP4(r.Context(), bvid, selectedCID, defaultQuality)
	if err != nil {
		http.Error(w, "playurl: "+err.Error(), http.StatusBadGateway)
		return
	}

	// Single-part videos stream as plain mp4 for instant playback. Multi-part
	// videos route through the ffmpeg HLS pipeline so they play seamlessly as
	// one file in iOS Safari. ?hls=1 forces the HLS path for debugging.
	streamSrc := fmt.Sprintf("/stream/%s?cid=%d", bvid, selectedCID)
	useHLS := stream.Parts > 1 || r.URL.Query().Get("hls") == "1"
	if useHLS {
		streamSrc = fmt.Sprintf("/hls/%s/playlist.m3u8?cid=%d", bvid, selectedCID)
	}

	comments, err := s.bili.VideoComments(r.Context(), info.AID)
	if err != nil {
		log.Printf("comments fetch %s: %v", bvid, err)
	}
	commentViews := make([]CommentView, 0, len(comments))
	for _, c := range comments {
		commentViews = append(commentViews, commentToView(c))
	}

	// Pull bilibili's server-side last-played position so the player can
	// resume silently. Suppress the resume if the previous session was
	// effectively complete (>95% watched).
	resumeAt, _ := s.bili.VideoProgress(r.Context(), info.AID, selectedCID)
	if info.Duration > 0 && resumeAt > 0 {
		if float64(resumeAt)/float64(info.Duration) > 0.95 {
			resumeAt = 0
		}
	}

	data := struct {
		tplBase
		Video       *bili.VideoInfo
		Stream      *bili.MP4Stream
		SelectedCID int64
		StreamSrc   string
		UseHLS      bool
		Comments    []CommentView
		ResumeAt    int
	}{
		Video:       info,
		Stream:      stream,
		SelectedCID: selectedCID,
		StreamSrc:   streamSrc,
		UseHLS:      useHLS,
		Comments:    commentViews,
		ResumeAt:    resumeAt,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, "watch.html", data); err != nil {
		log.Printf("watch template: %v", err)
	}
}

// handleStream resolves the upstream mp4 URL for the requested bvid+cid and
// proxies the bytes back to the client, forwarding Range requests so the native
// <video> element can seek and resume.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.LoggedIn() {
		http.Error(w, "not logged in", http.StatusUnauthorized)
		return
	}
	bvid := r.PathValue("bvid")
	if !validBVID(bvid) {
		http.Error(w, "bad bvid", http.StatusBadRequest)
		return
	}

	cidStr := r.URL.Query().Get("cid")
	var cid int64
	if cidStr == "" {
		info, err := s.bili.VideoInfo(r.Context(), bvid)
		if err != nil {
			http.Error(w, "video info: "+err.Error(), http.StatusBadGateway)
			return
		}
		cid = info.CID
	} else {
		n, err := strconv.ParseInt(cidStr, 10, 64)
		if err != nil {
			http.Error(w, "bad cid", http.StatusBadRequest)
			return
		}
		cid = n
	}

	stream, err := s.bili.PlayURLMP4(r.Context(), bvid, cid, defaultQuality)
	if err != nil {
		http.Error(w, "playurl: "+err.Error(), http.StatusBadGateway)
		return
	}

	req, err := s.bili.NewRequest(r.Context(), "GET", stream.URL, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if rng := r.Header.Get("Range"); rng != "" {
		req.Header.Set("Range", rng)
	}

	resp, err := s.bili.HTTPStream.Do(req)
	if err != nil {
		http.Error(w, "upstream: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	if w.Header().Get("Accept-Ranges") == "" {
		w.Header().Set("Accept-Ranges", "bytes")
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (s *Server) handleDanmaku(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.LoggedIn() {
		http.Error(w, "not logged in", http.StatusUnauthorized)
		return
	}
	bvid := r.PathValue("bvid")
	if !validBVID(bvid) {
		http.Error(w, "bad bvid", http.StatusBadRequest)
		return
	}
	cid, err := strconv.ParseInt(r.URL.Query().Get("cid"), 10, 64)
	if err != nil {
		http.Error(w, "bad cid", http.StatusBadRequest)
		return
	}
	items, err := s.bili.FetchDanmaku(r.Context(), cid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	json.NewEncoder(w).Encode(items)
}

// handleStreamAudio proxies the audio-only DASH track for a bvid+cid. Used
// by the iOS native shell so its background AVPlayer fetches just audio
// (~100 Kbps) instead of the combined audio+video stream the web <video>
// uses (~3 Mbps).
func (s *Server) handleStreamAudio(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.LoggedIn() {
		http.Error(w, "not logged in", http.StatusUnauthorized)
		return
	}
	bvid := r.PathValue("bvid")
	if !validBVID(bvid) {
		http.Error(w, "bad bvid", http.StatusBadRequest)
		return
	}

	cidStr := r.URL.Query().Get("cid")
	var cid int64
	if cidStr == "" {
		info, err := s.bili.VideoInfo(r.Context(), bvid)
		if err != nil {
			http.Error(w, "video info: "+err.Error(), http.StatusBadGateway)
			return
		}
		cid = info.CID
	} else {
		n, err := strconv.ParseInt(cidStr, 10, 64)
		if err != nil {
			http.Error(w, "bad cid", http.StatusBadRequest)
			return
		}
		cid = n
	}

	dash, err := s.bili.PlayURLDASH(r.Context(), bvid, cid, defaultQuality)
	if err != nil {
		http.Error(w, "playurl dash: "+err.Error(), http.StatusBadGateway)
		return
	}

	req, err := s.bili.NewRequest(r.Context(), "GET", dash.AudioURL, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if rng := r.Header.Get("Range"); rng != "" {
		req.Header.Set("Range", rng)
	}
	resp, err := s.bili.HTTPStream.Do(req)
	if err != nil {
		http.Error(w, "upstream: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	if w.Header().Get("Accept-Ranges") == "" {
		w.Header().Set("Accept-Ranges", "bytes")
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "audio/mp4")
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// handleHLS serves the HLS playlist + fmp4 segments produced by the ffmpeg
// session manager. Requesting playlist.m3u8 lazily starts (or replaces) the
// session for this (bvid, cid).
func (s *Server) handleHLS(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.LoggedIn() {
		http.Error(w, "not logged in", http.StatusUnauthorized)
		return
	}
	bvid := r.PathValue("bvid")
	if !validBVID(bvid) {
		http.Error(w, "bad bvid", http.StatusBadRequest)
		return
	}
	file := r.PathValue("file")
	if !validHLSFile(file) {
		http.Error(w, "bad file", http.StatusBadRequest)
		return
	}

	cidStr := r.URL.Query().Get("cid")
	cid, err := strconv.ParseInt(cidStr, 10, 64)
	if err != nil {
		http.Error(w, "bad cid", http.StatusBadRequest)
		return
	}
	key := hls.KeyOf(bvid, cid)

	sess := s.hls.Get(key)
	if sess == nil {
		// Only the playlist request kicks off ffmpeg. Segment requests for an
		// unknown key are 404 — they'd be stale URLs from a previous video.
		if file != hls.PlaylistName {
			http.NotFound(w, r)
			return
		}
		ds, err := s.bili.PlayURLDASH(r.Context(), bvid, cid, defaultQuality)
		if err != nil {
			http.Error(w, "playurl dash: "+err.Error(), http.StatusBadGateway)
			return
		}
		sess, err = s.hls.Start(r.Context(), key, ds.VideoURL, ds.AudioURL)
		if err != nil {
			http.Error(w, "ffmpeg start: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if file == hls.PlaylistName {
		// Wait for ffmpeg to write the first segment so we can serve a non-empty playlist.
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		if err := sess.WaitReady(ctx); err != nil {
			http.Error(w, "playlist not ready: "+err.Error(), http.StatusGatewayTimeout)
			return
		}
	} else {
		// Briefly poll for the segment file in case ffmpeg hasn't written it yet.
		if err := waitForFile(r.Context(), filepath.Join(sess.Dir, file), 8*time.Second); err != nil {
			http.NotFound(w, r)
			return
		}
	}

	full := filepath.Join(sess.Dir, file)
	switch {
	case strings.HasSuffix(file, ".m3u8"):
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store")
	case strings.HasSuffix(file, ".m4s"), strings.HasSuffix(file, ".mp4"):
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	http.ServeFile(w, r, full)
}

func validHLSFile(s string) bool {
	if s == hls.PlaylistName || s == "init.mp4" {
		return true
	}
	if !strings.HasPrefix(s, "seg") || !strings.HasSuffix(s, ".m4s") {
		return false
	}
	for _, c := range s[3 : len(s)-4] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func waitForFile(ctx context.Context, path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("client gone")
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("file not found: %s", path)
}

// validBVID guards path parameter input against funky values before we use it
// in upstream URLs. Real bilibili BVIDs are "BV" + 10 alphanumerics.
func validBVID(s string) bool {
	if len(s) < 3 || len(s) > 32 {
		return false
	}
	if !strings.HasPrefix(s, "BV") && !strings.HasPrefix(s, "bv") {
		return false
	}
	for _, c := range s[2:] {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}

func isBiliHost(host string) bool {
	host = strings.ToLower(host)
	if i := strings.Index(host, ":"); i >= 0 {
		host = host[:i]
	}
	for _, suffix := range []string{".hdslb.com", ".bilibili.com", ".biliapi.net", ".biliapi.com", ".biligame.com"} {
		if strings.HasSuffix(host, suffix) || host == suffix[1:] {
			return true
		}
	}
	return false
}
