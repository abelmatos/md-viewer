package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abelmatos/md-viewer/internal/render"
)

type fakePlatform struct {
	pick   string
	opened []string
	shown  []string
	title  string
	theme  Theme
}

func (f *fakePlatform) PickFile() (string, error)   { return f.pick, nil }
func (f *fakePlatform) PickFolder() (string, error) { return f.pick, nil }
func (f *fakePlatform) OpenURL(u string) error      { f.opened = append(f.opened, u); return nil }
func (f *fakePlatform) Reveal(p string) error       { f.shown = append(f.shown, p); return nil }
func (f *fakePlatform) SetTitle(t string)           { f.title = t }
func (f *fakePlatform) SetTheme(t Theme)            { f.theme = t }

func setup(t *testing.T) (root string, p *fakePlatform, a *App) {
	t.Helper()
	root = t.TempDir()
	files := map[string]string{
		".git/HEAD":     "",
		"README.md":     "# Project\n\nSee [the guide](docs/guide.md) and [the site](https://example.com).\n\n![logo](logo.png)\n",
		"logo.png":      "PNG",
		"docs/guide.md": "# Guide\n\n## Install\n\nBack to [home](../README.md#project) or [install](#install).\n\n[script](build.sh)\n",
		"docs/build.sh": "echo hi",
		"notes/a.md":    "# A\n",
	}
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	p = &fakePlatform{}
	return root, p, NewApp(p)
}

// must fails the test unless a call returned a view: must(t)(a.Open(p)).
func must(t *testing.T) func(*View, error) *View {
	return func(v *View, err error) *View {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if v == nil {
			t.Fatal("nil view")
		}
		return v
	}
}

func TestOpenFolderShowsReadme(t *testing.T) {
	root, p, a := setup(t)
	v := must(t)(a.Open(root))
	if v.Path != filepath.Join(root, "README.md") || !strings.Contains(v.HTML, "<h1") {
		t.Fatalf("view = %+v", v)
	}
	if v.Tree == nil || len(v.Tree.Children) != 3 {
		t.Fatalf("tree = %+v", v.Tree)
	}
	if p.title != "README.md - MD Viewer" {
		t.Errorf("title = %q", p.title)
	}
	if v.CanBack || v.CanForward {
		t.Error("history buttons enabled on first view")
	}
}

func TestOpenFileUsesRepoRoot(t *testing.T) {
	root, _, a := setup(t)
	v := must(t)(a.Open(filepath.Join(root, "notes", "a.md")))
	if v.Tree == nil || v.Tree.Path != root {
		t.Fatalf("sidebar root = %+v, want %s", v.Tree, root)
	}
	// Another file in the same repository keeps the sidebar.
	v = must(t)(a.Open(filepath.Join(root, "docs", "guide.md")))
	if v.Tree != nil {
		t.Error("tree resent for a file in the same root")
	}
	if _, err := a.Open(filepath.Join(root, "logo.png")); err == nil {
		t.Error("opened a non-Markdown file")
	}
}

func TestNavigateBackForward(t *testing.T) {
	root, p, a := setup(t)
	must(t)(a.Open(root))

	v := must(t)(a.Navigate("docs/guide.md", 50))
	if v.Path != filepath.Join(root, "docs", "guide.md") || !v.CanBack || v.Tree != nil {
		t.Fatalf("navigate = %+v", v)
	}

	// An anchor in the same document only scrolls.
	v = must(t)(a.Navigate("#install", 10))
	if !v.SameDoc || v.Fragment != "install" || v.HTML != "" {
		t.Fatalf("anchor = %+v", v)
	}

	// Back to the guide's top, then back to the README at its old offset.
	v = must(t)(a.Back(200))
	if !v.SameDoc || v.Scroll != 10 {
		t.Fatalf("back to guide = %+v", v)
	}
	v = must(t)(a.Back(0))
	if v.SameDoc || v.Scroll != 50 || v.CanBack || !v.CanForward || v.HTML == "" {
		t.Fatalf("back to readme = %+v", v)
	}
	v = must(t)(a.Forward(0))
	if v.Path != filepath.Join(root, "docs", "guide.md") {
		t.Fatalf("forward = %+v", v)
	}
	if v, err := a.Back(0); err != nil || v == nil {
		t.Fatalf("back: %v %v", v, err)
	}
	if v, err := a.Back(0); v != nil || err != nil {
		t.Fatalf("back past start = %+v, %v", v, err)
	}

	// Links to other apps leave the page as it is.
	if v, err := a.Navigate("https://example.com", 0); v != nil || err != nil {
		t.Fatalf("external = %+v, %v", v, err)
	}
	if len(p.opened) != 1 || p.opened[0] != "https://example.com" {
		t.Errorf("opened = %v", p.opened)
	}
	must(t)(a.Navigate("docs/guide.md", 0))
	if v, err := a.Navigate("build.sh", 0); v != nil || err != nil {
		t.Fatalf("local file = %+v, %v", v, err)
	}
	if len(p.shown) != 1 || p.shown[0] != filepath.Join(root, "docs", "build.sh") {
		t.Errorf("revealed = %v", p.shown)
	}
	if _, err := a.Navigate("javascript:alert(1)", 0); err == nil {
		t.Error("followed a javascript: link")
	}
	if _, err := a.Navigate("missing.md", 0); err == nil {
		t.Error("followed a missing link")
	}
}

func TestReloadAndImage(t *testing.T) {
	root, _, a := setup(t)
	must(t)(a.Open(root))
	readme := filepath.Join(root, "README.md")
	if err := os.WriteFile(readme, []byte("# Changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	v := must(t)(a.Reload(42))
	if !strings.Contains(v.HTML, "Changed") || v.Scroll != 42 || v.Tree == nil {
		t.Fatalf("reload = %+v", v)
	}
	uri, err := a.Image(readme, "logo.png")
	if err != nil || !strings.HasPrefix(uri, "data:image/png;base64,") {
		t.Fatalf("image = %q, %v", uri, err)
	}
}

func TestShowChecksPath(t *testing.T) {
	root, _, a := setup(t)
	must(t)(a.Open(root))
	must(t)(a.Show(filepath.Join(root, "notes", "a.md"), 0))
	outside := filepath.Join(t.TempDir(), "x.md")
	if err := os.WriteFile(outside, []byte("# secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		outside,                         // outside the open folder
		filepath.Join(root, "logo.png"), // not Markdown
		filepath.Join(root, "docs", "build.sh"),
		"docs/guide.md",     // relative
		`\\evil\share\x.md`, // network path
		filepath.Join(root, "..", filepath.Base(root), "..", "x.md"),
	} {
		if _, err := a.Show(p, 0); err == nil {
			t.Errorf("Show(%q) succeeded", p)
		}
	}
}

func TestFailedOpenKeepsState(t *testing.T) {
	root, _, a := setup(t)
	must(t)(a.Open(root))
	// A folder whose README can't be rendered: over the size limit.
	other := t.TempDir()
	big := filepath.Join(other, "README.md")
	if err := os.WriteFile(big, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(big, render.MaxDocSize+1); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Open(other); err == nil {
		t.Fatal("opened a README over the size limit")
	}
	// Still on the first folder: a file in it doesn't resend the tree, and
	// Show accepts its files.
	v := must(t)(a.Open(filepath.Join(root, "notes", "a.md")))
	if v.Tree != nil {
		t.Error("sidebar root changed by a failed open")
	}
	must(t)(a.Show(filepath.Join(root, "docs", "guide.md"), 0))
}

func TestImageOnlyForShownDocument(t *testing.T) {
	root, _, a := setup(t)
	must(t)(a.Open(root))
	readme := filepath.Join(root, "README.md")
	if _, err := a.Image(readme, "logo.png"); err != nil {
		t.Fatal(err)
	}
	must(t)(a.Navigate("docs/guide.md", 0))
	if _, err := a.Image(readme, "logo.png"); err == nil {
		t.Error("image served for a document that is no longer shown")
	}
}

func TestResume(t *testing.T) {
	root, _, a := setup(t)
	if v := must(t)(a.Resume()); !v.Empty {
		t.Fatalf("resume with nothing open = %+v", v)
	}
	must(t)(a.Open(root))
	must(t)(a.Navigate("docs/guide.md", 0))
	v := must(t)(a.Resume())
	if v.Path != filepath.Join(root, "docs", "guide.md") || v.HTML == "" || v.Tree == nil || !v.CanBack {
		t.Fatalf("resume = %+v", v)
	}
}

func TestEmptyFolder(t *testing.T) {
	_, _, a := setup(t)
	v := must(t)(a.Open(t.TempDir()))
	if !v.Empty || v.Tree == nil {
		t.Fatalf("empty folder = %+v", v)
	}
	if _, err := a.Navigate("x.md", 0); err == nil {
		t.Error("navigated with no document open")
	}
}

func TestShellHTML(t *testing.T) {
	page, err := shellHTML("k3y")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(page, "{{") || !strings.Contains(page, ".chroma") {
		t.Error("placeholders not replaced")
	}
	if !strings.Contains(page, `"k3y"`) {
		t.Error("bridge key missing")
	}
	other, _ := shellHTML("k3y")
	if page == other {
		t.Error("nonce reused")
	}
}

// Holding Ctrl+B saves the layout many times at once; every save must
// succeed and leave a whole file.
func TestSettingsConcurrentSaves(t *testing.T) {
	s := &settingsStore{path: filepath.Join(t.TempDir(), "settings.json")}
	var wg sync.WaitGroup
	errs := make(chan error, 50)
	for i := range 50 {
		wg.Go(func() { errs <- s.Save(Settings{SideWidth: 200 + i, SideHidden: i%2 == 0}) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("save: %v", err)
		}
	}
	if got := s.Load(); got.SideWidth < 200 {
		t.Errorf("after the saves = %+v", got)
	}
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(s.path), "*"))
	if len(left) != 1 {
		t.Errorf("files left behind: %v", left)
	}
}

func TestSettingsStore(t *testing.T) {
	s := &settingsStore{path: filepath.Join(t.TempDir(), "sub", "settings.json")}
	if got := s.Load(); got != (Settings{}) {
		t.Fatalf("missing file = %+v", got)
	}
	if err := s.Save(Settings{SideWidth: 340, SideHidden: true}); err != nil {
		t.Fatal(err)
	}
	if got := s.Load(); got != (Settings{SideWidth: 340, SideHidden: true}) {
		t.Fatalf("round trip = %+v", got)
	}
	if err := s.Save(Settings{SideWidth: 5}); err != nil {
		t.Fatal(err)
	}
	if got := s.Load(); got.SideWidth != 0 {
		t.Fatalf("out-of-range width kept: %+v", got)
	}
	if err := os.WriteFile(s.path, []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := s.Load(); got != (Settings{}) {
		t.Fatalf("corrupt file = %+v", got)
	}
}

func TestSlowRenderDoesNotBlock(t *testing.T) {
	root, _, a := setup(t)
	// Unclosed links make goldmark slow (seconds for this size).
	if err := os.WriteFile(filepath.Join(root, "slow.md"), []byte(strings.Repeat("[a](", 12000)), 0o600); err != nil {
		t.Fatal(err)
	}
	must(t)(a.Open(root))
	slowDone := make(chan error, 1)
	go func() {
		_, err := a.Navigate("slow.md", 0)
		slowDone <- err
	}()
	for a.renderer.Running() == 0 && len(slowDone) == 0 { // the slow render has started
		time.Sleep(time.Millisecond)
	}

	start := time.Now()
	v := must(t)(a.Show(filepath.Join(root, "notes", "a.md"), 0))
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Errorf("a sidebar click waited %v for the slow render", d)
	}
	// The slow one finishes later and must not replace what is shown.
	if err := <-slowDone; err == nil {
		t.Error("the overtaken navigation succeeded")
	}
	if v.Name != "a.md" {
		t.Fatalf("view = %+v", v)
	}
	if v := must(t)(a.Resume()); v.Name != "a.md" {
		t.Errorf("shown after the slow render = %s", v.Name)
	}
}

func TestShowRefusesLinkOutOfFolder(t *testing.T) {
	root, _, a := setup(t)
	outside := filepath.Join(t.TempDir(), "secret.md")
	if err := os.WriteFile(outside, []byte("# secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "notes", "link.md")); err != nil {
		t.Skip(err)
	}
	must(t)(a.Open(root))
	if _, err := a.Show(filepath.Join(root, "notes", "link.md"), 0); err == nil {
		t.Error("Show followed a link out of the folder")
	}
}

// TestMain lets the test binary act as the render child, as the exe does.
func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == renderFlag {
		os.Exit(renderChild())
	}
	os.Exit(m.Run())
}

func TestRenderInChild(t *testing.T) {
	big := "# Big\n\n" + strings.Repeat("Some *text* with `code`.\n\n", 4000) // > IsolateAbove
	html, err := renderInChild(context.Background(), []byte(big))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `<h1 id="big">Big</h1>`) || !strings.Contains(html, "<code>code</code>") {
		t.Errorf("child output: %.200s", html)
	}
}

func TestRunawayChildIsKilled(t *testing.T) {
	// Minutes of work in-process; the child must be gone at the deadline.
	slow := []byte(strings.Repeat("[a](", 300000))
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := renderInChild(ctx, slow); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("returned after %v: the child wasn't killed", d)
	}
}

// A new operation cancels the one it overtook: its render child is killed
// and frees its slot, so the next document doesn't wait for the slow ones'
// timeout.
func TestOvertakenRendersAreCancelled(t *testing.T) {
	root, _, a := setup(t)
	// Over render.IsolateAbove, so they render in a child; each takes minutes.
	for _, name := range []string{"slow1.md", "slow2.md"} {
		body := strings.Repeat("[a](", 300000) + name // different content: not refused as known-slow
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	must(t)(a.Open(root))
	slowDone := make(chan error, 2)
	for i, name := range []string{"slow1.md", "slow2.md"} {
		go func() {
			_, err := a.Show(filepath.Join(root, name), 0)
			slowDone <- err
		}()
		// Wait until it holds a render slot. Without cancellation both
		// keep theirs; with it, the second one's start ends the first.
		for a.renderer.Running() < i+1 && len(slowDone) == 0 {
			time.Sleep(time.Millisecond)
		}
	}

	start := time.Now()
	v := must(t)(a.Show(filepath.Join(root, "notes", "a.md"), 0))
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("the next document waited %v for the overtaken renders", d)
	}
	if v.Name != "a.md" {
		t.Errorf("view = %+v", v)
	}
	for range 2 {
		select {
		case err := <-slowDone:
			if !errors.Is(err, errSuperseded) {
				t.Errorf("overtaken render: err = %v, want errSuperseded", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("an overtaken render is still running")
		}
	}
}

// Every view the app returns carries the number of the operation that
// committed it, newer than any view before: the page draws a view only if
// it is newer than the one it drew last (#3).
func TestViewsCarrySeq(t *testing.T) {
	root, _, a := setup(t)
	var last uint64
	check := func(name string, v *View, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if v == nil {
			return // handed to another app: nothing to draw
		}
		if v.Seq <= last {
			t.Errorf("%s: seq %d, not newer than %d", name, v.Seq, last)
		}
		last = v.Seq
	}
	v, err := a.Resume()
	check("resume, nothing open", v, err)
	v, err = a.Open(root)
	check("open", v, err)
	v, err = a.Navigate("docs/guide.md", 0)
	check("navigate", v, err)
	v, err = a.Navigate("#install", 0)
	check("anchor", v, err)
	v, err = a.Navigate("https://example.com", 0)
	check("external", v, err)
	v, err = a.Back(0)
	check("back", v, err)
	v, err = a.Forward(0)
	check("forward", v, err)
	v, err = a.Show(filepath.Join(root, "notes", "a.md"), 0)
	check("show", v, err)
	v, err = a.Reload(0)
	check("reload", v, err)
	v, err = a.Resume()
	check("resume", v, err)
	v, err = a.Open(t.TempDir())
	check("open empty folder", v, err)
}
