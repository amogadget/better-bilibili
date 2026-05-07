# bili-web

A small, single-user, mobile-first web frontend for bilibili. Designed to be
self-hosted on a VPS so you can browse and watch from a device where the
official app is either unavailable, restricted, or simply unpleasant to use
(iOS being the original target).

The bilibili mobile website is intentionally crippled to push users into the
app, and the app is increasingly ad-heavy. This project is a thin proxy that
talks to bilibili's web APIs on your behalf and serves a clean, ad-free
interface tailored for a phone screen.

## This is a personal-use project

> **Read this before you deploy.** This server stores your real bilibili
> cookies in a config file and uses them on every request. Anyone who can
> reach the URL gets to act as you on bilibili — read your DMs, post
> comments under your name, change account settings, and more.
>
> **Do not expose this on the public internet without authentication in
> front of it.** The recommended setup (below) is to run it on a private
> network with [Tailscale](https://tailscale.com) so only your own devices
> can reach it. If you must expose it publicly, put HTTP basic auth or a
> similar gate on the reverse proxy.
>
> Also: this client uses bilibili's web APIs without permission from
> bilibili. They may change endpoints or signing requirements at any time,
> and aggressive use against the public APIs can get your account
> rate-limited or banned. Keep traffic personal-scale.

## Status

- Single user (one set of cookies, stored server-side).
- Read-only: browse, search, watch. No posting, liking, or commenting.
- Tested on iPhone Safari. Should work on any modern mobile browser.

## What it does

- Recommended feed (bilibili's algorithmic homepage).
- Subscriptions feed (videos from creators you follow).
- Search.
- Favorites (收藏夹) — folder list and folder contents.
- Watch later (稍后再看).
- Watch page with native HTML5 `<video>` playback (capped at 720p by default).
  - Single-part videos stream as plain mp4 directly from bilibili's CDN
    (instant start).
  - Multi-part videos are remuxed on-the-fly to HLS by `ffmpeg` and served
    seamlessly as one continuous stream.
  - Creator series (合集 / `ugc_season`) sidebar with episode navigation.
  - Top-level comments.
  - Danmaku (弹幕) overlay synced to playback time, with a toggle.
- Pagination on every list page.
- QR-code login: scan with the bilibili app on your phone to grant the server
  access to your account. Cookies persist on disk.

## Architecture

```
iPhone Safari
     |
     v   HTTPS
  Caddy   (TLS termination, reverse proxy)
     |
     v   HTTP on 127.0.0.1
  bili-web   (Go binary, ~7 MB, single static executable)
     |          |
     |          +-- ffmpeg subprocess (only for multi-part videos)
     v
  api.bilibili.com  (JSON APIs, with session cookies)
  *.bilivideo.com   (CDN — video bytes proxied through us so the right
                     Referer/User-Agent headers are attached)
```

The Go server:

- Talks to bilibili's web APIs with WBI request signing where required.
- Stores your session cookies (`SESSDATA`, `bili_jct`, `DedeUserID`) in
  `config.yaml` after a one-time QR login.
- Proxies media bytes (video, thumbnails, avatars) so bilibili's CDN sees
  the headers it expects, instead of a browser-stripped request.
- For multi-part long videos, spawns `ffmpeg` to mux bilibili's separate
  DASH audio + video streams into a fragmented-mp4 HLS playlist that iOS
  Safari can play natively.

## Requirements

- Linux server (any distro). Tested on Ubuntu 24.04 / arm64.
- Go 1.22 or newer for the build.
- `ffmpeg` for multi-part videos. Skippable if every video you watch is
  short enough to be served as a single mp4.
- A way to reach the server from your phone. Two paths covered below:
  Tailscale (private, recommended), or a public domain + Caddy.

## Installation

```bash
git clone <your-fork-url> /opt/bili-web
cd /opt/bili-web
go build -o bili-web ./cmd/bili-web
cp config.yaml.example config.yaml
chmod 600 config.yaml
```

Run it:

```bash
./bili-web -config config.yaml
```

It will listen on `127.0.0.1:8765` by default and write its working files
relative to the current directory (`web/` for templates and static assets,
`cache/hls/` for ffmpeg HLS sessions).

### As a systemd service

A reference unit file is in `deploy/bili-web.service`. Adjust paths and copy
to `/etc/systemd/system/`:

```bash
sudo cp deploy/bili-web.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now bili-web
```

You can confirm it's listening with `curl http://127.0.0.1:8765/healthz`.
That gets you a working server reachable on `localhost`. The next two
sections are about how to reach it from your phone.

## Deployment: pick one

### Option A — Tailscale (recommended for personal use)

This keeps your server invisible to the public internet. Your phone joins
your private Tailscale network, and the bili-web service is reachable only
from devices in that network.

1. **Install Tailscale on the VPS** and sign in. Follow the official
   instructions for your distro — on Ubuntu/Debian it's roughly:

   ```bash
   curl -fsSL https://tailscale.com/install.sh | sh
   sudo tailscale up
   ```

   Open the URL it prints to authenticate.

2. **Install the Tailscale app on your iPhone** (App Store) and sign in to
   the same account. Both devices now see each other on a private network.

3. **Find your VPS's Tailscale name.** On the VPS:

   ```bash
   tailscale status
   ```

   The first line shows something like `100.x.y.z   my-vps   you@   linux ...`.
   You can use either the IP (`100.x.y.z`) or the MagicDNS name
   (`my-vps`) to reach it.

4. **(Optional but nice) Get HTTPS automatically with `tailscale serve`.**
   This gives you a `https://my-vps.<tailnet>.ts.net` URL with a real TLS
   certificate, valid only inside your tailnet:

   ```bash
   sudo tailscale serve --bg --https=443 / http://127.0.0.1:8765
   ```

   Open that URL in Safari on your phone and you're in. No DNS records,
   no Caddy, no Cloudflare, nothing public.

   Without `tailscale serve`, you can still hit the service over plain
   HTTP at `http://my-vps:8765` — fine on a private tailnet, just less
   pretty.

That's it. Skip the rest of this section if you're using Tailscale.

### Option B — Public domain with Caddy

Use this only if you have a reason to expose the service publicly. **You
need to add an authentication layer in front of it** (basic auth or
similar) — see the warning at the top of this README.

#### 1. DNS record

Pick a subdomain for the service (e.g. `bili.example.com`). At your DNS
provider, add an **A record**:

| Field | Value |
|---|---|
| Type | A |
| Name | `bili` (or whatever subdomain you picked) |
| Value / Target | your VPS's public IPv4 address |
| TTL | leave default (auto / 1h is fine) |

If your DNS provider is **Cloudflare**, set the record to
**DNS only (gray cloud)**, not proxied (orange cloud). Two reasons:

- Cloudflare's free plan caps long-running requests at 100 seconds, which
  can break HLS playback.
- The proxied mode answers the HTTP-01 ACME challenge itself, so Caddy
  cannot fetch a Let's Encrypt certificate for your domain. With the gray
  cloud, the request reaches your server directly and Caddy gets its cert.

Verify the record propagated:

```bash
dig +short bili.example.com   # should print your VPS IP
```

#### 2. Install Caddy

On Ubuntu/Debian:

```bash
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update
sudo apt install caddy
```

This installs Caddy, enables it as a systemd service, and creates
`/etc/caddy/Caddyfile`.

#### 3. Configure Caddy

Open `/etc/caddy/Caddyfile` and add a block for your subdomain (replace
`bili.example.com` with yours):

```caddyfile
bili.example.com {
    reverse_proxy localhost:8765 {
        flush_interval -1
        transport http {
            response_header_timeout 30s
        }
    }
}
```

`flush_interval -1` matters: it disables Caddy's response buffering so
video bytes stream out as soon as they arrive.

There's a copy of this snippet at `deploy/Caddyfile.snippet` you can
append directly:

```bash
sudo tee -a /etc/caddy/Caddyfile < deploy/Caddyfile.snippet
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

Caddy will fetch a Let's Encrypt certificate the first time someone hits
the URL. You can watch progress with:

```bash
sudo journalctl -u caddy -f
```

Once you see `certificate obtained successfully`, open
`https://bili.example.com` on your phone.

#### 4. Firewall

Make sure your VPS firewall (and your cloud provider's firewall, if any —
e.g. Oracle's VCN security list, AWS security groups) allows inbound TCP
on ports **80** (for the ACME HTTP-01 challenge) and **443** (for HTTPS).
You should **not** expose port 8765 publicly; only Caddy on `localhost`
talks to it.

## Configuration (`config.yaml`)

```yaml
listen: "127.0.0.1:8765"
cookies:
  sessdata: ""
  bili_jct: ""
  dede_user_id: ""
```

Leave `cookies` blank — you'll fill them in by visiting `/login` once and
scanning a QR code with the bilibili app. The values are written back to
this file (chmod 0600) on a successful login.

## Routes

| Path | What |
|---|---|
| `/` | Recommended feed |
| `/subscriptions` | Subscriptions feed |
| `/search?q=...` | Search results |
| `/favorites` | List of your fav folders |
| `/favorites/{fid}` | Contents of a fav folder |
| `/watch-later` | Watch-later queue |
| `/watch/{bvid}` | Watch page |
| `/login` | QR login (only when not authenticated) |
| `/healthz` | Liveness check |

Internal routes (you don't normally hit these directly):

- `/stream/{bvid}` — single-mp4 byte proxy with Range support
- `/hls/{bvid}/{file}` — HLS playlist + fmp4 segments
- `/img?u=...` — thumbnail/avatar proxy (allowlisted to bilibili hosts)
- `/danmaku/{bvid}?cid=...` — danmaku JSON
- `/login/start`, `/login/poll` — QR generation + status polling

## Caveats and known limits

- **WBI signing is implemented for the recommend feed only.** The other
  endpoints currently in use don't require it. If bilibili tightens that in
  the future, expect to extend `internal/bili/wbi.go` to cover more callers.
- **No `buvid3` cookie is set.** A few endpoints want one; if any feed
  starts coming back empty, harvesting `buvid3` from a single GET to
  `bilibili.com` is the fix.
- **Quality is capped at 720p** for simplicity. Lifting this requires WBI
  signing on the playurl endpoint and a 1080P+ feature flag.
- **Single active HLS session.** Starting a new multi-part video kills the
  previous ffmpeg job and clears its cache directory. Fine for one user.
- **No comments below top-level**, no posting, no liking. Read-only.
- **Comment text is rendered as plain text.** Bilibili emoji shortcodes
  like `[doge]` show literally; they're not expanded into images.
- **Danmaku in iOS native fullscreen will not appear.** The overlay is a
  sibling DOM node, not part of the `<video>` element. Tap to exit
  fullscreen if you want danmaku.
- **The server is intentionally not multi-tenant.** See the warning at the
  top of this README — anyone who reaches the URL acts as you on bilibili.

## Layout

```
cmd/bili-web/         entrypoint
internal/
  bili/               bilibili API client (search, video info, playurl,
                      DASH, comments, danmaku, favorites, watch-later,
                      QR login, WBI signing)
  config/             YAML config with thread-safe cookie persistence
  hls/                ffmpeg session manager for multi-part videos
  server/             HTTP handlers and view types
web/
  templates/          server-rendered HTML (Go html/template)
  static/             style.css, login.js, danmaku.js
deploy/               systemd unit, Caddy snippet
```

## Updating

```bash
git pull
go build -o bili-web ./cmd/bili-web
sudo systemctl restart bili-web
```

Templates and static assets are read from disk on each request, so you can
edit `web/` and refresh without a rebuild.
