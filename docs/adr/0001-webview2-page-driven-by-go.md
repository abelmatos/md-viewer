# 1. A WebView2 page driven by Go

Status: accepted (amended by [ADR-0002](0002-bridge-trust-model.md))

## Context

The viewer must be a small, native-feeling Windows app with no installer and
render Markdown the way GitHub does. It should be buildable from Linux (it
cross-compiles with `GOOS=windows`, no cgo); CI builds the release exe on
`windows-latest`, as meeting-reminder does. Options were a native UI (WinUI,
Win32 rich edit), an Electron or Tauri app, or Go with the WebView2 runtime
that ships with Windows 10/11.

## Decision

Go with `github.com/jchv/go-webview2` (pure Go, no cgo), using its low-level
`edge.Chromium` in a window of our own. Go renders Markdown with goldmark and
sanitises it with bluemonday; one static page, loaded with
`NavigateToString`, asks Go for views over a message bridge and inserts
their HTML.

- The page has no file access and no origin (`about:blank`). Local images
  reach it as `data:` URIs that Go reads.
- Links never navigate the page. The page sends each `href` to Go, which
  resolves it: Markdown is shown, `http(s)`/`mailto` go to the default app,
  anything else is shown in Explorer, never run.
- A per-launch nonce in the Content Security Policy lets only the page's own
  script run, on top of sanitising.
- Go methods run on goroutines, so file access and rendering never freeze
  the window; results return through the window's message queue (`post`)
  and `Eval`. Open dialogs run on the UI thread, outside WebView2's event
  handlers, which must not run modal loops.

The high-level `webview2.WebView` was used first and dropped: it injects its
bindings into every document the WebView loads, offers no way to refuse a
navigation, and exits through `log.Fatal` with no message when startup fails.

## Consequences

- One ~9 MB exe per architecture (amd64, arm64); startup is the runtime's.
- Rendering, link rules and the bridge are plain Go, tested on Linux.
- About 300 lines of Win32 (window, message loop, DPI changes) are ours.
- Machines without the WebView2 runtime get a message box with the download
  link; other startup failures get a message box and a log under
  `%LOCALAPPDATA%\md-viewer`.
