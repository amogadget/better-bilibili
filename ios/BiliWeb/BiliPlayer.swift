// BiliPlayer.swift
//
// Native-AVPlayer-driven audio for the iOS shell. Mirrors the YouTube
// architecture: AVPlayer is the *primary* audio source from the moment a
// video starts. The web's visible <video> element runs muted (just for
// pixels). Because AVPlayer is always actively producing audio, iOS treats
// the host app as a real media app and keeps it running through the
// screen-lock and app-switch transitions with zero startup gap.
//
// Earlier iterations tried to keep AVPlayer muted in foreground and unmute
// on background. That fails in practice because muted AVPlayer often
// doesn't actually buffer or play — iOS lazy-loads it — so the unmute on
// background turns into a 1–2 second cold start. Driving audio through
// AVPlayer the whole time avoids that entirely.
//
// We still serve playback state both ways:
//   - Web pushes its currentTime/play state on every event + a 2s tick;
//     BiliPlayer mirrors that into AVPlayer.
//   - On foreground transition, BiliPlayer reads AVPlayer's currentTime
//     and tells the web to seek there so the visible <video> picks up
//     exactly where audio kept reaching.

import AVFoundation
import MediaPlayer
import UIKit
import WebKit

final class BiliPlayer: NSObject {

    // Bump this whenever you edit this file. The init() print makes it
    // appear in Xcode's console on launch so you can confirm a fresh build
    // is actually running on the device (vs. a stale install).
    private static let buildTag = "BiliPlayer 2026-05-11/attempt8"

    struct State {
        let src: URL
        let currentTime: Double
        let duration: Double
        let playing: Bool
        // True when the JS layer saw a recent user gesture (tap, click, key).
        // iOS's home-indicator swipe is a system gesture below the safe area
        // and produces NO JS input events, so an arriving pause with
        // userInitiated=false is the OS auto-pausing — not the user.
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
    private var resigningActive = false
    private var rateObservation: NSKeyValueObservation?

    override init() {
        super.init()
        print("BiliWeb: \(BiliPlayer.buildTag) init")
        registerLifecycleObservers()
        setupRemoteCommands()
    }

    func attach(webView: WKWebView) {
        self.webView = webView
    }

    func update(state: State) {
        let appState = UIApplication.shared.applicationState.rawValue
        print("BiliWeb: update(state) playing=\(state.playing) userInit=\(state.userInitiated) t=\(state.currentTime) resigning=\(resigningActive) appState=\(appState)")

        // Attempt 5 guard. Earlier rounds keyed off visibilityState /
        // applicationState / resigningActive — but the 2026-05-11 logs proved
        // the iOS auto-pause `pause` event fires BEFORE any of those signals,
        // so they all arrive too late. The only signal that beats the pause
        // is the absence of a JS gesture (the home-indicator swipe is a
        // system gesture and never enters the page's input pipeline).
        //
        // Rules:
        //   - state.userInitiated == true: always honor. The user tapped
        //     play/pause/seek; AVPlayer should mirror exactly, even mid-
        //     resign — this is what fixes Bug 3 (paused → lock shows
        //     playing) when the user pauses immediately before locking.
        //   - state.userInitiated == false AND we're trying to pause: this
        //     is iOS auto-pausing the muted <video> on backgrounding. Drop
        //     it. AVPlayer keeps playing in the background as designed.
        //   - state.userInitiated == false AND playing == true: passthrough
        //     for the periodic 2s tick (currentTime sync, NowPlaying refresh).
        //
        // We deliberately do NOT update lastState.playing on a dropped pause —
        // doing so would corrupt the source of truth and cause Bug 3
        // (paused-on-lock-shows-playing) by mirroring the bogus pause into
        // NowPlayingInfo.
        if !state.userInitiated && !state.playing {
            print("BiliWeb: update(state) involuntary pause dropped (no user gesture)")
            // Sync everything EXCEPT playing — preserve last known intent.
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
            // Same source — only re-seek on noticeable drift, otherwise this
            // would jitter every 2-second state push.
            let nativeTime = player.currentTime().seconds
            if abs(nativeTime - state.currentTime) > 1.0 {
                player.seek(to: CMTime(seconds: state.currentTime, preferredTimescale: 1000))
            }
        }

        // Mirror web's play/pause. AVPlayer is unmuted (audible) — it owns
        // audio output for the whole session.
        player?.isMuted = false
        if state.playing {
            if player?.rate == 0 { player?.play() }
        } else {
            if player?.rate != 0 { player?.pause() }
        }

        lastState = state
        refreshNowPlayingInfo()
    }

    private func replaceItem(with url: URL, startAt seconds: Double) {
        let item = AVPlayerItem(url: url)
        if let player = player {
            player.replaceCurrentItem(with: item)
        } else {
            player = AVPlayer(playerItem: item)
            // Observe rate transitions so the log shows exactly when (and
            // presumably why) AVPlayer stops. If the rate goes to 0 while
            // resigningActive=true, the bug is the OS pausing us, not our
            // update(state) path.
            rateObservation = player?.observe(\.rate, options: [.old, .new]) { [weak self] _, change in
                let old = change.oldValue ?? -1
                let new = change.newValue ?? -1
                guard old != new else { return }
                let resigning = self?.resigningActive ?? false
                let appState = UIApplication.shared.applicationState.rawValue
                print("BiliWeb: AVPlayer.rate \(old) -> \(new) resigning=\(resigning) appState=\(appState)")
            }
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
        nc.addObserver(self, selector: #selector(willResignActive),
                       name: UIApplication.willResignActiveNotification, object: nil)
        nc.addObserver(self, selector: #selector(didBecomeActive),
                       name: UIApplication.didBecomeActiveNotification, object: nil)
        nc.addObserver(self, selector: #selector(didEnterBackground),
                       name: UIApplication.didEnterBackgroundNotification, object: nil)
        nc.addObserver(self, selector: #selector(handleRouteChange),
                       name: AVAudioSession.routeChangeNotification, object: nil)
    }

    @objc private func willEnterForeground() {
        print("BiliWeb: willEnterForeground appState=\(UIApplication.shared.applicationState.rawValue) playerRate=\(player?.rate ?? 0)")
        guard let player = player else { return }
        // AVPlayer was the audio source while we were locked. Tell the web
        // <video> to seek to where AVPlayer reached so the visible frame
        // catches up to the audio.
        let resumeAt = player.currentTime().seconds
        // Only auto-play if the user didn't pause (via remote control or
        // otherwise) while backgrounded. If AVPlayer is stopped, just sync
        // the position without resuming playback.
        if player.rate > 0 {
            let js = "window.__biliResume && window.__biliResume(\(resumeAt));"
            webView?.evaluateJavaScript(js, completionHandler: nil)
        } else {
            let js = "window.__biliSync && window.__biliSync(\(resumeAt));"
            webView?.evaluateJavaScript(js, completionHandler: nil)
        }
    }

    @objc private func willResignActive() {
        print("BiliWeb: willResignActive playerRate=\(player?.rate ?? 0) lastPlaying=\(lastState?.playing ?? false)")
        resigningActive = true
    }

    @objc private func didBecomeActive() {
        print("BiliWeb: didBecomeActive playerRate=\(player?.rate ?? 0)")
        resigningActive = false
    }

    @objc private func didEnterBackground() {
        print("BiliWeb: didEnterBackground playerRate=\(player?.rate ?? 0)")
    }

    @objc private func handleRouteChange(_ notification: Notification) {
        let raw = (notification.userInfo?[AVAudioSessionRouteChangeReasonKey] as? UInt) ?? 0
        let reason = AVAudioSession.RouteChangeReason(rawValue: raw)
        print("BiliWeb: routeChange reason=\(raw) (\(String(describing: reason)))")
    }

    @objc private func handleInterruption(_ notification: Notification) {
        guard let userInfo = notification.userInfo,
              let raw = userInfo[AVAudioSessionInterruptionTypeKey] as? UInt,
              let type = AVAudioSession.InterruptionType(rawValue: raw)
        else { return }

        // Log everything in userInfo for diagnosis — iOS sometimes includes
        // an InterruptionReason key on newer versions that tells us *why*
        // the interruption happened (built-in mic, app suspended, etc.).
        print("BiliWeb: interruption type=\(raw) (\(type == .began ? "began" : "ended")) resigning=\(resigningActive) lastPlaying=\(lastState?.playing ?? false) userInfo=\(userInfo)")

        if type == .began {
            // Attempt 4 theory: when the WKWebView <video> is paused by iOS
            // during the resign-active transition, the host's audio session
            // gets an interruption notification, and AVPlayer auto-pauses
            // on .began regardless of our state-push guards. If the user
            // was playing and this interruption coincides with the resign
            // window, force-resume — this is a system reflex, not a real
            // user-meaningful interruption (which would be a phone call).
            //
            // resigningActive scopes the fix narrowly: during a normal
            // foreground interruption (phone call, Siri, another media app),
            // we leave AVPlayer paused and let the .ended path handle it.
            if resigningActive, lastState?.playing == true {
                print("BiliWeb: interruption.began during resign — re-activating session and resuming")
                DispatchQueue.main.async { [weak self] in
                    guard let self = self else { return }
                    do {
                        try AVAudioSession.sharedInstance().setActive(true)
                    } catch {
                        print("BiliWeb: session re-activate failed: \(error)")
                    }
                    self.player?.play()
                    print("BiliWeb: post-resume playerRate=\(self.player?.rate ?? 0)")
                }
            }
        } else if type == .ended,
                  let optsRaw = userInfo[AVAudioSessionInterruptionOptionKey] as? UInt {
            let opts = AVAudioSession.InterruptionOptions(rawValue: optsRaw)
            print("BiliWeb: interruption ended shouldResume=\(opts.contains(.shouldResume))")
            if opts.contains(.shouldResume) {
                player?.play()
            }
        }
    }

    // MARK: - Now Playing

    private func refreshNowPlayingInfo() {
        guard let state = lastState else { return }
        let elapsed = player?.currentTime().seconds ?? state.currentTime

        var info: [String: Any] = [:]
        info[MPMediaItemPropertyTitle] = state.title
        info[MPMediaItemPropertyArtist] = state.artist
        info[MPNowPlayingInfoPropertyElapsedPlaybackTime] = elapsed
        if state.duration.isFinite, state.duration > 0 {
            info[MPMediaItemPropertyPlaybackDuration] = state.duration
        }
        info[MPNowPlayingInfoPropertyPlaybackRate] = (player?.rate ?? 0) > 0 ? 1.0 : 0.0
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
            self?.player?.play()
            self?.evalOnWeb("document.querySelector('#player-video')?.play();")
            self?.refreshNowPlayingInfo()
            return .success
        }
        cc.pauseCommand.addTarget { [weak self] _ in
            self?.player?.pause()
            self?.evalOnWeb("document.querySelector('#player-video')?.pause();")
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
