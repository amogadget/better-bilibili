(function () {
    const container = document.getElementById('qr-container');
    const status = document.getElementById('status');
    let key = null;
    let aborted = false;

    async function start() {
        try {
            const r = await fetch('/login/start');
            if (!r.ok) throw new Error('start failed: ' + r.status);
            const d = await r.json();
            key = d.key;
            container.innerHTML = `<img src="${d.qr}" alt="Login QR code">`;
            status.textContent = 'Waiting for scan…';
            poll();
        } catch (e) {
            status.textContent = 'Error: ' + e.message;
            setTimeout(start, 4000);
        }
    }

    async function poll() {
        if (aborted || !key) return;
        try {
            const r = await fetch('/login/poll?key=' + encodeURIComponent(key));
            if (!r.ok) throw new Error('poll failed: ' + r.status);
            const d = await r.json();
            if (d.code === 0) {
                status.textContent = 'Logged in! Redirecting…';
                aborted = true;
                setTimeout(() => { location.href = '/'; }, 600);
                return;
            } else if (d.code === 86038) {
                status.textContent = 'QR expired. Refreshing…';
                key = null;
                start();
                return;
            } else if (d.code === 86090) {
                status.textContent = 'Scanned. Confirm in the bilibili app.';
            } else {
                status.textContent = 'Waiting for scan…';
            }
        } catch (e) {
            status.textContent = 'Error: ' + e.message;
        }
        setTimeout(poll, 2000);
    }

    start();
})();
