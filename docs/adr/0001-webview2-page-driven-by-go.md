# 1. A WebView2 page driven by Go bindings

Status: accepted

## Context

The viewer must be a small, native-feeling Windows app with no installer,
render Markdown the way GitHub does, and build from Linux in CI, as
meeting-reminder does. Options were a native UI (WinUI, Win32 rich edit), an
Electron or Tauri app, or Go with the WebView2 runtime that ships with
Windows 10/11.

## Decision

Go with `github.com/jchv/go-webview2` (pure Go, no cgo). Go renders Markdown
with goldmark and sanitises it with bluemonday; one static page, loaded with
`SetHtml`, asks Go for views through bound functions (`mdNavigate`,
`mdBack`, …) and inserts their HTML.

- The page has no file access and no origin (`about:blank`), so it can't be
  turned against the disk. Local images reach it as `data:` URIs that Go
  reads, image types only.
- Links never navigate the page. The page sends each `href` to Go, which
  resolves it: Markdown is shown, `http(s)`/`mailto` go to the default app,
  anything else is shown in Explorer, never run.
- A per-launch nonce in the Content Security Policy lets only the page's
  own script run, on top of sanitising.
- Open dialogs run a modal loop, which WebView2 doesn't allow inside its
  event handlers, where bindings run. They are queued with `Dispatch` and
  deliver their result to the page with `Eval`.

## Consequences

- One ~9 MB exe; startup is the WebView2 runtime's.
- Rendering and link rules are plain Go, tested on Linux.
- Machines without the WebView2 runtime get a message box with the download
  link instead of a window.
