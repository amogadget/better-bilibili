// ContentView.swift
//
// A SwiftUI view that hosts a WKWebView pointed at the bili-web frontend.
// All of the actual UI lives in the web app; this file is just a transport.

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

    func makeUIView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        // Allow inline (non-fullscreen) playback so the player area is in-page.
        config.allowsInlineMediaPlayback = true
        // Don't require an extra user gesture before <video>/<audio> elements
        // can play — the user already tapped the play button.
        config.mediaTypesRequiringUserActionForPlayback = []
        // Persistent cookie / localStorage so QR login survives between launches.
        config.websiteDataStore = .default()

        let webView = WKWebView(frame: .zero, configuration: config)
        webView.allowsBackForwardNavigationGestures = true
        webView.scrollView.contentInsetAdjustmentBehavior = .always
        webView.load(URLRequest(url: url))
        return webView
    }

    func updateUIView(_ uiView: WKWebView, context: Context) {}
}
