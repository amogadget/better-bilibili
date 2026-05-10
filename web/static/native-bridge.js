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

    // In the iOS shell, native AVPlayer is the audio source. The visible
    // <video> element is for pixels only — mute it so we don't get two
    // simultaneous audio streams. The native player keeps producing audio
    // through screen-lock and app-switch transitions; the web video just
    // shows frames while the app is in foreground.
    video.muted = true;
    // If the user toggles unmute on the native controls, re-mute on the
    // next event so we never play both at once.
    video.addEventListener('volumechange', () => {
        if (!video.muted) video.muted = true;
    });

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

    // Use the same combined stream URL the web <video> uses. The audio-only
    // DASH track from bilibili is a bare fragmented mp4 (no manifest) and
    // AVPlayer doesn't reliably play it as a standalone URL — it expects a
    // progressive mp4 or HLS playlist. Using the combined stream means the
    // native player has the same well-formed mp4/HLS to chew on.
    //
    // (data-audio-src is left on the element for future use once we have a
    // server-side HLS audio playlist or progressive-mp4 remux.)
    function audioSourceURL() {
        if (video.src) return new URL(video.src, location.href).href;
        return null;
    }

    function pushState() {
        const src = audioSourceURL();
        if (!src) return;
        const meta = readMeta();
        try {
            native.postMessage({
                action: 'state',
                src: src,
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
