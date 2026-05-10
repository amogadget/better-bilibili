// native-bridge.js
//
// When running inside the iOS native shell (BiliWeb.app, which exposes
// `window.webkit.messageHandlers.player`), keep the host app informed of
// the current playback state. The host transparently swaps in an AVPlayer
// when the app backgrounds so audio keeps going while iOS suspends the
// WebContent process — this script is what lets it know the URL, position,
// and metadata to take over with.
//
// In Safari (no message handler) this file does nothing; audio-mode.js
// handles the dual-element fallback there.
(function () {
    const native = window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.player;
    if (!native) return;

    const video = document.getElementById('player-video');
    if (!video) return;

    function readMeta() {
        const titleEl  = document.querySelector('.watch-title');
        const bylineEl = document.querySelector('.byline');
        const audioEl  = document.getElementById('player-audio');
        const poster   = (audioEl && audioEl.dataset && audioEl.dataset.poster) || video.poster || '';
        return {
            title:  (titleEl  && titleEl.textContent.trim())  || document.title || '',
            artist: (bylineEl && bylineEl.textContent.trim()) || '',
            artwork: poster ? new URL(poster, location.href).href : '',
        };
    }

    function pushState() {
        if (!video.src) return;
        const meta = readMeta();
        try {
            native.postMessage({
                action: 'state',
                src: new URL(video.src, location.href).href,
                currentTime: video.currentTime,
                duration: isFinite(video.duration) ? video.duration : 0,
                playing: !video.paused,
                title:   meta.title,
                artist:  meta.artist,
                artwork: meta.artwork,
            });
        } catch (e) { /* postMessage can throw on serialization edge cases */ }
    }

    video.addEventListener('play',           pushState);
    video.addEventListener('pause',          pushState);
    video.addEventListener('seeked',         pushState);
    video.addEventListener('loadedmetadata', pushState);
    video.addEventListener('ratechange',     pushState);

    // Periodic refresh while playing so currentTime/positions on the lock screen
    // aren't stale by the time the app backgrounds.
    setInterval(() => { if (!video.paused) pushState(); }, 2000);

    // Native pauses its AVPlayer on foreground transition and tells us where
    // it left off so the on-screen <video> can pick up at exactly that frame.
    window.__biliResume = function (t) {
        try { video.currentTime = t; } catch (e) { /* ignore unseekable state */ }
        video.play().catch(() => { /* may need user tap; harmless */ });
    };

    // Initial sync as soon as metadata is ready (covers the foreground "scan
    // and chill" case before the user actually plays).
    if (video.readyState >= 1) pushState();
})();
