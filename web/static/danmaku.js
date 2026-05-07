(function () {
    const video = document.querySelector('.player-wrap video');
    const layer = document.querySelector('.danmaku-layer');
    const toggle = document.getElementById('danmaku-toggle');
    if (!video || !layer || !toggle) return;

    const url = layer.dataset.url;
    let items = [];
    let cursor = 0;
    let enabled = localStorage.getItem('bili-danmaku') !== 'off';
    let lanes = [];
    const LANE_COUNT = 8;
    const SCROLL_DURATION_MS = 8000;

    const SLOT_OPENS_AT = (i) => lanes[i] || 0;

    function setEnabled(on) {
        enabled = on;
        localStorage.setItem('bili-danmaku', on ? 'on' : 'off');
        toggle.classList.toggle('active', on);
        toggle.textContent = on ? '弹 ON' : '弹 OFF';
        if (!on) clearLayer();
    }

    function clearLayer() {
        layer.innerHTML = '';
        lanes = new Array(LANE_COUNT).fill(0);
    }

    function reset() {
        const t = video.currentTime;
        cursor = items.findIndex(d => d.t >= t);
        if (cursor < 0) cursor = items.length;
        clearLayer();
    }

    function pickLane() {
        const now = performance.now();
        for (let i = 0; i < LANE_COUNT; i++) {
            if (lanes[i] <= now) return i;
        }
        // No free lane — overlap onto the soonest-free one to avoid losing items.
        let best = 0;
        for (let i = 1; i < LANE_COUNT; i++) {
            if (lanes[i] < lanes[best]) best = i;
        }
        return best;
    }

    function spawn(item) {
        if (!enabled) return;
        const el = document.createElement('span');
        el.className = 'danmaku';
        el.textContent = item.x;
        if (item.c && item.c !== 16777215) {
            el.style.color = '#' + item.c.toString(16).padStart(6, '0');
        }

        if (item.k === 1) {
            // scroll right-to-left
            const lane = pickLane();
            el.style.top = (lane * 28 + 4) + 'px';
            layer.appendChild(el);

            const layerW = layer.offsetWidth;
            const elW = el.offsetWidth;
            el.style.transform = `translateX(${layerW}px)`;
            // Force a reflow so the transition starts from the initial position.
            void el.offsetWidth;
            el.style.transition = `transform ${SCROLL_DURATION_MS}ms linear`;
            el.style.transform = `translateX(${-elW}px)`;

            const laneFreeIn = (elW / (layerW + elW)) * SCROLL_DURATION_MS;
            lanes[lane] = performance.now() + laneFreeIn + 200; // small gap
            el.addEventListener('transitionend', () => el.remove(), { once: true });
        } else if (item.k === 5) {
            el.classList.add('top');
            layer.appendChild(el);
            setTimeout(() => el.remove(), 5000);
        } else if (item.k === 4) {
            el.classList.add('bottom');
            layer.appendChild(el);
            setTimeout(() => el.remove(), 5000);
        }
    }

    function tick() {
        if (!enabled || items.length === 0) return;
        const now = video.currentTime;
        // Spawn anything whose time has arrived (small lookahead for smoother launch).
        while (cursor < items.length && items[cursor].t <= now + 0.05) {
            spawn(items[cursor]);
            cursor++;
        }
    }

    async function load() {
        try {
            const r = await fetch(url);
            if (!r.ok) throw new Error('http ' + r.status);
            items = await r.json();
            // Already sorted server-side; defensive sort for safety.
            items.sort((a, b) => a.t - b.t);
            reset();
        } catch (e) {
            console.warn('danmaku load failed', e);
        }
    }

    video.addEventListener('timeupdate', tick);
    video.addEventListener('seeked', reset);
    video.addEventListener('seeking', clearLayer);
    toggle.addEventListener('click', () => setEnabled(!enabled));

    setEnabled(enabled);
    load();
})();
