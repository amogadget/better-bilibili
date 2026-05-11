# BiliWeb iOS native shell

A ~300-line Swift app that wraps the bili-web frontend in a `WKWebView` and
gives it true OS-level background audio. iOS lets native apps with
`UIBackgroundModes = [audio]` keep playing when the screen is locked or the
user switches apps; web pages alone cannot get that entitlement, which is
why you'd build this even though "the app is just the website."

Personal-use, side-installable. Not meant for the App Store.

## Table of contents

- [What you need](#what-you-need)
- [Setup](#setup)
  1. [Install Xcode](#1-install-xcode)
  2. [Create the project](#2-create-the-project)
  3. [Drop in our source files](#3-drop-in-our-source-files)
  4. [Enable capabilities](#4-enable-capabilities)
  5. [Run on your iPhone](#5-run-on-your-iphone)
  6. [Trust the developer profile](#6-trust-the-developer-profile-once-per-apple-id)
  7. [Verify background audio](#7-verify-background-audio)
- [How background audio works](#how-background-audio-works)
- [Troubleshooting](#troubleshooting)
- [Re-installing weekly (free signing only)](#re-installing-weekly-free-signing-only)
- [Files in this directory](#files-in-this-directory)
- [Caveats](#caveats)

## What you need

- A Mac with **Xcode 15 or newer** (free from the Mac App Store, ~7 GB).
- An **Apple ID** signed into Xcode. Free is fine — see the table below.
- A **USB or USB-C cable** to plug your iPhone into the Mac.
- Your iPhone running **iOS 16 or newer**.

### Free vs paid signing

| | Free Apple ID | Apple Developer Program ($99/year) |
|---|---|---|
| App expires after | **7 days** — re-run from Xcode | **1 year** |
| Background audio capability | Yes | Yes |
| Setup hassle | Same | Same |

Start free. If the weekly re-sign annoys you, upgrade — your code doesn't
change.

## Setup

### 1. Install Xcode

Mac App Store → search "Xcode" → install. Launch it once and accept the
license. First-time install can take 30+ minutes.

### 2. Create the project

1. **File → New → Project…**
2. **iOS → App**, click Next.
3. Fill in:
   - **Product Name:** `BiliWeb`
   - **Team:** your Apple ID (add it under Xcode → Settings → Accounts → "+"
     if it's not in the dropdown).
   - **Organization Identifier:** `com.yourname` (combined with the product
     name into a unique bundle ID).
   - **Interface:** SwiftUI · **Language:** Swift · **Storage:** None.
   - Uncheck "Include Tests" if shown.
4. Pick a folder **outside** this repo, unless you want the `.xcodeproj`
   tracked in git. Click Create.

### 3. Drop in our source files

Xcode generates `BiliWebApp.swift` and `ContentView.swift`. We have
replacements, plus one extra file. In Finder, navigate to the `BiliWeb/`
folder inside the new Xcode project:

1. **Replace** `BiliWebApp.swift` with `ios/BiliWeb/BiliWebApp.swift` from
   this repo.
2. **Replace** `ContentView.swift` with `ios/BiliWeb/ContentView.swift`.
3. **Add** `BiliPlayer.swift`:
   - Copy `ios/BiliWeb/BiliPlayer.swift` into the same `BiliWeb/` folder.
   - In Xcode: **File → Add Files to "BiliWeb"…**, pick the file, leave
     "Copy items if needed" unchecked, ensure the BiliWeb target is checked.
4. **Open `ContentView.swift`** in Xcode and change:
   ```swift
   let kBiliWebURL = "https://bili.example.com"
   ```
   to your actual URL (Caddy domain or Tailscale MagicDNS hostname).

### 4. Enable capabilities

#### Background Modes — Audio

This is the important one — the whole reason for going native.

1. Click the **blue project icon** in the left sidebar.
2. Select the **BiliWeb target** under TARGETS.
3. Open **Signing & Capabilities**.
4. **+ Capability** → search "Background Modes" → double-click to add.
5. In the new Background Modes section, **check "Audio, AirPlay, and
   Picture in Picture"**.

While you're here, confirm **Automatically manage signing** is on and the
**Team** dropdown shows your Apple ID.

#### Landscape orientations

Without this, fullscreen video won't rotate.

1. Same target → **General** tab.
2. Scroll to **Deployment Info → iPhone Orientation**.
3. Check **Portrait**, **Landscape Left**, and **Landscape Right** (Upside
   Down optional).

### 5. Run on your iPhone

1. Plug the iPhone into the Mac. On first plug-in, tap **Trust this
   computer** on the phone.
2. In Xcode's top center, click the device dropdown and pick **your
   iPhone**.
3. Press **⌘R** or the ▶ Play button.

Xcode will build, sign, push to your phone, and try to launch.

### 6. Trust the developer profile (once per Apple ID)

The first run usually fails with *"Untrusted Developer."* Standard for free
signing. On the iPhone:

1. **Settings → General → VPN & Device Management** (or "Device
   Management" on some iOS versions).
2. Under **DEVELOPER APP**, tap your Apple ID email.
3. **Trust "your-apple-id"** → confirm.

Back in Xcode, hit Run again. The app launches.

### 7. Verify background audio

1. Open the app, sign in (QR scan if needed — cookie persists between
   launches).
2. Play any video.
3. Lock the phone (side button).
4. Audio should keep playing. Lock screen shows title, thumbnail, and
   play/pause controls.

If audio cuts when you lock or swipe home, see [Troubleshooting](#troubleshooting).

## How background audio works

WKWebView runs the page in a separate WebContent process that iOS suspends
when the app backgrounds, regardless of host-app `UIBackgroundModes`. So
the page's own `<audio>`/`<video>` can't be the audio source during lock.

Instead, the host app runs an **AVPlayer** in the host process, pointed at
the same stream URL. The web page mutes its `<video>` and pushes state
(currentTime, playing, metadata) over the `player` channel; AVPlayer
mirrors that state continuously, so backgrounding causes zero audio gap.

```
foreground           background           foreground again
─────────            ──────────           ───────────────
WKWebView <video>  →  AVPlayer (host)  →  WKWebView <video>
plays + pushes        keeps playing       seeks to AVPlayer
state messages        in host process     currentTime, plays
```

**Audio session ownership is paired with AVPlayer's state.** When AVPlayer
plays, `BiliPlayer.claimAudioSession()` activates `AVAudioSession` and
starts a silent AVAudioEngine loop (the `SilenceKeeper` — needed because
iOS only treats the host as a "media app" while it's actively producing
audio, and that's what lets WKWebView's audio piggyback on the background
entitlement). When AVPlayer pauses, `releaseAudioSession()` stops silence
and calls `setActive(false, options: .notifyOthersOnDeactivation)` so the
Bluetooth route immediately frees up for other apps.

**Distinguishing user pauses from system pauses.** When iOS backgrounds
the app, it auto-pauses the muted `<video>`, firing a `pause` event
identical to the user tapping pause. Every lifecycle signal we could gate
on (visibilityState, applicationState, willResignActive) arrives *after*
that pause event. The one signal that beats it: WebKit fires
`touchcancel` when iOS claims an in-progress touch as a system gesture
(home-indicator swipe, control-center pull). The JS layer tags each
state push with `userInitiated` — true if a JS gesture is recent AND
hasn't been canceled — and Swift drops any pause with
`userInitiated=false`. See `native-bridge.js` for the gesture tracker
and `BiliPlayer.update(state:)` for the drop logic.

The web page only does its part if it sees `window.webkit.messageHandlers
.player` (i.e. it's inside this app, not Safari). In Safari the page
falls back to the dual-`<audio>` trick from `audio-mode.js`.

## Troubleshooting

If audio cuts when you lock or swipe home, work through these in order:

1. **Background Modes capability is enabled.** Recheck step 4 above. After
   any change here, stop the app on the phone and re-run from Xcode —
   capability changes only take effect in a fresh build.
2. **You're running the latest build.** Editing Swift requires a rebuild
   + reinstall from Xcode (`⌘R`). Editing JS in `web/static/` is picked
   up on next page load — but make sure `Cache-Control: no-cache` is set
   on `/static/` (see the Go server's `noCacheHandler`).
3. **The audio session activates successfully.** Watch Xcode's console
   on launch and during the first play; `claimAudioSession setActive
   failed:` or `silence engine failed to start:` indicate a setup
   problem.
4. **Other audio apps can claim Bluetooth while paused.** Play a video,
   then pause. Open Spotify (or any audio app) — it should be able to
   play through your BT headphones. If not, the
   `releaseAudioSession` path isn't running; check the pause handler
   in `BiliPlayer.update(state:)`.

## Re-installing weekly (free signing only)

Free-signed apps stop working after 7 days. To refresh:

1. Plug the phone in.
2. Open the project in Xcode.
3. Press **⌘R**. Done.

If you'd rather not do this manually, look at **AltStore** or **SideStore**
which automate the re-sign in the background. Setup is its own little
adventure — not covered here.

## Files in this directory

- **`BiliWeb/BiliWebApp.swift`** — `@main` entry. Configures
  `AVAudioSession` and holds the `SilenceKeeper` instance. The silence
  loop is only started when AVPlayer actually plays (not at launch), so
  the audio session is held only during real playback.
- **`BiliWeb/ContentView.swift`** — SwiftUI view with the `WKWebView` and
  the `WKScriptMessageHandler` glue for the `player` channel. Also
  handles the orientation-lock request from the swipe-up gesture.
- **`BiliWeb/BiliPlayer.swift`** — `AVPlayer`-backed audio bridge. Mirrors
  web state into AVPlayer, drives `claimAudioSession`/`releaseAudioSession`
  around play/pause, feeds Now Playing center, and handles remote
  control commands. Drops involuntary (non-`userInitiated`) pauses so iOS
  auto-pauses on backgrounding don't halt the background audio.

These are designed to be **drag-and-drop replacements (and additions)** for
what Xcode's new-project wizard generates. There's deliberately no
`.xcodeproj` in this directory: project files have machine-specific paths
and are unfriendly to share via git.

## Caveats

- **Login state is shared with Safari.** The bilibili session lives on
  *the server* (in `config.yaml`), not in your browser cookies. Once
  you've QR-logged-in from any client, every other client of the same
  server sees you as logged in. You should not need to re-scan when
  first opening the app.
- **Updating the URL:** if your bili-web hostname changes, edit
  `ContentView.swift` and rebuild.
- **No Now Playing artwork bridge yet:** iOS picks up the title and
  artwork from the page's MediaSession metadata. If lock-screen artwork
  doesn't appear, that's where to look — not the Swift code.
- **Picture-in-Picture:** the Background Modes capability enables PiP at
  the OS level, but whether a PiP button appears is driven by the web's
  native `<video>` controls.
- **When paused, the lock-screen media controls may disappear** because
  no app holds the active audio session (the trade-off that lets other
  apps claim Bluetooth audio while you're paused). Resume by unlocking
  and tapping play in-app.
