// BiliPlayer.swift
//
// The bridge that gives this app real "YouTube-app-style" background audio.
//
// Why this exists:
//
//   WKWebView runs its content in a separate WebContent process. When the
//   host app backgrounds, iOS suspends WebContent — the page's <audio> and
//   <video> elements stop, and there is no host-side workaround that can
//   reach into that other process. Background-audio entitlements only apply
//   to audio produced by the host app's own process.
//
// What it does:
//
//   The web page (when it detects it's running in the native shell) posts
//   periodic state updates over a `player` message channel: stream URL,
//   currentTime, play/pause flag, title, artist, artwork URL.
//
//   When the app enters background, BiliPlayer takes the most recent state,
//   spins up an AVPlayer in the host process pointing at the same stream
//   URL, seeks to the recorded position, and starts playing. The host app's
//   audio session (Background Modes = Audio) keeps that AVPlayer alive
//   indefinitely.
//
//   When the app enters foreground, BiliPlayer pauses the AVPlayer, reads
//   its currentTime, and tells the web page to seek <video> to that
//   timestamp and resume. From the user's perspective playback never
//   stopped — they just see/hear the source switch under the hood.
//
//   Now Playing center (lock-screen panel + Control Center) is fed by
//   MPNowPlayingInfoCenter; remote-control commands map to AVPlayer.

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

    // Called from the WKScriptMessageHandler when JS pushes new state.
    func update(state: State) {
        lastState = state
        // If AVPlayer is currently active (i.e. we're in background), keep it
        // in sync with the web's seeks.
        if let player = player, !UIApplication.shared.applicationState.isForegroundLike {
            if let item = player.currentItem,
               let currentURL = (item.asset as? AVURLAsset)?.url,
               currentURL != state.src {
                replaceItem(with: state.src, startAt: state.currentTime)
            } else {
                player.seek(to: CMTime(seconds: state.currentTime, preferredTimescale: 1000))
            }
        }
        refreshNowPlayingInfo()
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
        guard let state = lastState, state.playing else { return }
        replaceItem(with: state.src, startAt: state.currentTime)
        player?.play()
        refreshNowPlayingInfo()
    }

    @objc private func willEnterForeground() {
        guard let player = player else { return }
        let resumeAt = player.currentTime().seconds
        player.pause()
        // Best-effort: tell the web page to pick up where we left off.
        let js = "window.__biliResume && window.__biliResume(\(resumeAt));"
        webView?.evaluateJavaScript(js, completionHandler: nil)
    }

    private func replaceItem(with url: URL, startAt seconds: Double) {
        let item = AVPlayerItem(url: url)
        if let player = player {
            player.replaceCurrentItem(with: item)
        } else {
            player = AVPlayer(playerItem: item)
        }
        player?.seek(to: CMTime(seconds: seconds, preferredTimescale: 1000))
    }

    // MARK: - Now Playing

    private func refreshNowPlayingInfo() {
        guard let state = lastState else { return }
        var info: [String: Any] = [:]
        info[MPMediaItemPropertyTitle] = state.title
        info[MPMediaItemPropertyArtist] = state.artist
        info[MPNowPlayingInfoPropertyElapsedPlaybackTime] = state.currentTime
        if state.duration.isFinite { info[MPMediaItemPropertyPlaybackDuration] = state.duration }
        info[MPNowPlayingInfoPropertyPlaybackRate] = state.playing ? 1.0 : 0.0
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

private extension UIApplication.State {
    var isForegroundLike: Bool { self == .active || self == .inactive }
}
