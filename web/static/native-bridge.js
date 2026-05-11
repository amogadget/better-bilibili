// native-bridge.js
//
// When running inside the iOS native shell (BiliWeb.app, which exposes
// `window.webkit.messageHandlers.player`), keep the host app informed of
// the current playback state. The host runs an AVPlayer in parallel and
// uses these state pushes to mirror position/play-pause; when the app
// backgrounds, AVPlayer is the audio source while WKWebView's WebContent
// process is suspended.
//
// In Safari (no message handler) this file does nothing; audio-mode.js
// handles the dual-element fallback there.
//
// ---------------------------------------------------------------------------
// The tricky part: distinguishing user pauses from iOS system pauses.
//
// When iOS backgrounds the app (swipe-up-to-home, lock, app switcher), it
// auto-pauses the muted <video> element. That fires a normal `pause` event
// indistinguishable from the user tapping the pause button — and our naive
// "mirror pause into AVPlayer" handler would then halt the background audio.
//
// Every signal we tried to gate on (visibilityState, applicationState,
// willResignActive) arrives AFTER the pause event has been delivered. The
// one signal that beats it: the iOS home-indicator swipe is a system gesture
// — WebKit fires `touchstart` for it but later fires `touchcancel` when iOS
// claims the gesture as its own. So we tag each state push with
// `userInitiated`, which is true only if a JS gesture is recent AND wasn't
// canceled. Swift then drops any pause with `userInitiated=false`.
//
// For the swipe-up case, pause arrives *during* the touch (touchcancel
// hasn't fired yet). So we defer pause pushes by 300ms when a touch is
// in progress, giving touchcancel time to race ahead and zero out gesture
// credit. Clean taps (touchend already fired) bypass the defer so user
// pauses reach AVPlayer immediately — important because the user might
// lock the screen right after, and the deferred pause would otherwise be
// swallowed by the visibility guard.
// ---------------------------------------------------------------------------
(function () {
    const native = window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.player;
    if (!native) return;

    const video = document.getElementById('player-video');
    if (!video) return;

    // Gesture credit: `lastUserGesture` is the ms-timestamp of the most
    // recent JS input event from the user. A push is `userInitiated` only
    // if a gesture happened within GESTURE_WINDOW_MS. `currentTouchStartedAt`
    // tracks whether a touch is currently in progress (cleared on touchend
    // or touchcancel), so we know whether to defer pause pushes.
    let lastUserGesture = 0;
    let currentTouchStartedAt = 0;
    const GESTURE_WINDOW_MS = 500;
    const PAUSE_DEFER_MS = 300;

    function invalidateGestureCredit() {
        lastUserGesture = 0;
        currentTouchStartedAt = 0;
    }

    document.addEventListener('touchstart', () => {
        currentTouchStartedAt = Date.now();
        lastUserGesture = currentTouchStartedAt;
    }, true);
    // touchmove is gated on currentTouchStartedAt > 0. Without this gate,
    // touchmoves that iOS sometimes dispatches after touchcancel would
    // overwrite the cancel-zeroed lastUserGesture, defeating the whole
    // point of touchcancel-as-system-gesture-signal.
    document.addEventListener('touchmove', () => {
        if (currentTouchStartedAt > 0) lastUserGesture = Date.now();
    }, true);
    document.addEventListener('touchend', () => { currentTouchStartedAt = 0; }, true);
    document.addEventListener('touchcancel', () => {
        // iOS claimed this in-progress touch as a system gesture
        // (home-indicator swipe, control center, etc.). Zero out gesture
        // credit entirely — the short-circuit `lastUserGesture > 0` in
        // sendState then forces userInitiated=false on any subsequent
        // pause push, regardless of how recent the touch was.
        lastUserGesture = 0;
        currentTouchStartedAt = 0;
    }, true);

    // Mouse, pointer, click, and keyboard inputs are always user-initiated
    // — none of them fire during the home-indicator swipe.
    document.addEventListener('mousedown',   () => { lastUserGesture = Date.now(); }, true);
    document.addEventListener('pointerdown', () => { lastUserGesture = Date.now(); }, true);
    document.addEventListener('click',       () => { lastUserGesture = Date.now(); }, true);
    document.addEventListener('keydown',     () => { lastUserGesture = Date.now(); }, true);

    // Belt-and-suspenders: if touchcancel doesn't fire on some iOS version,
    // the page going hidden is also a clear "no user gesture is in progress"
    // signal.
    window.addEventListener('pagehide', invalidateGestureCredit);
    document.addEventListener('visibilitychange', () => {
        if (document.visibilityState !== 'visible') invalidateGestureCredit();
    });

    // In the iOS shell, native AVPlayer is the audio source. The visible
    // <video> element is for pixels only — mute it so we don't get two
    // simultaneous audio streams.
    video.muted = true;
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
    // AVPlayer doesn't reliably play it as a standalone URL.
    function audioSourceURL() {
        if (video.src) return new URL(video.src, location.href).href;
        return null;
    }

    function sendState(reason) {
        const vis = document.visibilityState;
        if (vis !== 'visible') return;

        const src = audioSourceURL();
        if (!src) return;

        const gestureAge = Date.now() - lastUserGesture;
        const userInitiated = lastUserGesture > 0 && gestureAge < GESTURE_WINDOW_MS;
        const meta = readMeta();
        try {
            native.postMessage({
                action: 'state',
                src: src,
                currentTime: video.currentTime,
                duration: isFinite(video.duration) ? video.duration : 0,
                playing: !video.paused,
                userInitiated: userInitiated,
                title:   meta.title,
                artist:  meta.artist,
                artwork: meta.artwork,
            });
        } catch (e) { /* postMessage can throw on serialization edge cases */ }
    }

    // Defer pause pushes only when a touch is still in progress — that's
    // the swipe-up case where we need to wait for touchcancel. A clean
    // user tap already cleared currentTouchStartedAt via touchend, so it
    // sends immediately (important: the user may lock the screen right
    // after, and a deferred pause would be eaten by the visibility guard
    // before reaching Swift, leaving AVPlayer running).
    let pendingPauseTimer = null;
    function pushState(reason) {
        if (video.paused && currentTouchStartedAt > 0) {
            if (pendingPauseTimer) clearTimeout(pendingPauseTimer);
            pendingPauseTimer = setTimeout(() => {
                pendingPauseTimer = null;
                sendState(reason);
            }, PAUSE_DEFER_MS);
            return;
        }
        if (pendingPauseTimer) {
            clearTimeout(pendingPauseTimer);
            pendingPauseTimer = null;
        }
        sendState(reason);
    }

    video.addEventListener('play',           () => pushState('play'));
    video.addEventListener('pause',          () => pushState('pause'));
    video.addEventListener('seeked',         () => pushState('seeked'));
    video.addEventListener('loadedmetadata', () => pushState('loadedmetadata'));
    video.addEventListener('ratechange',     () => pushState('ratechange'));

    // Periodic refresh while playing so currentTime stays in sync.
    setInterval(() => { if (!video.paused) sendState('tick'); }, 2000);

    // Native pauses its AVPlayer on foreground transition and tells us
    // where it left off so the on-screen <video> can pick up at that frame.
    window.__biliResume = function (t) {
        try { video.currentTime = t; } catch (e) { /* ignore unseekable state */ }
        video.play().catch(() => { /* may need user tap; harmless */ });
    };

    // Seek-only variant called when the user had paused before backgrounding.
    window.__biliSync = function (t) {
        try { video.currentTime = t; } catch (e) { /* ignore unseekable state */ }
    };

    if (video.readyState >= 1) sendState('initial');
})();
