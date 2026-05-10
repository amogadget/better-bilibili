// fullscreen-gesture.js
//
// YouTube-style "swipe up on the player to enter rotated fullscreen."
//
// Status: debugging. Lots of console.log so we can see in Xcode/Safari
// Web Inspector exactly where the chain breaks. Strip the logs once the
// flow is confirmed working end-to-end.
//
// In the iOS native shell we ask Swift to rotate the OS via the
// `orientation` bridge, then call webkitEnterFullscreen once the rotation
// lands. Safari has no rotation API; this script does nothing for Safari
// per user's request (only the app matters).
(function () {
    const wrap  = document.querySelector('.player-wrap');
    const video = document.getElementById('player-video');
    if (!wrap || !video) return;

    const native = window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.player;
    if (!native) return; // skip entirely in Safari per user direction

    const SWIPE_THRESHOLD = 60;
    const X_TOLERANCE     = 50;
    const SCROLL_GUARD    = 10; // start blocking page scroll once we look like an upward swipe

    let startX = null;
    let startY = null;
    let armedLeftHalf = false;
    function reset() { startX = null; startY = null; armedLeftHalf = false; }

    wrap.addEventListener('touchstart', (e) => {
        if (e.touches.length !== 1) { reset(); return; }
        const t    = e.touches[0];
        const rect = wrap.getBoundingClientRect();
        armedLeftHalf = t.clientX <= rect.left + rect.width * 0.5;
        startX = t.clientX;
        startY = t.clientY;
    }, { passive: true });

    // touchmove must be non-passive so we can preventDefault and stop the
    // page from scrolling while the user is swiping inside the player.
    wrap.addEventListener('touchmove', (e) => {
        if (startY === null || !armedLeftHalf) return;
        const t  = e.touches[0];
        const dx = Math.abs(t.clientX - startX);
        const dy = startY - t.clientY; // positive = upward

        if (dy > SCROLL_GUARD && dx < X_TOLERANCE) {
            // Looks like an upward swipe — stop the page scrolling along with it.
            if (e.cancelable) e.preventDefault();
        }

        if (dy > SWIPE_THRESHOLD && dx < X_TOLERANCE) {
            enterFullscreen();
            reset();
        }
    }, { passive: false });

    wrap.addEventListener('touchend',    reset, { passive: true });
    wrap.addEventListener('touchcancel', reset, { passive: true });

    function postOrientation(to) {
        try {
            native.postMessage({ action: 'orientation', to: to });
            console.log('[bili] postMessage orientation=' + to + ' ok');
        } catch (e) {
            console.log('[bili] postMessage failed:', e);
        }
    }

    function callWebkitFullscreen() {
        if (typeof video.webkitEnterFullscreen === 'function') {
            try {
                video.webkitEnterFullscreen();
                console.log('[bili] webkitEnterFullscreen called');
                return true;
            } catch (e) {
                console.log('[bili] webkitEnterFullscreen threw:', e);
            }
        }
        return false;
    }

    function enterFullscreen() {
        console.log('[bili] swipe -> enterFullscreen, native bridge present');
        postOrientation('landscape');

        // Wait for the OS rotation, fall back to a timeout.
        let fired = false;
        const fire = () => {
            if (fired) return;
            fired = true;
            callWebkitFullscreen();
        };
        window.addEventListener('orientationchange', () => {
            console.log('[bili] orientationchange fired');
            fire();
        }, { once: true });
        setTimeout(() => {
            if (!fired) console.log('[bili] orientation timeout reached, fullscreening anyway');
            fire();
        }, 500);

        video.addEventListener('webkitendfullscreen', () => {
            console.log('[bili] webkitendfullscreen -> portrait');
            postOrientation('portrait');
        }, { once: true });
    }
})();
