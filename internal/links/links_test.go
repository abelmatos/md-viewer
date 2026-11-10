package links

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abelmatos/md-viewer/internal/paths"
	"github.com/abelmatos/md-viewer/internal/testfs"
)

func TestResolve(t *testing.T) {
	root := t.TempDir()
	testfs.Write(t, root, map[string]string{
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
		{`mailto:a@b.c?subject=Hi&attach=C:\x\id_rsa&body=x`, Target{Kind: External, URL: "mailto:a@b.c?body=x&subject=Hi"}},
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
		if got.Real != got.Path { // no links here: resolving changes nothing
			t.Errorf("Resolve(%q).Real = %q, want %q", tt.href, got.Real, got.Path)
		}
		got.Real = ""
		if got != tt.want {
			t.Errorf("Resolve(%q) = %+v, want %+v", tt.href, got, tt.want)
		}
	}

	for _, href := range []string{"", "javascript:alert(1)", "file:///etc/passwd", "ftp://x"} {
		if _, err := Resolve(root, cur, href); !errors.Is(err, paths.ErrUnsupported) {
			t.Errorf("Resolve(%q) error = %v, want paths.ErrUnsupported", href, err)
		}
	}
	// Network paths would make Windows authenticate to the server.
	for _, href := range []string{`\\evil\share\x.png`, "//evil/share/x.md", "%5C%5Cevil%5Cshare%5Cx.png", `\\%3F\UNC\evil\s\x`, "/\\evil/s/x", `\%3F%3F\UNC\evil\s\x.png`, "/%3F%3F/UNC/evil/s/x.png"} {
		if _, err := Resolve(root, cur, href); !errors.Is(err, paths.ErrUnsupported) {
			t.Errorf("Resolve(%q) error = %v, want paths.ErrUnsupported", href, err)
		}
		if _, err := ImageDataURI(root, cur, href); !errors.Is(err, paths.ErrUnsupported) {
			t.Errorf("ImageDataURI(%q) error = %v, want paths.ErrUnsupported", href, err)
		}
	}
	if _, err := Resolve(root, cur, "missing.md"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("missing file: error = %v", err)
	}
}

func TestImageDataURI(t *testing.T) {
	root := t.TempDir()
	testfs.Write(t, root, map[string]string{"doc.md": "", "img/dot.png": "PNG", "notes.txt": "secret"})
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
	testfs.Write(t, root, map[string]string{"repo/.git/HEAD": "", "repo/docs/a/x.md": "", "plain/y.md": ""})
	if got := RepoRoot(filepath.Join(root, "repo", "docs", "a")); got != filepath.Join(root, "repo") {
		t.Errorf("RepoRoot = %s", got)
	}
	plain := filepath.Join(root, "plain")
	if got := RepoRoot(plain); got != plain {
		t.Errorf("RepoRoot without .git = %s", got)
	}
}

func TestImageOutsideFolder(t *testing.T) {
	base := t.TempDir()
	testfs.Write(t, base, map[string]string{"repo/docs/doc.md": "", "repo/img/in.png": "PNG", "other/out.png": "PNG"})
	root := filepath.Join(base, "repo")
	cur := filepath.Join(root, "docs", "doc.md")
	if _, err := ImageDataURI(root, cur, "../img/in.png"); err != nil {
		t.Errorf("image inside the folder: %v", err)
	}
	if _, err := ImageDataURI(root, cur, "../../other/out.png"); !errors.Is(err, paths.ErrUnsupported) {
		t.Errorf("image outside the folder: %v", err)
	}
}

func FuzzResolve(f *testing.F) {
	root := f.TempDir()
	testfs.Write(&testing.T{}, root, map[string]string{"doc.md": "", "sub/x.md": ""})
	cur := filepath.Join(root, "doc.md")
	for _, s := range []string{"sub/x.md", "#a", "https://x", `\\evil\s\x`, "//evil/s", "%5C%5Ce%5Cs", `\%3F%3F\UNC\e\s`, "/sub", "../../etc", "COM1.png", "x:y"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, href string) {
		target, err := Resolve(root, cur, href)
		if err != nil {
			return
		}
		switch target.Kind {
		case Markdown, Local:
			if _, err := paths.CheckPath(target.Path, cur); err != nil {
				t.Fatalf("Resolve(%q) returned an unsafe path %q", href, target.Path)
			}
		}
	})
}

// Links on disk (from git with Developer Mode, or an extracted archive) must
// not lead the viewer anywhere paths.CheckPath refuses, nor out of the folder.
// A device name stands in for \\attacker\share, which Linux can't express.
func TestLinkedLookups(t *testing.T) {
	base := t.TempDir()
	testfs.Write(t, base, map[string]string{"repo/doc.md": "", "repo/sub/": "", "outside/con.md": "# secret", "outside/private.png": "PNG"})
	root := filepath.Join(base, "repo")
	cur := filepath.Join(root, "doc.md")
	link := func(target, name string) {
		t.Helper()
		if err := os.Symlink(target, filepath.Join(root, filepath.FromSlash(name))); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	link(filepath.Join(base, "outside", "con.md"), "sub/README.md")
	link(filepath.Join(base, "outside", "con.md"), ".git")
	link(filepath.Join(base, "outside", "private.png"), "pic.png")

	if got := FolderIndex(filepath.Join(root, "sub"), cur); got != "" {
		t.Errorf("FolderIndex followed a refused link: %s", got)
	}
	if tg, err := Resolve(root, cur, "sub/"); err == nil && tg.Kind == Markdown {
		t.Errorf("folder link opened a refused README: %+v", tg)
	}
	if got := RepoRoot(filepath.Join(root, "sub")); got != root {
		t.Errorf("RepoRoot = %s", got) // .git is seen, not followed
	}
	if _, err := ImageDataURI(root, cur, "pic.png"); !errors.Is(err, paths.ErrUnsupported) {
		t.Errorf("image linked from outside the folder: %v", err)
	}
}

func TestResolveWithinShare(t *testing.T) {
	// A document on a share may link within that share.
	if _, err := Resolve("", `\\localhost\no-such-share\a.md`, "//LOCALHOST/no-such-share/b.md"); errors.Is(err, paths.ErrUnsupported) {
		t.Errorf("link within the same share rejected: %v", err)
	}
}

func TestResolveRefusesLinkedShare(t *testing.T) {
	root := t.TempDir()
	testfs.Write(t, root, map[string]string{"doc.md": ""})
	cur := filepath.Join(root, "doc.md")
	for target, name := range map[string]string{`//evil/share/x.png`: "img.png", `//evil/share`: "assets"} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	for _, name := range []string{"img.png", "assets/x.png"} {
		if _, err := Resolve(root, cur, name); !errors.Is(err, paths.ErrUnsupported) {
			t.Errorf("Resolve(%s) = %v, want paths.ErrUnsupported", name, err)
		}
	}
}
