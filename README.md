# md-viewer

A small, free Windows viewer for Markdown files. It renders them like
GitHub, lists every Markdown file in the folder, and follows links between
them with back and forward.

- One `md-viewer.exe`, no installer, no ads, no telemetry. It only goes
  online to load web images a document shows. The page is shown with the
  WebView2 runtime that ships with Windows 10/11.
- GitHub-flavoured Markdown: tables, task lists, strikethrough, autolinks,
  footnotes, highlighted code blocks, and the raw HTML READMEs use
  (`<details>`, centred images). Light and dark mode follow Windows.
- A sidebar lists the Markdown files of the folder you open. Opening a file
  shows its Git repository (or its folder). Hidden folders and
  `node_modules` are skipped.
- Links between Markdown files open in the viewer, including `#anchors`,
  folder links (shows their README) and root links like `/docs/x.md`. Web
  links open in your browser. Links to other files show them in Explorer;
  they are never run.
- Local images, relative to the document, are shown.

## Use

Download `md-viewer.exe` (or `md-viewer-arm64.exe` for ARM PCs) from the
[latest release](../../releases/latest), or
from the `md-viewer-exe` artifact of a
[build run](../../actions/workflows/build.yml), and put it in a folder of its
own (e.g. `%LOCALAPPDATA%\Programs\md-viewer`). Windows SmartScreen may warn
about an unsigned download: click **More info → Run anyway**.

Then open a file or folder with the toolbar, or start it with one:

```
md-viewer.exe C:\code\my-repo
md-viewer.exe C:\code\my-repo\docs\guide.md
```

To open `.md` files with a double-click, right-click one in Explorer, choose
**Open with → Choose another app → Choose an app on your PC**, pick
`md-viewer.exe` and tick **Always**.

| Key | Action |
| --- | --- |
| `Ctrl+O` / `Ctrl+Shift+O` | Open a file / folder |
| `Alt+←` / `Alt+→`, mouse back/forward buttons | Back / forward |
| `F5` | Reload the document and the sidebar |
| `Ctrl+B` | Show or hide the sidebar |
| `Ctrl+wheel`, `Ctrl+=` / `Ctrl+-` | Zoom |

## Security

Markdown files may come from anywhere, so the app treats them, and
everything the page sends to Go, as untrusted. In short:

- The rendered HTML is sanitised against an explicit allowlist (no scripts,
  event handlers, styles, forms, iframes or image maps), and the page's
  Content Security Policy only runs its own script.
- Clicking a link never navigates the page. Go decides what it opens:
  Markdown in the viewer, `http`, `https` and `mailto` in the default app,
  anything else is shown in Explorer, not run. Any other page load is
  refused.
- The page talks to Go with a per-launch key that no other document has, and
  Go checks every path it receives.
- Paths in documents can't reach other network shares (which would leak
  your Windows password hash), devices or hidden data streams, also through
  symbolic links. Images must be inside the open folder.
- Web images (`https:`) in a document are loaded, as on GitHub, which tells
  their server your IP address.
- DLLs are loaded from System32 only. Releases carry a signed build
  provenance: `gh attestation verify md-viewer.exe -R abelmatos/md-viewer`.

Details and limits: [ADR-0002](docs/adr/0002-bridge-trust-model.md).

## Development

Requires Go (see `go.mod`); everything else is optional. Windows builds run in
GitHub Actions, so no Windows toolchain is needed. Run every check for both
platforms, since the Windows-only files only compile with `GOOS=windows`:

```sh
go vet ./... && GOOS=windows go vet ./...
go test -race ./...
golangci-lint run ./... && GOOS=windows golangci-lint run ./...   # as in CI
GOOS=windows go build -ldflags="-H windowsgui" -o md-viewer.exe .
```

From WSL you can run the built exe directly: `./md-viewer.exe "$(wslpath -w docs)"`.
Start it with `-debug` to enable the developer tools (right-click → Inspect).
The log is at `%LOCALAPPDATA%\md-viewer\md-viewer.log`.

| Path | What |
| --- | --- |
| `internal/doc` | Rendering, sanitising, link resolution, folder tree, history (any OS) |
| `app.go` | Viewer state and its operations (any OS) |
| `bridge.go` | The page–Go message bridge: key check, methods, replies (any OS) |
| `ui/index.html` | The page: layout, styles, and the script that renders views |
| `*_windows.go` | Window, WebView2, open dialogs, Explorer and browser hand-off, log |
| `winres/` | Icon, manifest and version info; run `go generate` after editing |

Releases: push a `v*` tag and CI attaches both exes, `SHA256SUMS` and build
provenance to a GitHub release. The exes are not Authenticode-signed yet, so
SmartScreen warns on each new version. Design decisions are recorded in [docs/adr](docs/adr/).

## License

MIT
