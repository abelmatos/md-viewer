# 2. Trust model: documents are hostile, the bridge is the boundary

Status: accepted

## Context

Markdown files come from anywhere: cloned repositories, downloads, mail.
A review found that a click on an image-map `<area>` (which the sanitiser
allowed and the page didn't intercept) navigated the WebView to a remote
page, which then had every Go binding and could read any file by path. The
real trust boundary is the page–Go bridge, not link resolution alone.

## Decision

Treat every document, and everything the page sends, as untrusted, with
several independent layers:

1. **Sanitiser allowlist** (`internal/doc/render.go`). An explicit list of
   what GitHub renders: no scripts, event handlers, styles, forms, iframes
   or image maps. Document ids get a `user-content-` prefix in the page so
   they can't take the app's own ids.
2. **Content Security Policy** with a per-launch nonce: only the page's own
   script runs; images only from `data:` and `https:`; no forms, no `<base>`.
3. **Every click on a link is caught** in the capture phase and sent to Go.
4. **Only the app's page may load.** Document requests other than the
   app's own are answered with an empty 403, and if anything replaces the
   page, Go puts it back and the page resumes the current state.
5. **The bridge needs a key.** Each launch generates a secret that exists
   only in the app's page. Requests without it are dropped, and replies are
   evaluated only in a document that holds it (`window.__mdKey`, set
   non-writable by the page). No bindings are injected into documents.
6. **Methods check their inputs.** The page can only show Markdown files
   inside the open folder (`Show`); links resolve relative to the shown
   document; images must be image files inside the open folder or the
   document's folder, for the document actually shown.
7. **Paths from documents are checked before the file system is touched**
   (`doc.CheckPath`): only drive letters or the document's own network
   share (touching another share leaks the NTLM hash), also after following
   symbolic links and junctions element by element; no device names
   (`COM1.png`), no alternate data streams; only regular files are read.

Limits: documents up to 2 MB, images up to 20 MB, folder listing up to
5,000 Markdown files / 100,000 entries / 3 s, rendering up to 10 s.
goldmark has inputs whose rendering time grows with the square of their
size and can't be cancelled; a timed-out render keeps one core busy until it
finishes, and the same content is then refused at once.

## Consequences

- A hostile document can at worst show wrong content, waste CPU for a
  while, or load remote `https:` images (which reveals the reader's IP, as
  on GitHub).
- The bridge contract is in `bridge.go` and tested on Linux, including that
  requests without the key are dropped and replies are guarded.
- Killing slow renders would need rendering in a child process; not done,
  since it adds a process start to every document.
