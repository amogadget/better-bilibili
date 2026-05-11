// BiliWebApp.swift
//
// Entry point for the thin native shell. Two pieces of setup conspire to
// give the embedded web page true OS-level background-audio rights:
//
//  1. AVAudioSession is configured with the .playback category and activated
//     before any UI loads.
//  2. A silent audio buffer is played in a continuous loop on AVAudioEngine.
//     This is a known and necessary workaround: WKWebView's audio doesn't
//     inherit the host app's background-audio rights unless the host app
//     itself is actively producing audio. With the silence loop running,
//     iOS treats the app as a foreground media app, and WKWebView's <audio>
//     element keeps playing when the screen locks or the user swipes home.
//
// Combined with the "Audio" Background Mode declared on the target (see
// ios/README.md), this is what enables YouTube-app-style behavior.

import SwiftUI
import AVFoundation
import UIKit

/// AppDelegate lets us answer iOS's "which orientations does this app
/// currently support" question dynamically. We default to portrait, then
/// flip to landscape briefly while the user is in fullscreen video and
/// flip back on exit. iOS reads this every time it considers a rotation.
class AppDelegate: NSObject, UIApplicationDelegate {
    static var supportedOrientations: UIInterfaceOrientationMask = [.portrait]

    func application(_ application: UIApplication,
                     supportedInterfaceOrientationsFor window: UIWindow?) -> UIInterfaceOrientationMask {
        return AppDelegate.supportedOrientations
    }
}

@main
struct BiliWebApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate

    // Hold onto the silence keeper for the app's lifetime so the engine
    // doesn't get torn down by ARC. Exposed (non-private) so BiliPlayer
    // can stop it when AVPlayer pauses — keeping silence running forever
    // makes iOS treat the app as the active media app and bind the
    // Bluetooth audio route to us continuously, blocking other apps from
    // using BT audio while we're "paused".
    static let silence = SilenceKeeper()

    init() {
        configureAudioSession()
        // Don't start silence here — BiliPlayer will start it when
        // AVPlayer begins playing. While the user isn't playing anything
        // we should not hold the audio session at all.
    }

    var body: some Scene {
        WindowGroup {
            ContentView()
                .ignoresSafeArea(.all, edges: .bottom)
        }
    }

    private func configureAudioSession() {
        do {
            let session = AVAudioSession.sharedInstance()
            // .allowAirPlay requires the .playAndRecord category and would
            // produce OSStatus -50 with .playback. AirPlay works with
            // .playback by default — no option flag needed.
            try session.setCategory(.playback, mode: .moviePlayback)
            try session.setActive(true)
        } catch {
            print("BiliWeb: audio session setup failed: \(error)")
        }
    }
}

/// Plays an inaudible buffer on a permanent loop so iOS recognises the host
/// app as actively producing audio. This claims the audio session for the
/// app and lets WKWebView's audio piggyback onto the background entitlement.
///
/// `start()` and `stop()` are paired around AVPlayer's playing/paused state
/// so iOS only sees us as a "media app" when we actually have audio to
/// produce. Running silence permanently makes iOS keep our app bound to
/// the Bluetooth audio route, blocking other apps from using BT audio.
final class SilenceKeeper {
    private let engine = AVAudioEngine()
    private let player = AVAudioPlayerNode()
    private var setUp = false
    private(set) var running = false

    private func setUpIfNeeded() {
        guard !setUp else { return }
        setUp = true

        guard let format = AVAudioFormat(
            standardFormatWithSampleRate: 44_100,
            channels: 2
        ) else { return }

        engine.attach(player)
        engine.connect(player, to: engine.mainMixerNode, format: format)

        // 1 second of pre-zeroed PCM silence, looped forever.
        let frameCount: AVAudioFrameCount = 44_100
        guard let buffer = AVAudioPCMBuffer(
            pcmFormat: format,
            frameCapacity: frameCount
        ) else { return }
        buffer.frameLength = frameCount

        player.scheduleBuffer(buffer, at: nil, options: .loops, completionHandler: nil)
    }

    func start() {
        setUpIfNeeded()
        guard !running else { return }
        running = true
        do {
            try engine.start()
        } catch {
            print("BiliWeb: silence engine failed to start: \(error)")
            running = false
            return
        }
        player.play()
    }

    func stop() {
        guard running else { return }
        running = false
        player.pause()
        engine.stop()
    }
}
