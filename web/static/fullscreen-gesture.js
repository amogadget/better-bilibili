// fullscreen-gesture.js
//
// YouTube-style swipe-up-to-landscape-fullscreen for the iOS native shell.
//
// We deliberately do NOT use video.webkitEnterFullscreen(). iOS's native
// fullscreen player negotiates orientation with its own logic and snaps
// the rotation back to portrait. Instead:
//
//   1. JS asks Swift (via the `player` channel) to lock the app to
//      landscape-only orientation. iOS rotates; the mask is .landscape
//      only, so gravity can't pull it back.
//   2. JS adds a body class. CSS makes .player-wrap fill the viewport
//      and hides every other UI element.
//   3. A close button overlays the top-left. Tap it → portrait again.
//
// The gesture itself is interactive (YouTube-style): during the drag the
// player scales and lifts proportionally to finger distance; on release
// we either commit (animate further then trigger the device rotation) or
// snap back with a spring overshoot. The release-decided model means a
// half-hearted drag bounces cleanly back instead of triggering an
// unintended fullscreen.
//
// Safari is unsupported by design — the user only wants this in the app.
(function () {
    const wrap  = document.querySelector('.player-wrap');
    const video = document.getElementById('player-video');
    if (!wrap || !video) return;

    const native = window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.player;
    if (!native) return;

    const COMMIT_THRESHOLD  = 80;   // upward px on release to commit
    const MAX_DRAG_FEEDBACK = 150;  // px past which visual feedback stops growing
    const X_TOLERANCE       = 50;   // mostly-horizontal drags ignored
    const SCROLL_GUARD      = 10;   // start blocking page scroll this early
    const COMMIT_DURATION   = 280;  // ms, roughly matches iOS rotation animation
    const SNAP_DURATION     = 320;  // ms, spring needs a touch more time
    const MAX_SCALE         = 0.25; // scale = 1 + progress * MAX_SCALE
    const MAX_TRANSLATE_Y   = 45;   // px, lift companion to the zoom

    const COMMIT_EASING = 'cubic-bezier(0.4, 0, 0.2, 1)';                // ease-out
    const SNAP_EASING   = 'cubic-bezier(0.34, 1.56, 0.64, 1)';           // spring overshoot

    let startX = null;
    let startY = null;
    let lastY  = null;
    let armedLeftHalf = false;
    let dragging = false;

    function resetTracking() {
        startX = null;
        startY = null;
        lastY  = null;
        armedLeftHalf = false;
        dragging = false;
    }

    function applyDragTransform(dy) {
        const clamped    = Math.max(0, Math.min(dy, MAX_DRAG_FEEDBACK));
        const progress   = clamped / MAX_DRAG_FEEDBACK;
        const scale      = 1 + progress * MAX_SCALE;
        const translateY = -progress * MAX_TRANSLATE_Y;
        wrap.style.transition = 'none';
        wrap.style.transform  = 'translateY(' + translateY + 'px) scale(' + scale + ')';
    }

    function commit() {
        // Continue the zoom briefly while iOS starts rotating. The
        // orientation message is posted at the START of our transition
        // so the two animations overlap and read as one motion.
        wrap.style.transition = 'transform ' + COMMIT_DURATION + 'ms ' + COMMIT_EASING;
        wrap.style.transform  = 'translateY(-55px) scale(' + (1 + MAX_SCALE + 0.03) + ')';
        postOrientation('landscape');
        setTimeout(() => {
            wrap.style.transition = 'none';
            wrap.style.transform  = '';
            document.body.classList.add('bili-landscape-fullscreen');
            ensureCloseButton();
        }, COMMIT_DURATION);
    }

    function snapBack() {
        wrap.style.transition = 'transform ' + SNAP_DURATION + 'ms ' + SNAP_EASING;
        wrap.style.transform  = '';
    }

    wrap.addEventListener('touchstart', (e) => {
        if (e.touches.length !== 1) {
            // Multi-touch — abort the gesture and snap back if mid-drag.
            if (dragging) snapBack();
            resetTracking();
            return;
        }
        const t    = e.touches[0];
        const rect = wrap.getBoundingClientRect();
        armedLeftHalf = t.clientX <= rect.left + rect.width * 0.5;
        startX = t.clientX;
        startY = t.clientY;
        lastY  = t.clientY;
        dragging = false;
        // Allow the gesture to immediately follow the finger even if a
        // previous settling animation is still running.
        wrap.style.transition = 'none';
    }, { passive: true });

    // Non-passive so we can preventDefault and stop the page from scrolling
    // along with the swipe.
    wrap.addEventListener('touchmove', (e) => {
        if (startY === null || !armedLeftHalf) return;
        const t  = e.touches[0];
        const dx = Math.abs(t.clientX - startX);
        const dy = startY - t.clientY;
        lastY    = t.clientY;

        // Mostly-vertical, upward motion past the scroll guard → we're
        // dragging. Block page scroll and apply visual feedback.
        if (dy > SCROLL_GUARD && dx < X_TOLERANCE) {
            if (e.cancelable) e.preventDefault();
            dragging = true;
        }

        if (dragging) applyDragTransform(dy);
    }, { passive: false });

    function handleRelease() {
        if (!dragging) {
            // Touch never crossed the scroll guard; nothing to undo.
            resetTracking();
            return;
        }
        const dy = (startY !== null && lastY !== null) ? startY - lastY : 0;
        if (dy >= COMMIT_THRESHOLD && armedLeftHalf) {
            commit();
        } else {
            snapBack();
        }
        resetTracking();
    }

    wrap.addEventListener('touchend',    handleRelease, { passive: true });
    wrap.addEventListener('touchcancel', handleRelease, { passive: true });

    function postOrientation(to) {
        try {
            native.postMessage({ action: 'orientation', to: to });
        } catch (e) {
            console.log('[bili] postMessage failed:', e);
        }
    }

    function exitLandscape() {
        if (!document.body.classList.contains('bili-landscape-fullscreen')) return;
        document.body.classList.remove('bili-landscape-fullscreen');
        postOrientation('portrait');
    }

    function ensureCloseButton() {
        if (document.getElementById('bili-fs-close')) return;
        const btn = document.createElement('button');
        btn.id = 'bili-fs-close';
        btn.type = 'button';
        btn.setAttribute('aria-label', 'Exit fullscreen');
        btn.innerHTML = '&#x2715;'; // ✕
        btn.addEventListener('click', (e) => {
            e.preventDefault();
            e.stopPropagation();
            exitLandscape();
        });
        document.body.appendChild(btn);
    }

    // If the user navigates away (back, link), un-lock orientation so the
    // next page doesn't open in a forced landscape state.
    window.addEventListener('pagehide', () => {
        if (document.body.classList.contains('bili-landscape-fullscreen')) {
            postOrientation('portrait');
        }
    });
})();
