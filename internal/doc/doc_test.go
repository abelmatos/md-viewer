package doc

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// write creates files under dir; a name ending in "/" creates a folder.
func write(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(p, 0o750); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRender(t *testing.T) {
	r := NewRenderer()
	src := "# Hello World\n\n" +
		"| a | b |\n|---|---|\n| 1 | 2 |\n\n" +
		"- [x] done\n- [ ] todo\n\n" +
		"~~old~~ https://example.com\n\n" +
		"```go\nfunc main() {}\n```\n\n" +
		"<details><summary>More</summary>hidden</details>\n\n" +
		"<p align=\"center\"><img src=\"logo.png\" width=\"100\"></p>\n\n" +
		"Note[^1]\n\n[^1]: The note.\n"
	html, err := r.Render([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<h1 id="hello-world">Hello World</h1>`,
		`<table>`,
		`<input checked="" disabled="" type="checkbox"`,
		`<del>old</del>`,
		`<a href="https://example.com"`,
		`class="chroma"`,
		`<details><summary>More</summary>`,
		`<p align="center"><img src="logo.png" width="100"></p>`,
		`id="fn:1"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
}

func TestRenderStripsScripts(t *testing.T) {
	r := NewRenderer()
	src := "<script>alert(1)</script>\n\n" +
		"<img src=x onerror=\"alert(2)\">\n\n" +
		"[click](javascript:alert(3))\n\n" +
		"<iframe src=\"https://evil.example\"></iframe>\n\n" +
		"<a href=\"#\" style=\"position:fixed\" onclick=\"alert(4)\">x</a>\n"
	html, err := r.Render([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"<script", "onerror", "javascript:", "<iframe", "onclick", "style="} {
		if strings.Contains(strings.ToLower(html), bad) {
			t.Errorf("%q survived sanitising:\n%s", bad, html)
		}
	}
}

func TestCodeCSS(t *testing.T) {
	css, err := CodeCSS()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(css, ".chroma") || !strings.Contains(css, "prefers-color-scheme: dark") {
		t.Errorf("unexpected CSS:\n%.300s", css)
	}
}

func TestResolve(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"README.md":         "",
		"docs/guide.md":     "",
		"docs/My File.md":   "",
		"docs/img/a.png":    "",
		"docs/sub/index.md": "",
		"empty/":            "",
	})
	cur := filepath.Join(root, "docs", "guide.md")
	p := func(s string) string { return filepath.Join(root, filepath.FromSlash(s)) }

	tests := []struct {
		href string
		want Target
	}{
		{"#install", Target{Kind: Anchor, Fragment: "install"}},
		{"#caf%C3%A9", Target{Kind: Anchor, Fragment: "café"}},
		{"https://example.com/x", Target{Kind: External, URL: "https://example.com/x"}},
		{"mailto:a@b.c", Target{Kind: External, URL: "mailto:a@b.c"}},
		{"../README.md", Target{Kind: Markdown, Path: p("README.md")}},
		{"My%20File.md#top", Target{Kind: Markdown, Path: p("docs/My File.md"), Fragment: "top"}},
		{"./guide.md?plain=1", Target{Kind: Markdown, Path: p("docs/guide.md")}},
		{"/README.md", Target{Kind: Markdown, Path: p("README.md")}},
		{"sub", Target{Kind: Markdown, Path: p("docs/sub/index.md")}},
		{"sub/", Target{Kind: Markdown, Path: p("docs/sub/index.md")}},
		{"img/a.png", Target{Kind: Local, Path: p("docs/img/a.png")}},
		{"../empty", Target{Kind: Local, Path: p("empty")}},
	}
	for _, tt := range tests {
		got, err := Resolve(root, cur, tt.href)
		if err != nil {
			t.Errorf("Resolve(%q): %v", tt.href, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Resolve(%q) = %+v, want %+v", tt.href, got, tt.want)
		}
	}

	for _, href := range []string{"", "javascript:alert(1)", "file:///etc/passwd", "ftp://x"} {
		if _, err := Resolve(root, cur, href); !errors.Is(err, ErrUnsupported) {
			t.Errorf("Resolve(%q) error = %v, want ErrUnsupported", href, err)
		}
	}
	if _, err := Resolve(root, cur, "missing.md"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("missing file: error = %v", err)
	}
}

func TestTree(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"README.md":                 "",
		"b.markdown":                "",
		"a.txt":                     "",
		"docs/Zeta.md":              "",
		"docs/alpha.md":             "",
		"docs/deep/x.md":            "",
		"empty/notes.txt":           "",
		".git/HEAD.md":              "",
		"node_modules/pkg/READ.md":  "",
		"src/main.go":               "",
		"src/internal/design.mdown": "",
	})
	tree, truncated, err := Tree(root)
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Error("truncated")
	}
	var got []string
	var walk func(n *Node, prefix string)
	walk = func(n *Node, prefix string) {
		for _, c := range n.Children {
			name := prefix + c.Name
			if c.Dir {
				got = append(got, name+"/")
				walk(c, name+"/")
			} else {
				got = append(got, name)
			}
		}
	}
	walk(tree, "")
	want := []string{
		"docs/", "docs/deep/", "docs/deep/x.md", "docs/alpha.md", "docs/Zeta.md",
		"src/", "src/internal/", "src/internal/design.mdown",
		"b.markdown", "README.md",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("tree:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if f := FirstFile(tree); f != filepath.Join(root, "b.markdown") {
		t.Errorf("FirstFile = %s", f)
	}
}

func TestHistory(t *testing.T) {
	var h History
	if _, ok := h.Current(); ok || h.CanBack() || h.CanForward() {
		t.Fatal("empty history has entries")
	}
	h.Visit(Entry{Path: "a"})
	h.Visit(Entry{Path: "b"})
	h.SetScroll(120)
	h.Visit(Entry{Path: "c"})
	if e, _ := h.Back(); e.Path != "b" || e.Scroll != 120 {
		t.Fatalf("Back = %+v", e)
	}
	if !h.CanForward() {
		t.Fatal("no forward after back")
	}
	h.Visit(Entry{Path: "d"}) // drops c
	if h.CanForward() {
		t.Fatal("forward kept after visit")
	}
	h.Back()
	if e, _ := h.Back(); e.Path != "a" || h.CanBack() {
		t.Fatalf("Back = %+v", e)
	}
	if _, ok := h.Back(); ok {
		t.Fatal("Back past start")
	}
	if e, _ := h.Forward(); e.Path != "b" {
		t.Fatalf("Forward = %+v", e)
	}
}

func TestImageDataURI(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{"doc.md": "", "img/dot.png": "PNG", "notes.txt": "secret"})
	cur := filepath.Join(root, "doc.md")
	uri, err := ImageDataURI(root, cur, "img/dot.png")
	if err != nil {
		t.Fatal(err)
	}
	if uri != "data:image/png;base64,UE5H" {
		t.Errorf("uri = %s", uri)
	}
	for _, src := range []string{"notes.txt", "https://x/y.png", "missing.png", "doc.md"} {
		if _, err := ImageDataURI(root, cur, src); err == nil {
			t.Errorf("ImageDataURI(%q) succeeded", src)
		}
	}
}

func TestRepoRoot(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{"repo/.git/HEAD": "", "repo/docs/a/x.md": "", "plain/y.md": ""})
	if got := RepoRoot(filepath.Join(root, "repo", "docs", "a")); got != filepath.Join(root, "repo") {
		t.Errorf("RepoRoot = %s", got)
	}
	plain := filepath.Join(root, "plain")
	if got := RepoRoot(plain); got != plain {
		t.Errorf("RepoRoot without .git = %s", got)
	}
}
