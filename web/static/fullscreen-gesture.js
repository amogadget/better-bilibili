// fullscreen-gesture.js
//
// YouTube-style "drag the left half of the player upward to enter rotated
// fullscreen." We just call video.webkitEnterFullscreen() — on iOS Safari
// (and WKWebView) the native player auto-rotates the screen to landscape
// for 16:9 videos, which is exactly the rotation behavior the user wants
// without having to physically turn the phone.
//
// Restricted to the left half of the player so the right half remains
// available for native controls (scrub bar, fullscreen button, etc).
(function () {
    const wrap  = document.querySelector('.player-wrap');
    const video = document.getElementById('player-video');
    if (!wrap || !video) return;

    const SWIPE_THRESHOLD = 60; // upward pixels needed to fire
    const X_TOLERANCE     = 50; // mostly-horizontal drags are ignored

    let startX = null;
    let startY = null;

    function reset() { startX = null; startY = null; }

    wrap.addEventListener('touchstart', (e) => {
        if (e.touches.length !== 1) { reset(); return; }
        const t    = e.touches[0];
        const rect = wrap.getBoundingClientRect();
        // Only arm on the left half — right half is for the scrub bar /
        // native fullscreen button so users can still hit those without
        // accidentally invoking us.
        if (t.clientX > rect.left + rect.width * 0.5) { reset(); return; }
        startX = t.clientX;
        startY = t.clientY;
    }, { passive: true });

    wrap.addEventListener('touchmove', (e) => {
        if (startY === null) return;
        const t  = e.touches[0];
        const dx = Math.abs(t.clientX - startX);
        const dy = startY - t.clientY; // positive = upward
        if (dy > SWIPE_THRESHOLD && dx < X_TOLERANCE) {
            enterFullscreen();
            reset();
        }
    }, { passive: true });

    wrap.addEventListener('touchend',    reset, { passive: true });
    wrap.addEventListener('touchcancel', reset, { passive: true });

    function enterFullscreen() {
        // iOS-only proprietary call. This is the ONLY API on iOS that
        // triggers the auto-rotating native fullscreen player; the
        // standard requestFullscreen() does not rotate.
        if (typeof video.webkitEnterFullscreen === 'function') {
            try { video.webkitEnterFullscreen(); return; } catch (e) { /* fall through */ }
        }
        // Desktop fallback.
        const target = wrap.requestFullscreen ? wrap : video;
        if (typeof target.requestFullscreen === 'function') {
            target.requestFullscreen().catch(() => {});
        }
    }
})();
