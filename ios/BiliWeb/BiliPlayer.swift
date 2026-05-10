// BiliPlayer.swift
//
// Native-AVPlayer-backed background audio for the iOS shell.
//
// Architecture (mirrors what YouTube's iOS app effectively does):
//
//   foreground                background              foreground again
//   ──────────                ──────────              ────────────────
//   WKWebView <video>         WKWebView suspended     WKWebView <video>
//   plays + audible           native AVPlayer         seeks to AVPlayer
//                             unmutes — instant       currentTime, plays
//   native AVPlayer
//   plays + MUTED, in
//   lockstep w/ web
//
// The key trick: native AVPlayer is ALWAYS playing in lockstep with the
// web's <video> while the app is foregrounded — it's just muted, so the
// audible audio comes from the web. When the app backgrounds, iOS suspends
// WebContent (so the web's audio stops) and we unmute the native AVPlayer
// in the same instant. There is no buffer/load/seek delay because native
// has been playing the whole time.
//
// Cost: roughly 2× bandwidth while the app is foregrounded, because the
// web and the native player each fetch the same stream. This drops back
// to 1× the moment the app backgrounds (web stops fetching).

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

    /// Called from the WKScriptMessageHandler whenever the web pushes new
    /// playback state. Maintains an AVPlayer running in lockstep with the
    /// web's <video> so it can take over instantly when the app backgrounds.
    func update(state: State) {
        let appIsBackground = UIApplication.shared.applicationState == .background

        // While we're already running playback in the background, ignore web
        // state pushes that try to pause us — the web JS context is being
        // suspended anyway and any "pause" we receive is iOS auto-pausing,
        // not the user.
        if appIsBackground {
            // We still want to update Now Playing metadata (e.g. title) if it
            // arrived just before suspension.
            lastState = state
            refreshNowPlayingInfo()
            return
        }

        let currentURL = (player?.currentItem?.asset as? AVURLAsset)?.url
        if currentURL != state.src {
            replaceItem(with: state.src, startAt: state.currentTime)
        } else if let player = player {
            // Same source: only re-seek on noticeable drift, otherwise we'd
            // jitter every timeupdate.
            let nativeTime = player.currentTime().seconds
            if abs(nativeTime - state.currentTime) > 1.0 {
                player.seek(to: CMTime(seconds: state.currentTime, preferredTimescale: 1000))
            }
        }

        // Mirror web's play/pause state.
        if state.playing {
            if player?.rate == 0 { player?.play() }
        } else {
            if player?.rate != 0 { player?.pause() }
        }

        // While foreground, audio comes from the web's <video>; native is
        // muted-but-playing so it can become audible instantly on lock.
        player?.isMuted = true

        lastState = state
        refreshNowPlayingInfo()
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
        nc.addObserver(self, selector: #selector(didEnterBackground),
                       name: UIApplication.didEnterBackgroundNotification, object: nil)
        nc.addObserver(self, selector: #selector(willEnterForeground),
                       name: UIApplication.willEnterForegroundNotification, object: nil)
    }

    @objc private func didEnterBackground() {
        // The native AVPlayer has been playing all along — just unmute.
        // No buffer, no seek, no startup delay.
        player?.isMuted = false
    }

    @objc private func willEnterForeground() {
        guard let player = player else { return }
        // Hand audio back to the web. Tell it where AVPlayer reached so the
        // visible <video> can seek there before the user even sees it.
        let resumeAt = player.currentTime().seconds
        let js = "window.__biliResume && window.__biliResume(\(resumeAt));"
        webView?.evaluateJavaScript(js) { [weak player] _, _ in
            player?.isMuted = true
        }
    }

    // MARK: - Now Playing

    private func refreshNowPlayingInfo() {
        guard let state = lastState else { return }
        // Prefer native AVPlayer's clock over the (possibly stale) web state
        // when reporting elapsed time to the lock screen.
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
            return .success
        }
        cc.pauseCommand.addTarget { [weak self] _ in
            self?.player?.pause()
            self?.evalOnWeb("document.querySelector('#player-video')?.pause();")
            return .success
        }
        cc.changePlaybackPositionCommand.addTarget { [weak self] event in
            guard let self = self,
                  let positionEvent = event as? MPChangePlaybackPositionCommandEvent
            else { return .commandFailed }
            self.player?.seek(to: CMTime(seconds: positionEvent.positionTime, preferredTimescale: 1000))
            return .success
        }
    }

    private func evalOnWeb(_ js: String) {
        DispatchQueue.main.async { [weak self] in
            self?.webView?.evaluateJavaScript(js, completionHandler: nil)
        }
    }
}
