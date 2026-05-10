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

    func update(state: State) {
        // While the WebContent process is suspended (app backgrounded),
        // ignore state pushes — anything we receive is iOS auto-pause noise,
        // not a user action.
        if UIApplication.shared.applicationState == .background {
            lastState = state
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
    }

    @objc private func willEnterForeground() {
        guard let player = player else { return }
        // AVPlayer was the audio source while we were locked. Tell the web
        // <video> to seek to where AVPlayer reached so the visible frame
        // catches up to the audio.
        let resumeAt = player.currentTime().seconds
        let js = "window.__biliResume && window.__biliResume(\(resumeAt));"
        webView?.evaluateJavaScript(js, completionHandler: nil)
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
