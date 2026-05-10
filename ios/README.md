# BiliWeb iOS native shell

A ~60-line Swift app that wraps the bili-web frontend in a `WKWebView` and
gives it true OS-level background audio. iOS lets native apps with
`UIBackgroundModes = [audio]` keep playing when the screen is locked or the
user switches apps; web pages alone cannot get that entitlement, which is
why you'd build this even though "the app is just the website."

This is a personal-use side-installable app. It is not meant for the App
Store.

## What you'll need

- A Mac with **Xcode 15 or newer** (free from the Mac App Store; ~7 GB).
- An **Apple ID** signed into Xcode. Free is fine — see the trade-offs below.
- A **USB or USB-C cable** to plug your iPhone into the Mac. (Wireless is
  also possible after the first wired install, but wired is simplest.)
- Your iPhone running iOS 16 or newer.

### About signing — free vs paid

| | Free Apple ID | Apple Developer Program ($99/year) |
|---|---|---|
| App expires after | **7 days** — re-run from Xcode to refresh | **1 year** |
| Background audio capability | Yes | Yes |
| Cost | $0 | $99/year |
| Setup hassle | Same | Same |

Start free. If the weekly re-sign annoys you, upgrade later — your code
doesn't change.

## Step-by-step setup

### 1. Install Xcode

Open the Mac App Store, search "Xcode", install. Launch it once and accept
the license. This may take 30+ minutes the first time.

### 2. Create the project

1. **File → New → Project…**
2. Choose **iOS → App**, click Next.
3. Fill in:
   - **Product Name:** `BiliWeb`
   - **Team:** your Apple ID (it appears in the dropdown after you sign in
     under Xcode → Settings → Accounts → "+" → Apple ID).
   - **Organization Identifier:** anything like `com.yourname` (this gets
     combined with the product name into a unique bundle ID).
   - **Interface:** SwiftUI
   - **Language:** Swift
   - **Storage:** None
   - Uncheck "Include Tests" if it's there (optional).
4. Choose a folder — **outside** this `bili-web-vps` repo unless you want
   the `.xcodeproj` tracked in git. (Most people keep iOS projects in a
   separate folder.) Click Create.

### 3. Replace the generated source files

Xcode generates `BiliWebApp.swift` and `ContentView.swift`. We have replacements.

In Finder, navigate to the project folder Xcode created. Inside it you'll
see a `BiliWeb/` folder containing those two `.swift` files.

1. **Replace `BiliWebApp.swift`** with the version from this repo:
   `ios/BiliWeb/BiliWebApp.swift`
2. **Replace `ContentView.swift`** with the version from this repo:
   `ios/BiliWeb/ContentView.swift`
3. **Open `ContentView.swift` in Xcode** and change the line:
   ```swift
   let kBiliWebURL = "https://bili.example.com"
   ```
   to your actual URL — your Caddy-fronted domain or your Tailscale
   MagicDNS hostname.

### 4. Enable Background Audio capability

This is the important one — the whole reason for going native.

1. In Xcode's left sidebar, click the **blue project icon** at the top.
2. In the editor, select the **BiliWeb target** (under TARGETS).
3. Click the **Signing & Capabilities** tab.
4. Click **+ Capability** at the top-left of that tab.
5. Search "Background Modes" and double-click it to add.
6. In the Background Modes section that just appeared, **check the box for
   "Audio, AirPlay, and Picture in Picture"**.

That's it. No Info.plist editing needed in modern Xcode — the capability
button writes the entries for you.

While you're on this tab, also confirm:
- **"Automatically manage signing"** is checked.
- The **Team** dropdown shows your Apple ID.

### 5. Plug in your iPhone and run

1. Plug your iPhone into the Mac with a cable.
2. The first time, your iPhone will pop a "Trust this computer?" dialog.
   Tap Trust and enter your passcode.
3. In Xcode, at the top center, click the device selector (it says
   something like "Any iOS Device" or a simulator name) and pick **your
   iPhone**.
4. Hit the ▶ Play button (top-left) or press **Cmd-R**.

Xcode will build, sign with your Apple ID, push the binary to your phone,
and try to launch it.

### 6. Trust the developer profile (one-time, per Apple ID)

The first run will fail with an error like *"Untrusted Developer."*
Standard for free signing.

On the iPhone:

1. **Settings → General → VPN & Device Management** (sometimes just
   "Device Management" depending on iOS version).
2. Under **DEVELOPER APP**, tap your Apple ID email.
3. Tap **Trust "your-apple-id"**, then Trust again on the confirmation.

Now go back to Xcode, hit Run again. The app launches.

### 7. Verify background audio works

1. Open the app. The web frontend loads.
2. Sign in (QR scan if you haven't already — the cookie persists between
   launches, same as Safari).
3. Play any video.
4. Press the side button to **lock the phone**.
5. The audio should keep playing. You should also see the title +
   thumbnail on the lock screen with play/pause controls.

If audio cuts when you lock — the most common reason is that the
Background Modes capability didn't get checked correctly. Repeat step 4.

## Re-installing weekly (free signing only)

Free-signed apps stop working after 7 days. To refresh:

1. Plug the phone in.
2. Open the project in Xcode.
3. Hit Run.
4. Done — same as the initial install but takes ~10 seconds.

If you'd rather not do this manually, look at **AltStore** (or
**SideStore**), which automates the re-sign in the background. Setup is
its own little adventure; not covered here.

## Files in this directory

- `BiliWeb/BiliWebApp.swift` — `@main` entry, configures AVAudioSession.
- `BiliWeb/ContentView.swift` — SwiftUI view with the WKWebView.

These are designed to be **drag-and-drop replacements** for what Xcode's
new-project wizard generates. There's deliberately no `.xcodeproj` in this
directory: project files have machine-specific paths and are unfriendly to
share via git.

## Caveats

- **Cookie storage:** WKWebView keeps its own cookies separately from
  Safari's. The first time you open the app, you'll need to scan the QR
  login again.
- **Updating the URL:** if your bili-web hostname changes, edit
  `ContentView.swift` and rebuild.
- **No Now Playing artwork bridge yet:** iOS picks up the title and
  artwork from the web page's MediaSession metadata, which the web app
  already sets. If lock-screen artwork ever doesn't appear, that's where
  to look — not in the Swift code.
- **Picture-in-Picture:** the Background Modes capability declared above
  enables PiP at the OS level, but the web's native `<video>` controls
  drive whether a PiP button appears. Tap it from the player when needed.
