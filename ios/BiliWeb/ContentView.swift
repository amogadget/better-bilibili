// ContentView.swift
//
// SwiftUI host for a WKWebView pointed at the bili-web frontend, plus the
// WKScriptMessageHandler glue that wires the page's `player` channel into
// BiliPlayer (the AVPlayer-backed background-audio bridge).

import SwiftUI
import WebKit

// CHANGE THIS to the URL where your bili-web instance is reachable from your
// phone. Examples:
//   "https://bili.example.com"           — your public Caddy-fronted host
//   "http://my-vps.tail-xxxx.ts.net"     — Tailscale MagicDNS name
let kBiliWebURL = "https://bili.example.com"

struct ContentView: View {
    var body: some View {
        WebView(url: URL(string: kBiliWebURL)!)
    }
}

struct WebView: UIViewRepresentable {
    let url: URL

    func makeCoordinator() -> Coordinator {
        Coordinator()
    }

    func makeUIView(context: Context) -> WKWebView {
        let userController = WKUserContentController()
        userController.add(context.coordinator, name: "player")

        let config = WKWebViewConfiguration()
        config.userContentController = userController
        // Allow inline (non-fullscreen) playback so the player area is in-page.
        config.allowsInlineMediaPlayback = true
        // Don't require an extra user gesture before <video>/<audio> can play —
        // the user already tapped the play button.
        config.mediaTypesRequiringUserActionForPlayback = []
        // Persistent cookie / localStorage so QR login survives between launches.
        config.websiteDataStore = .default()

        let webView = WKWebView(frame: .zero, configuration: config)
        webView.allowsBackForwardNavigationGestures = true
        webView.scrollView.contentInsetAdjustmentBehavior = .always
        context.coordinator.player.attach(webView: webView)
        webView.load(URLRequest(url: url))
        return webView
    }

    func updateUIView(_ uiView: WKWebView, context: Context) {}

    final class Coordinator: NSObject, WKScriptMessageHandler {
        let player = BiliPlayer()

        func userContentController(_ userContentController: WKUserContentController,
                                   didReceive message: WKScriptMessage) {
            guard message.name == "player",
                  let body = message.body as? [String: Any],
                  let action = body["action"] as? String
            else { return }

            switch action {
            case "state":
                guard let srcStr = body["src"] as? String,
                      let src = URL(string: srcStr) else { return }
                let state = BiliPlayer.State(
                    src: src,
                    currentTime: body["currentTime"] as? Double ?? 0,
                    duration: body["duration"] as? Double ?? .nan,
                    playing: body["playing"] as? Bool ?? false,
                    title: body["title"] as? String ?? "",
                    artist: body["artist"] as? String ?? "",
                    artworkURL: (body["artwork"] as? String).flatMap(URL.init)
                )
                player.update(state: state)
            default:
                break
            }
        }
    }
}
