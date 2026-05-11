// BiliPlayer.swift
//
// Native-AVPlayer-driven audio for the iOS shell. Mirrors the YouTube
// architecture: AVPlayer is the *primary* audio source from the moment a
// video starts. The web's visible <video> element runs muted (just for
// pixels). Because AVPlayer is actively producing audio, iOS treats the
// host app as a real media app and keeps it running through the
// screen-lock and app-switch transitions with zero startup gap.
//
// State flow:
//   - Web pushes its currentTime/play state on every event + a 2s tick;
//     BiliPlayer mirrors that into AVPlayer.
//   - On foreground transition, BiliPlayer reads AVPlayer's currentTime
//     and tells the web to seek there so the visible <video> picks up
//     exactly where audio kept reaching.
//
// Audio session ownership is paired with AVPlayer's play/pause state:
// when AVPlayer plays, we activate AVAudioSession and start the silence
// loop (so iOS knows we're producing audio). When AVPlayer pauses, we
// release the session with .notifyOthersOnDeactivation so other audio
// apps can claim the Bluetooth route. Without this, holding the session
// permanently means BT stays bound to us forever — including while
// "paused" — and other apps can't make sound.
//
// Distinguishing user pauses from iOS auto-pauses (when WKWebView's
// <video> is paused on backgrounding) is done in the JS layer: each
// state push carries a `userInitiated` flag. We drop any incoming pause
// with userInitiated=false; see the comment on `update(state:)` below.

import AVFoundation
import MediaPlayer
import UIKit
import WebKit

final class BiliPlayer: NSObject {

    struct State {
        let src: URL
        let currentTime: Double
        let duration: Double
        let playing: Bool
        /// True when the JS layer saw a recent user gesture (tap, click,
        /// key). The iOS home-indicator swipe is a system gesture: WebKit
        /// fires touchstart for it, but then touchcancel when iOS claims
        /// the gesture — JS zeros out gesture credit on touchcancel, so a
        /// pause arriving with userInitiated=false is the OS auto-pausing
        /// the muted <video>, not the user.
        let userInitiated: Bool
        let title: String
        let artist: String
        let artworkURL: URL?
    }

    private var player: AVPlayer?
    private var lastState: State?
    private weak var webView: WKWebView?
    private var artworkCacheKey: String?
    private var nowPlayingArtwork: MPMediaItemArtwork?

    override init() {
        super.init()
        registerLifecycleObservers()
        setupRemoteCommands()
    }

    func attach(webView: WKWebView) {
        self.webView = webView
    }

    func update(state: State) {
        // Drop iOS auto-pauses. When the app backgrounds, iOS pauses the
        // muted WebKit <video> and the resulting pause event fires before
        // any lifecycle signal we could gate on (visibilityState,
        // applicationState, willResignActive). The only way to tell user
        // pauses from system pauses is the JS-side `userInitiated` flag.
        //
        // Preserve lastState.playing on the dropped path so NowPlayingInfo
        // doesn't get corrupted by the bogus pause.
        if !state.userInitiated && !state.playing {
            if let prev = lastState {
                lastState = State(
                    src: state.src,
                    currentTime: state.currentTime,
                    duration: state.duration,
                    playing: prev.playing,
                    userInitiated: prev.userInitiated,
                    title: state.title,
                    artist: state.artist,
                    artworkURL: state.artworkURL
                )
            } else {
                lastState = state
            }
            refreshNowPlayingInfo()
            return
        }

        let currentURL = (player?.currentItem?.asset as? AVURLAsset)?.url
        if currentURL != state.src {
            replaceItem(with: state.src, startAt: state.currentTime)
        } else if let player = player {
            // Same source — only re-seek on noticeable drift, otherwise
            // this would jitter every 2s state push.
            let nativeTime = player.currentTime().seconds
            if abs(nativeTime - state.currentTime) > 1.0 {
                player.seek(to: CMTime(seconds: state.currentTime, preferredTimescale: 1000))
            }
        }

        player?.isMuted = false
        if state.playing {
            if player?.rate == 0 {
                claimAudioSession()
                player?.play()
            }
        } else {
            if player?.rate != 0 {
                player?.pause()
                releaseAudioSession()
            }
        }

        lastState = state
        refreshNowPlayingInfo()
    }

    /// Activate the audio session and start the silence keeper. Called
    /// when AVPlayer is about to play. Holds iOS's media-app status only
    /// for the duration of actual playback so we don't permanently bind
    /// the Bluetooth route.
    private func claimAudioSession() {
        do {
            try AVAudioSession.sharedInstance().setActive(true)
        } catch {
            print("BiliWeb: claimAudioSession setActive failed: \(error)")
        }
        BiliWebApp.silence.start()
    }

    /// Stop the silence keeper and deactivate the audio session.
    /// `.notifyOthersOnDeactivation` wakes other audio apps so they can
    /// take the route immediately.
    private func releaseAudioSession() {
        BiliWebApp.silence.stop()
        do {
            try AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
        } catch {
            print("BiliWeb: releaseAudioSession setActive(false) failed: \(error)")
        }
    }

    private func replaceItem(with url: URL, startAt seconds: Double) {
        let item = AVPlayerItem(url: url)
        if let player = player {
            player.replaceCurrentItem(with: item)
        } else {
            player = AVPlayer(playerItem: item)
        }
        player?.automaticallyWaitsToMinimizeStalling = true
        if seconds > 0 {
            player?.seek(to: CMTime(seconds: seconds, preferredTimescale: 1000))
        }
    }

    // MARK: - App lifecycle

    private func registerLifecycleObservers() {
        let nc = NotificationCenter.default
        nc.addObserver(self, selector: #selector(willEnterForeground),
                       name: UIApplication.willEnterForegroundNotification, object: nil)
        nc.addObserver(self, selector: #selector(handleInterruption),
                       name: AVAudioSession.interruptionNotification, object: nil)
        // Natural end-of-stream. AVPlayer doesn't auto-release the session
        // or update NowPlayingInfo on its own — without this, the lock
        // screen keeps showing the video as "playing" after it's finished.
        nc.addObserver(self, selector: #selector(itemDidPlayToEnd),
                       name: .AVPlayerItemDidPlayToEndTime, object: nil)
    }

    @objc private func itemDidPlayToEnd(_ notification: Notification) {
        // Filter to our own item (the notification is global with object:nil).
        guard let ended = notification.object as? AVPlayerItem,
              ended === player?.currentItem else { return }
        player?.pause()
        releaseAudioSession()
        refreshNowPlayingInfo()
    }

    /// Stops playback and releases everything. Called by ContentView's
    /// navigation delegate when the WKWebView navigates to a new page —
    /// the user clicked a link away from the watch page, AVPlayer should
    /// stop following along. Safe to call even when no player exists
    /// (initial app load, etc.).
    func stop() {
        guard player != nil else { return }
        player?.pause()
        player?.replaceCurrentItem(with: nil)
        releaseAudioSession()
        MPNowPlayingInfoCenter.default().nowPlayingInfo = nil
        lastState = nil
        nowPlayingArtwork = nil
        artworkCacheKey = nil
    }

    @objc private func willEnterForeground() {
        guard let player = player else { return }
        // AVPlayer was the audio source while we were backgrounded. Tell
        // the web <video> to seek to where AVPlayer reached so the visible
        // frame catches up to the audio. If the user paused (via remote
        // control or otherwise) during background, just sync position
        // without resuming.
        let resumeAt = player.currentTime().seconds
        if player.rate > 0 {
            let js = "window.__biliResume && window.__biliResume(\(resumeAt));"
            webView?.evaluateJavaScript(js, completionHandler: nil)
        } else {
            let js = "window.__biliSync && window.__biliSync(\(resumeAt));"
            webView?.evaluateJavaScript(js, completionHandler: nil)
        }
    }

    @objc private func handleInterruption(_ notification: Notification) {
        guard let userInfo = notification.userInfo,
              let raw = userInfo[AVAudioSessionInterruptionTypeKey] as? UInt,
              let type = AVAudioSession.InterruptionType(rawValue: raw)
        else { return }

        if type == .ended,
           let optsRaw = userInfo[AVAudioSessionInterruptionOptionKey] as? UInt {
            let opts = AVAudioSession.InterruptionOptions(rawValue: optsRaw)
            if opts.contains(.shouldResume) {
                claimAudioSession()
                player?.play()
            }
        }
    }

    // MARK: - Now Playing

    private func refreshNowPlayingInfo() {
        guard let state = lastState else { return }
        let elapsed = player?.currentTime().seconds ?? state.currentTime
        let isPlaying = (player?.rate ?? 0) > 0

        var info: [String: Any] = [:]
        info[MPMediaItemPropertyTitle] = state.title
        info[MPMediaItemPropertyArtist] = state.artist
        info[MPNowPlayingInfoPropertyElapsedPlaybackTime] = elapsed
        if state.duration.isFinite, state.duration > 0 {
            info[MPMediaItemPropertyPlaybackDuration] = state.duration
        }
        info[MPNowPlayingInfoPropertyPlaybackRate] = isPlaying ? 1.0 : 0.0
        info[MPNowPlayingInfoPropertyDefaultPlaybackRate] = 1.0
        if let art = nowPlayingArtwork {
            info[MPMediaItemPropertyArtwork] = art
        }
        MPNowPlayingInfoCenter.default().nowPlayingInfo = info

        if let url = state.artworkURL, url.absoluteString != artworkCacheKey {
            fetchArtwork(url)
        }
    }

    private func fetchArtwork(_ url: URL) {
        artworkCacheKey = url.absoluteString
        URLSession.shared.dataTask(with: url) { [weak self] data, _, _ in
            guard let self = self,
                  let data = data,
                  let image = UIImage(data: data) else { return }
            let art = MPMediaItemArtwork(boundsSize: image.size) { _ in image }
            DispatchQueue.main.async {
                self.nowPlayingArtwork = art
                self.refreshNowPlayingInfo()
            }
        }.resume()
    }

    // MARK: - Remote control (lock screen + Control Center)

    private func setupRemoteCommands() {
        let cc = MPRemoteCommandCenter.shared()
        cc.playCommand.addTarget { [weak self] _ in
            self?.claimAudioSession()
            self?.player?.play()
            self?.evalOnWeb("document.querySelector('#player-video')?.play();")
            self?.refreshNowPlayingInfo()
            return .success
        }
        cc.pauseCommand.addTarget { [weak self] _ in
            self?.player?.pause()
            self?.evalOnWeb("document.querySelector('#player-video')?.pause();")
            self?.releaseAudioSession()
            self?.refreshNowPlayingInfo()
            return .success
        }
        cc.changePlaybackPositionCommand.addTarget { [weak self] event in
            guard let self = self,
                  let positionEvent = event as? MPChangePlaybackPositionCommandEvent
            else { return .commandFailed }
            self.player?.seek(to: CMTime(seconds: positionEvent.positionTime, preferredTimescale: 1000))
            self.evalOnWeb("var v=document.querySelector('#player-video'); if(v)v.currentTime=\(positionEvent.positionTime);")
            return .success
        }
    }

    private func evalOnWeb(_ js: String) {
        DispatchQueue.main.async { [weak self] in
            self?.webView?.evaluateJavaScript(js, completionHandler: nil)
        }
    }
}
