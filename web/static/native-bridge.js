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
    const BUILD_TAG = 'native-bridge.js 2026-05-11/attempt6';

    const native = window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.player;
    if (!native) { console.log('[BiliWeb] ' + BUILD_TAG + ' (no native bridge)'); return; }

    // Forward JS logs to Swift so they appear in Xcode's console without
    // needing to attach Safari Web Inspector. Use nlog() instead of
    // console.log for anything you want to read alongside Swift's prints.
    function nlog(msg) {
        const line = '[js] ' + msg;
        console.log('[BiliWeb] ' + msg);
        try { native.postMessage({ action: 'log', msg: line }); } catch (e) { /* ignore */ }
    }
    nlog(BUILD_TAG);

    const video = document.getElementById('player-video');
    if (!video) return;

    // Attempt 6 gesture tracking.
    //
    // Attempt 5 assumed iOS's swipe-up-to-home produces no JS input events.
    // The 2026-05-11 logs disproved that — the swipe fires touchstart in
    // the page before iOS claims it as a system gesture, so the subsequent
    // involuntary pause showed up as userInitiated=true.
    //
    // The signal that actually distinguishes "user tapped pause" from
    // "iOS swiped up to home" is **touchcancel**: WebKit fires it when iOS
    // takes over an in-progress touch (home indicator, control-center pull,
    // notification-center pull, etc.). When that fires, retroactively
    // revoke the gesture credit from this touch.
    //
    // Belt-and-suspenders: pagehide and visibilitychange-to-hidden also
    // invalidate, in case touchcancel doesn't fire on some iOS version.
    // And pause pushes are deferred 300ms to give the invalidation signals
    // a chance to race ahead of the pause event.
    let lastUserGesture = 0;
    let currentTouchStartedAt = 0;
    const GESTURE_WINDOW_MS = 500;
    const PAUSE_DEFER_MS = 300;

    function invalidateGestureCredit(reason) {
        if (lastUserGesture !== 0) {
            nlog('gesture invalidated by ' + reason + ' (was age=' + (Date.now() - lastUserGesture) + 'ms)');
        }
        lastUserGesture = 0;
        currentTouchStartedAt = 0;
    }

    document.addEventListener('touchstart', (e) => {
        const t = e.touches && e.touches[0];
        const tgt = (e.target && e.target.tagName) || '?';
        currentTouchStartedAt = Date.now();
        lastUserGesture = currentTouchStartedAt;
        nlog('touchstart target=' + tgt + ' y=' + (t ? Math.round(t.clientY) : 'n/a') + '/' + window.innerHeight);
    }, true);
    document.addEventListener('touchmove', () => { lastUserGesture = Date.now(); }, true);
    document.addEventListener('touchend',  () => { currentTouchStartedAt = 0; }, true);
    document.addEventListener('touchcancel', () => {
        // iOS claimed this in-progress touch as a system gesture. Roll
        // back lastUserGesture to BEFORE the touch started so any pause
        // event arriving in the next moments doesn't credit it as a user
        // action. This is the primary defense against the home-indicator
        // swipe being misread as a user tap.
        if (currentTouchStartedAt > 0) {
            lastUserGesture = currentTouchStartedAt - 1;
            nlog('touchcancel — rolled gesture back to ' + (currentTouchStartedAt - 1));
        } else {
            nlog('touchcancel (no active touch)');
        }
        currentTouchStartedAt = 0;
    }, true);

    document.addEventListener('mousedown',    () => { lastUserGesture = Date.now(); }, true);
    document.addEventListener('pointerdown',  () => { lastUserGesture = Date.now(); }, true);
    document.addEventListener('click',        () => { lastUserGesture = Date.now(); }, true);
    document.addEventListener('keydown',      () => { lastUserGesture = Date.now(); }, true);

    window.addEventListener('pagehide', () => invalidateGestureCredit('pagehide'));
    document.addEventListener('visibilitychange', () => {
        nlog('visibilitychange -> ' + document.visibilityState + ' playing=' + !video.paused);
        if (document.visibilityState !== 'visible') {
            invalidateGestureCredit('visibilitychange');
        }
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

    function sendState(reason) {
        const playing = !video.paused;
        const vis = document.visibilityState;
        const gestureAge = Date.now() - lastUserGesture;
        const userInitiated = lastUserGesture > 0 && gestureAge < GESTURE_WINDOW_MS;
        nlog('pushState reason=' + reason + ' playing=' + playing + ' vis=' + vis + ' t=' + video.currentTime.toFixed(2) + ' gestureAge=' + gestureAge + ' userInit=' + userInitiated);

        if (vis !== 'visible') {
            nlog('pushState blocked by visibility guard');
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

    // Pause pushes are deferred so touchcancel / pagehide / visibilitychange
    // can race ahead and invalidate the gesture credit before we send. Play
    // and seek pushes go immediately — those are user actions that should
    // reach AVPlayer without delay.
    let pendingPauseTimer = null;
    function pushState(reason) {
        const wouldBePause = video.paused;
        if (wouldBePause) {
            if (pendingPauseTimer) clearTimeout(pendingPauseTimer);
            pendingPauseTimer = setTimeout(() => {
                pendingPauseTimer = null;
                sendState(reason + '*deferred');
            }, PAUSE_DEFER_MS);
            nlog('pushState reason=' + reason + ' deferred ' + PAUSE_DEFER_MS + 'ms (pause)');
            return;
        }
        // Non-pause: cancel any pending deferred pause and send immediately.
        if (pendingPauseTimer) {
            clearTimeout(pendingPauseTimer);
            pendingPauseTimer = null;
            nlog('pending deferred pause cancelled by ' + reason);
        }
        sendState(reason);
    }

    video.addEventListener('play',           () => pushState('play'));
    video.addEventListener('pause',          () => pushState('pause'));
    video.addEventListener('seeked',         () => pushState('seeked'));
    video.addEventListener('loadedmetadata', () => pushState('loadedmetadata'));
    video.addEventListener('ratechange',     () => pushState('ratechange'));

    // Periodic refresh while playing so currentTime/positions on the lock screen
    // aren't stale by the time the app backgrounds.
    setInterval(() => { if (!video.paused) sendState('tick'); }, 2000);

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
    if (video.readyState >= 1) sendState('initial');
})();
