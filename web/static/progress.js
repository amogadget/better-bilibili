// progress.js
//
// Two responsibilities, both wired to the watch page's <video>:
//
//   1. SILENT RESUME. The server stamps `data-resume-at` (in seconds) on
//      the <video> with bilibili's stored last-played position. On
//      `loadedmetadata` we seek to that point — no banner, no prompt.
//      Skipped if the position is past 95% of duration (already finished).
//
//   2. HEARTBEAT. As the user watches, periodically POST the current
//      playback position to /heartbeat so bilibili's server-side history
//      reflects what we actually played. Sent on play/pause/seek/ended,
//      throttled to ~15s during steady playback, and force-flushed via
//      navigator.sendBeacon() on beforeunload so closing the tab still
//      records the position.
(function () {
    const video = document.getElementById('player-video');
    if (!video) return;

    const aid    = video.dataset.aid;
    const cid    = video.dataset.cid;
    const bvid   = video.dataset.bvid;
    const resume = parseInt(video.dataset.resumeAt || '0', 10);
    if (!aid || !cid || !bvid) return;

    // ---- silent resume ---------------------------------------------------

    let resumed = false;
    function tryResume() {
        if (resumed || resume <= 0) return;
        const dur = video.duration;
        if (isFinite(dur) && dur > 0 && resume / dur > 0.95) {
            resumed = true;
            return;
        }
        try { video.currentTime = resume; } catch (e) {}
        resumed = true;
    }
    if (resume > 0) {
        if (video.readyState >= 1) {
            tryResume();
        } else {
            video.addEventListener('loadedmetadata', tryResume, { once: true });
        }
    }

    // ---- heartbeat -------------------------------------------------------

    let lastSentMs   = 0;
    let lastSentTime = -1;
    const THROTTLE_MS = 14000;

    function send(t, force) {
        if (!isFinite(t) || t < 0) return;
        const now = Date.now();
        const tInt = Math.floor(t);
        if (!force) {
            if (now - lastSentMs < THROTTLE_MS) return;
            if (Math.abs(tInt - lastSentTime) < 1) return;
        }
        lastSentMs   = now;
        lastSentTime = tInt;

        const body = new URLSearchParams({ aid, cid, bvid, t: String(tInt) });
        if (navigator.sendBeacon) {
            navigator.sendBeacon('/heartbeat', body);
        } else {
            fetch('/heartbeat', {
                method:    'POST',
                body:      body,
                keepalive: true,
            }).catch(() => { /* swallow — best-effort */ });
        }
    }

    video.addEventListener('play',    () => send(video.currentTime, true));
    video.addEventListener('pause',   () => send(video.currentTime, true));
    video.addEventListener('seeked',  () => send(video.currentTime, true));
    video.addEventListener('ended',   () => send(video.duration || video.currentTime, true));
    video.addEventListener('timeupdate', () => {
        if (!video.paused) send(video.currentTime, false);
    });
    window.addEventListener('beforeunload', () => send(video.currentTime, true));
    window.addEventListener('pagehide',     () => send(video.currentTime, true));
})();
