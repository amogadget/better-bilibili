// fullscreen-gesture.js
//
// YouTube-style "drag the left half of the player upward to enter rotated
// fullscreen."
//
// In the iOS native shell we explicitly ask Swift to rotate the OS to
// landscape via the `orientation` bridge, then call webkitEnterFullscreen
// once the rotation lands. On webkitendfullscreen we ask Swift to rotate
// back to portrait. This is the approach YouTube's iOS app uses; iOS
// won't auto-rotate for video fullscreen unless the app explicitly
// requests it via UIKit, even when the device's rotation lock is off.
//
// In Safari (no native bridge) we fall back to plain webkitEnterFullscreen.
// iOS Safari sometimes rotates for landscape videos and sometimes not — it
// depends on iOS version and a handful of opaque conditions; nothing more
// we can do from JavaScript.
//
// The gesture is restricted to the left half of the player so the right
// half stays available for the native scrub bar / fullscreen button.
(function () {
    const wrap  = document.querySelector('.player-wrap');
    const video = document.getElementById('player-video');
    if (!wrap || !video) return;

    const SWIPE_THRESHOLD = 60;
    const X_TOLERANCE     = 50;

    const native = window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.player;

    let startX = null;
    let startY = null;
    function reset() { startX = null; startY = null; }

    wrap.addEventListener('touchstart', (e) => {
        if (e.touches.length !== 1) { reset(); return; }
        const t    = e.touches[0];
        const rect = wrap.getBoundingClientRect();
        if (t.clientX > rect.left + rect.width * 0.5) { reset(); return; }
        startX = t.clientX;
        startY = t.clientY;
    }, { passive: true });

    wrap.addEventListener('touchmove', (e) => {
        if (startY === null) return;
        const t  = e.touches[0];
        const dx = Math.abs(t.clientX - startX);
        const dy = startY - t.clientY;
        if (dy > SWIPE_THRESHOLD && dx < X_TOLERANCE) {
            enterFullscreen();
            reset();
        }
    }, { passive: true });

    wrap.addEventListener('touchend',    reset, { passive: true });
    wrap.addEventListener('touchcancel', reset, { passive: true });

    function postOrientation(to) {
        if (!native) return;
        try { native.postMessage({ action: 'orientation', to: to }); } catch (e) {}
    }

    function callWebkitFullscreen() {
        if (typeof video.webkitEnterFullscreen === 'function') {
            try { video.webkitEnterFullscreen(); return true; } catch (e) {}
        }
        // Desktop / non-iOS fallback
        const target = wrap.requestFullscreen ? wrap : video;
        if (typeof target.requestFullscreen === 'function') {
            target.requestFullscreen().catch(() => {});
            return true;
        }
        return false;
    }

    function enterFullscreen() {
        if (!native) {
            // Safari / desktop — best effort, no orientation control.
            callWebkitFullscreen();
            return;
        }

        // Native shell: rotate first, then fullscreen.
        postOrientation('landscape');

        // Wait for the OS rotation to land. Prefer the actual
        // orientationchange event, but fall back to a timeout so we still
        // fullscreen even if no event fires.
        let fired = false;
        const fire = () => {
            if (fired) return;
            fired = true;
            callWebkitFullscreen();
        };
        window.addEventListener('orientationchange', fire, { once: true });
        setTimeout(fire, 500);

        // On fullscreen exit, swing the device back to portrait.
        video.addEventListener('webkitendfullscreen', () => {
            postOrientation('portrait');
        }, { once: true });
    }
})();
