// BiliWebApp.swift
//
// Entry point for the thin native shell. Its only real job — beyond hosting a
// WKWebView — is to configure an AVAudioSession with the .playback category
// before the web view loads. Combined with the "Audio" Background Mode
// declared on the target (see ios/README.md), this is what gives the embedded
// web page true OS-level background-audio rights, so playback continues when
// the screen locks or the user switches apps.

import SwiftUI
import AVFoundation

@main
struct BiliWebApp: App {
    init() {
        configureAudioSession()
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
            try session.setCategory(
                .playback,
                mode: .moviePlayback,
                options: [.allowAirPlay]
            )
            try session.setActive(true)
        } catch {
            // Audio session setup is best-effort; a failure here means
            // background audio won't work but the app is still usable.
            print("BiliWeb: audio session setup failed: \(error)")
        }
    }
}
