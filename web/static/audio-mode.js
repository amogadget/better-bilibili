// YouTube-style background audio.
//
// iOS Safari pauses any <video> when the screen locks. <audio>, however,
// keeps playing. We run a hidden <audio> element in lockstep with the
// visible <video>: muted while the page is foreground, unmuted the instant
// the page hides. The user perceives a single seamless playback that just
// keeps going when they lock the phone — no toggle, no gap.
//
// Tradeoff: bandwidth roughly doubles while the page is foreground because
// the same source URL is fetched into two media elements. Browser HTTP
// cache may dedupe overlapping range requests, which mitigates this in
// practice. Bandwidth normalizes back to single-stream the moment the page
// hides (video stays paused; only audio runs).
(function () {
    // When running inside the iOS native shell, native-bridge.js + AVPlayer
    // own background audio. The dual-<audio> hack below would just fight
    // them, so stand down.
    if (window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.player) {
        return;
    }

    const video = document.getElementById('player-video');
    const audio = document.getElementById('player-audio');
    if (!video || !audio) return;

    audio.muted = true;
    audio.preload = 'none';

    let primed = false;
    let suppressVideoPauseSync = false;

    function prime() {
        if (primed) return;
        audio.src = video.src;
        audio.load();
        primed = true;
    }

    function applyMediaSession() {
        if (!('mediaSession' in navigator)) return;
        try {
            navigator.mediaSession.metadata = new MediaMetadata({
                title:  audio.dataset.title  || '',
                artist: audio.dataset.author || '',
                artwork: audio.dataset.poster ? [{
                    src: audio.dataset.poster,
                    sizes: '512x512',
                    type: 'image/jpeg',
                }] : [],
            });
        } catch (e) { /* MediaMetadata constructor unsupported */ }

        const safe = (fn) => (e) => { try { fn(e); } catch (err) { /* swallow */ } };
        navigator.mediaSession.setActionHandler('play',  safe(() => {
            audio.play();
            if (!document.hidden) video.play();
        }));
        navigator.mediaSession.setActionHandler('pause', safe(() => {
            audio.pause();
            if (!document.hidden) video.pause();
        }));
        navigator.mediaSession.setActionHandler('seekto', safe((e) => {
            const t = e.seekTime;
            audio.currentTime = t;
            if (!document.hidden) video.currentTime = t;
        }));
    }

    // When the user starts playback on the video element, mirror it into the
    // hidden audio element so both are running by the time the screen locks.
    video.addEventListener('play', () => {
        prime();
        try { audio.currentTime = video.currentTime; } catch (e) {}
        audio.play().catch(() => { /* may need a fresh gesture; not fatal */ });
        applyMediaSession();
    });

    // Pausing the video pauses the audio — UNLESS the pause is iOS auto-pausing
    // because the screen just locked. In that case we want audio to keep going.
    video.addEventListener('pause', () => {
        if (suppressVideoPauseSync) return;
        if (document.hidden) return;
        audio.pause();
    });

    video.addEventListener('seeked', () => {
        if (primed) try { audio.currentTime = video.currentTime; } catch (e) {}
    });

    // Periodic drift correction. Two independent media elements playing the
    // same source can drift; keep them within ~0.5s.
    video.addEventListener('timeupdate', () => {
        if (!primed || audio.paused) return;
        if (Math.abs(audio.currentTime - video.currentTime) > 0.5) {
            try { audio.currentTime = video.currentTime; } catch (e) {}
        }
    });

    document.addEventListener('visibilitychange', () => {
        if (document.hidden) {
            // Page going background. iOS will pause the video; suppress the
            // pause→audio.pause() handler so audio keeps playing.
            suppressVideoPauseSync = true;
            audio.muted = false;
        } else {
            // Coming back. Resync video to wherever audio reached, mute audio
            // again, and resume video if the user hadn't paused on the lock screen.
            suppressVideoPauseSync = false;
            audio.muted = true;
            if (primed) {
                try { video.currentTime = audio.currentTime; } catch (e) {}
                if (!audio.paused) {
                    video.play().catch(() => {});
                }
            }
        }
    });
})();
