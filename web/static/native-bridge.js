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
    // Visible build marker so you can confirm in the Safari/WKWebView console
    // that a fresh copy of this file actually reached the device. If you
    // edit native-bridge.js and don't see the new tag on next page load,
    // WKWebView is serving a cached copy.
    const BUILD_TAG = 'native-bridge.js 2026-05-11/attempt5';
    console.log('[BiliWeb] ' + BUILD_TAG);

    const native = window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.player;
    if (!native) return;

    const video = document.getElementById('player-video');
    if (!video) return;

    // Track when the user last interacted with the page. iOS's
    // swipe-up-to-home is a system gesture that starts in the home-indicator
    // strip below our safe area — it does NOT fire JS input events. So a
    // `pause` event arriving without a recent gesture in this window is iOS
    // auto-pausing the <video>, not the user tapping pause. Attempts 1–4
    // tried to detect this *after* the pause arrived (via visibility,
    // applicationState, resigningActive) — Xcode logs from 2026-05-11 proved
    // all of those signals arrive AFTER the pause event has already been
    // pushed to Swift and processed. The only signal that beats the pause
    // is the absence of a preceding gesture.
    let lastUserGesture = 0;
    const GESTURE_WINDOW_MS = 500;
    // touchmove is included so a long scrub-bar drag keeps refreshing
    // the gesture timestamp (otherwise the pause that some browsers fire
    // mid-scrub would look involuntary after 500ms of dragging).
    ['touchstart', 'touchmove', 'mousedown', 'pointermove', 'click', 'keydown'].forEach((ev) => {
        document.addEventListener(ev, () => { lastUserGesture = Date.now(); }, true);
    });

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

    function pushState(reason) {
        const playing = !video.paused;
        const vis = document.visibilityState;
        const gestureAge = Date.now() - lastUserGesture;
        const userInitiated = gestureAge < GESTURE_WINDOW_MS;
        console.log('[BiliWeb] pushState reason=' + reason + ' playing=' + playing + ' vis=' + vis + ' t=' + video.currentTime.toFixed(2) + ' gestureAge=' + gestureAge + ' userInitiated=' + userInitiated);

        // When the page is hidden (app backgrounded, screen locked), iOS
        // auto-pauses the <video> — that's noise, not a user action. The
        // native AVPlayer keeps running independently.
        if (vis !== 'visible') {
            console.log('[BiliWeb] pushState blocked by visibility guard');
            return;
        }

        const src = audioSourceURL();
        if (!src) return;
        const meta = readMeta();
        try {
            native.postMessage({
                action: 'state',
                src: src,
                currentTime: video.currentTime,
                duration: isFinite(video.duration) ? video.duration : 0,
                playing: playing,
                userInitiated: userInitiated,
                title:   meta.title,
                artist:  meta.artist,
                artwork: meta.artwork,
            });
        } catch (e) { /* postMessage can throw on serialization edge cases */ }
    }

    video.addEventListener('play',           () => pushState('play'));
    video.addEventListener('pause',          () => pushState('pause'));
    video.addEventListener('seeked',         () => pushState('seeked'));
    video.addEventListener('loadedmetadata', () => pushState('loadedmetadata'));
    video.addEventListener('ratechange',     () => pushState('ratechange'));

    document.addEventListener('visibilitychange', () => {
        console.log('[BiliWeb] visibilitychange -> ' + document.visibilityState + ' playing=' + !video.paused);
    });

    // Periodic refresh while playing so currentTime/positions on the lock screen
    // aren't stale by the time the app backgrounds.
    setInterval(() => { if (!video.paused) pushState('tick'); }, 2000);

    // Native pauses its AVPlayer on foreground transition and tells us where
    // it left off so the on-screen <video> can pick up at exactly that frame.
    window.__biliResume = function (t) {
        try { video.currentTime = t; } catch (e) { /* ignore unseekable state */ }
        video.play().catch(() => { /* may need user tap; harmless */ });
    };

    // Seek-only variant called when the user had paused before backgrounding.
    // Syncs the visible frame to where AVPlayer reached, but does not start
    // playback — the user left it paused and we respect that.
    window.__biliSync = function (t) {
        try { video.currentTime = t; } catch (e) { /* ignore unseekable state */ }
    };

    // Initial sync as soon as metadata is ready (covers the foreground "scan
    // and chill" case before the user actually plays).
    if (video.readyState >= 1) pushState('initial');
})();
