package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakePlatform struct {
	pick   string
	opened []string
	shown  []string
	title  string
}

func (f *fakePlatform) PickFile() (string, error)   { return f.pick, nil }
func (f *fakePlatform) PickFolder() (string, error) { return f.pick, nil }
func (f *fakePlatform) OpenURL(u string) error      { f.opened = append(f.opened, u); return nil }
func (f *fakePlatform) Reveal(p string) error       { f.shown = append(f.shown, p); return nil }
func (f *fakePlatform) SetTitle(t string)           { f.title = t }

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

func TestPickCancelled(t *testing.T) {
	_, p, a := setup(t)
	p.pick = ""
	if v, err := a.PickFolder(); v != nil || err != nil {
		t.Fatalf("cancelled pick = %+v, %v", v, err)
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
	page, err := shellHTML()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(page, "{{") || !strings.Contains(page, ".chroma") {
		t.Error("placeholders not replaced")
	}
	other, _ := shellHTML()
	if page == other {
		t.Error("nonce reused")
	}
}

func TestSettingsStore(t *testing.T) {
	s := settingsStore{path: filepath.Join(t.TempDir(), "sub", "settings.json")}
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
