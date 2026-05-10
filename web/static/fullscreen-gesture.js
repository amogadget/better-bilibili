// fullscreen-gesture.js
//
// YouTube-style swipe-up-to-landscape-fullscreen for the iOS native shell.
//
// We deliberately do NOT use video.webkitEnterFullscreen(). iOS's native
// fullscreen player negotiates orientation with its own logic and will
// snap the rotation back to portrait the moment it appears, defeating the
// whole point. YouTube avoids that by rendering its own fullscreen view
// inside a force-rotated app; we do the same:
//
//   1. JS asks Swift (via the `player` message handler) to lock the app
//      to landscape-only orientation. iOS rotates; because the mask is
//      .landscape only, gravity can't pull it back to portrait.
//   2. JS adds a body class. CSS makes .player-wrap fill the viewport
//      and hides every other UI element (topbar, subnav, comments, etc.).
//   3. A close button overlays the top-left of the rotated screen. Tap
//      it → JS removes the class and asks Swift for portrait-only,
//      restoring the normal layout.
//
// Safari is unsupported by design — the user only wants this in the app.
(function () {
    const wrap  = document.querySelector('.player-wrap');
    const video = document.getElementById('player-video');
    if (!wrap || !video) return;

    const native = window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.player;
    if (!native) return;

    const SWIPE_THRESHOLD = 60; // upward pixels before we trigger
    const X_TOLERANCE     = 50; // mostly-horizontal drags ignored
    const SCROLL_GUARD    = 10; // start blocking page scroll this early

    let startX = null;
    let startY = null;
    let armedLeftHalf = false;
    function resetGesture() { startX = null; startY = null; armedLeftHalf = false; }

    wrap.addEventListener('touchstart', (e) => {
        if (e.touches.length !== 1) { resetGesture(); return; }
        const t    = e.touches[0];
        const rect = wrap.getBoundingClientRect();
        armedLeftHalf = t.clientX <= rect.left + rect.width * 0.5;
        startX = t.clientX;
        startY = t.clientY;
    }, { passive: true });

    // Non-passive so we can preventDefault and stop the page from scrolling
    // along with the swipe.
    wrap.addEventListener('touchmove', (e) => {
        if (startY === null || !armedLeftHalf) return;
        const t  = e.touches[0];
        const dx = Math.abs(t.clientX - startX);
        const dy = startY - t.clientY;

        if (dy > SCROLL_GUARD && dx < X_TOLERANCE) {
            if (e.cancelable) e.preventDefault();
        }

        if (dy > SWIPE_THRESHOLD && dx < X_TOLERANCE) {
            enterLandscape();
            resetGesture();
        }
    }, { passive: false });

    wrap.addEventListener('touchend',    resetGesture, { passive: true });
    wrap.addEventListener('touchcancel', resetGesture, { passive: true });

    function postOrientation(to) {
        try {
            native.postMessage({ action: 'orientation', to: to });
        } catch (e) {
            console.log('[bili] postMessage failed:', e);
        }
    }

    function enterLandscape() {
        if (document.body.classList.contains('bili-landscape-fullscreen')) return;
        postOrientation('landscape');
        document.body.classList.add('bili-landscape-fullscreen');
        ensureCloseButton();
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
