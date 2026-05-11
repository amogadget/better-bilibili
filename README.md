# bili-web

A small, single-user, mobile-first web frontend for bilibili. Designed to
be self-hosted on a VPS so you can browse and watch from a device where
the official app is unavailable, restricted, or simply unpleasant to use
(iOS being the original target).

The bilibili mobile website is intentionally crippled to push users into
the app, and the app is increasingly ad-heavy. This is a thin proxy that
talks to bilibili's web APIs on your behalf and serves a clean, ad-free
interface tailored for a phone screen.

> [!WARNING]
> **Read this before you deploy.** This server stores your real bilibili
> cookies in a config file and uses them on every request. **Anyone who
> can reach the URL gets to act as you on bilibili** — read your DMs,
> change account settings, etc.
>
> Do not expose this on the public internet without authentication in
> front of it. The recommended setup is to run it on a private network
> with [Tailscale](https://tailscale.com) so only your own devices can
> reach it. If you must expose it publicly, put HTTP basic auth on the
> reverse proxy.
>
> This client also uses bilibili's web APIs without permission from
> bilibili. They may change endpoints at any time, and aggressive use
> against the public APIs can get your account rate-limited or banned.
> Keep traffic personal-scale.

## Table of contents

- [What it does](#what-it-does)
- [Status](#status)
- [Architecture](#architecture)
- [Quick start](#quick-start)
- [Installation](#installation)
  - [Build and run](#build-and-run)
  - [Run as a systemd service](#run-as-a-systemd-service)
- [Deployment: pick one](#deployment-pick-one)
  - [Option A — Tailscale (recommended)](#option-a--tailscale-recommended-for-personal-use)
  - [Option B — Public domain with Caddy](#option-b--public-domain-with-caddy)
- [iOS native app](#ios-native-app)
- [Configuration](#configuration)
- [Routes reference](#routes-reference)
- [Caveats and known limits](#caveats-and-known-limits)
- [Repo layout](#repo-layout)
- [Updating](#updating)

## What it does

- Recommended feed (bilibili's algorithmic homepage)
- Subscriptions feed (videos from creators you follow)
- Search
- Favorites (收藏夹) — folder list and contents
- Watch later (稍后再看)
- Watch page with native HTML5 `<video>` playback (capped at 720p)
  - Single-part videos stream as plain mp4 directly from bilibili's CDN
    (instant start)
  - Multi-part videos are remuxed on-the-fly to HLS by `ffmpeg` and served
    seamlessly as one continuous stream
  - Creator series (合集 / `ugc_season`) sidebar with episode navigation
  - Top-level comments
  - Danmaku (弹幕) overlay synced to playback, with a toggle
- Pagination on every list page
- QR-code login — scan with the bilibili app on your phone to grant the
  server access. Cookies persist on disk

## Status

- Single user (one set of cookies, stored server-side).
- Read-only: browse, search, watch. No posting, liking, or commenting.
- Tested on iPhone Safari. Should work on any modern mobile browser.

## Architecture

```
iPhone Safari
     │
     ▼   HTTPS
  Caddy   (TLS termination, reverse proxy)
     │
     ▼   HTTP on 127.0.0.1
  bili-web   (Go binary, ~7 MB, single static executable)
     │          │
     │          └── ffmpeg subprocess (only for multi-part videos)
     ▼
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

## Quick start

If you've done this kind of thing before, the short version:

```bash
git clone <your-fork-url> /opt/bili-web
cd /opt/bili-web
go build -o bili-web ./cmd/bili-web
cp config.yaml.example config.yaml && chmod 600 config.yaml
./bili-web -config config.yaml          # listens on 127.0.0.1:8765
```

Then pick a [deployment option](#deployment-pick-one) (Tailscale or Caddy)
and visit `/login` once to scan the QR code from the bilibili app.

The rest of this README is the detailed walk-through.

## Requirements

- Linux server (any distro). Tested on Ubuntu 24.04 / arm64.
- **Go 1.22 or newer** for the build.
- **ffmpeg** for multi-part videos (skippable if every video you watch is
  short enough to be served as a single mp4).
- A way to reach the server from your phone — see
  [Deployment](#deployment-pick-one).

## Installation

### Build and run

```bash
git clone <your-fork-url> /opt/bili-web
cd /opt/bili-web
go build -o bili-web ./cmd/bili-web
cp config.yaml.example config.yaml
chmod 600 config.yaml
./bili-web -config config.yaml
```

It listens on `127.0.0.1:8765` by default and reads working files relative
to the current directory (`web/` for templates and static assets,
`cache/hls/` for ffmpeg HLS sessions).

### Run as a systemd service

A reference unit file is at `deploy/bili-web.service`. Adjust paths, copy
to `/etc/systemd/system/`:

```bash
sudo cp deploy/bili-web.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now bili-web
```

Confirm it's listening:

```bash
curl http://127.0.0.1:8765/healthz
```

That's a working server reachable on `localhost`. The next section is
about how to reach it from your phone.

## Deployment: pick one

### Option A — Tailscale (recommended for personal use)

Your server stays invisible to the public internet. Your phone joins your
private Tailscale network, and the bili-web service is reachable only
from devices in that network.

1. **Install Tailscale on the VPS.** Follow the official instructions for
   your distro — on Ubuntu/Debian:

   ```bash
   curl -fsSL https://tailscale.com/install.sh | sh
   sudo tailscale up
   ```

   Open the URL it prints to authenticate.

2. **Install the Tailscale app on your iPhone** (App Store) and sign in
   to the same account.

3. **Find your VPS's Tailscale name:**

   ```bash
   tailscale status
   ```

   The first line shows something like `100.x.y.z   my-vps   you@   linux ...`.
   You can use either the IP or the MagicDNS name (`my-vps`).

4. **(Optional but nice) Get HTTPS with `tailscale serve`** — gives you a
   `https://my-vps.<tailnet>.ts.net` URL with a real TLS certificate,
   valid only inside your tailnet:

   ```bash
   sudo tailscale serve --bg --https=443 / http://127.0.0.1:8765
   ```

Without `tailscale serve`, you can still hit the service over plain HTTP
at `http://my-vps:8765` — fine on a private tailnet.

That's it. Skip the rest of this section if you're using Tailscale.

### Option B — Public domain with Caddy

Use this only if you have a reason to expose the service publicly. **You
need to add an authentication layer in front of it** — see the warning
at the top.

#### 1. DNS record

At your DNS provider, add an **A record** pointing at your VPS's public
IPv4:

| Field | Value |
|---|---|
| Type | A |
| Name | `bili` (or whatever subdomain you pick) |
| Value | your VPS's public IPv4 |
| TTL | leave default |

If your DNS provider is **Cloudflare**, set the record to **DNS only
(gray cloud)**, not proxied (orange cloud) — Cloudflare's free plan caps
long-running requests at 100s (breaks HLS), and proxied mode prevents
Caddy from completing the HTTP-01 ACME challenge.

Verify:

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

#### 3. Configure Caddy

Open `/etc/caddy/Caddyfile` and add a block (replace `bili.example.com`):

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

A copy of this snippet is at `deploy/Caddyfile.snippet`:

```bash
sudo tee -a /etc/caddy/Caddyfile < deploy/Caddyfile.snippet
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

Caddy will fetch a Let's Encrypt certificate the first time someone hits
the URL. Watch progress with `sudo journalctl -u caddy -f`. Once you see
`certificate obtained successfully`, open `https://bili.example.com` on
your phone.

#### 4. Firewall

Allow inbound TCP on ports **80** (ACME HTTP-01) and **443** (HTTPS).
You should **not** expose port 8765 publicly; only Caddy on `localhost`
talks to it.

## iOS native app

A thin native shell that wraps the web frontend in a `WKWebView` and gives
it true OS-level background audio (the web alone can't get the
background-audio entitlement).

See [`ios/README.md`](ios/README.md) for the full Xcode setup walkthrough
and the architectural deep-dive on how the AVPlayer audio bridge and the
"is this pause from the user or from iOS?" gesture-tracking logic work.

## Configuration

`config.yaml`:

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

## Routes reference

User-facing:

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

Internal (you don't normally hit these directly):

- `/stream/{bvid}` — single-mp4 byte proxy with Range support
- `/hls/{bvid}/{file}` — HLS playlist + fmp4 segments
- `/img?u=...` — thumbnail/avatar proxy (allowlisted to bilibili hosts)
- `/danmaku/{bvid}?cid=...` — danmaku JSON
- `/login/start`, `/login/poll` — QR generation + status polling

## Caveats and known limits

- **WBI signing is implemented for the recommend feed only.** Other
  endpoints in use don't currently require it. If bilibili tightens that,
  extend `internal/bili/wbi.go` to cover more callers.
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
- **The server is intentionally not multi-tenant.** See the warning at
  the top — anyone who reaches the URL acts as you on bilibili.

## Repo layout

```
cmd/bili-web/         entry point
internal/
  bili/               bilibili API client (search, video info, playurl,
                      DASH, comments, danmaku, favorites, watch-later,
                      QR login, WBI signing)
  config/             YAML config with thread-safe cookie persistence
  hls/                ffmpeg session manager for multi-part videos
  server/             HTTP handlers and view types
web/
  templates/          server-rendered HTML (Go html/template)
  static/             CSS, JS (login, danmaku, audio fallback, native
                      bridge for iOS shell, fullscreen gesture, etc.)
ios/                  native iOS shell sources + setup README
deploy/               systemd unit, Caddy snippet
```

## Updating

```bash
git pull
go build -o bili-web ./cmd/bili-web
sudo systemctl restart bili-web
```

Templates and static assets are read from disk on each request, so you
can edit `web/` and refresh without a rebuild. The `/static/` route
serves `Cache-Control: no-cache, must-revalidate` so WKWebView / Safari
always revalidate freshness — important when iterating on JS that the
iOS shell loads.
