package doc

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCheckPathText(t *testing.T) {
	cur := filepath.Join(t.TempDir(), "doc.md")
	dir := filepath.Dir(cur)
	for _, p := range []string{
		filepath.Join(dir, "COM1.png"),
		filepath.Join(dir, "sub", "nul"),
		filepath.Join(dir, "lpt9.txt.md"),
		filepath.Join(dir, "CON "),
		filepath.Join(dir, "x:stream.png"),
		filepath.Join(dir, "x.png:stream"),
		`\\evil\share\x.png`,
	} {
		if err := CheckPath(p, cur); !errors.Is(err, ErrUnsupported) {
			t.Errorf("CheckPath(%q) = %v, want ErrUnsupported", p, err)
		}
	}
	for _, p := range []string{
		filepath.Join(dir, "doc.md"),
		filepath.Join(dir, "COMPUTE.md"),
		filepath.Join(dir, "console.md"),
		filepath.Join(dir, "com10.md"),
		filepath.Join(dir, "missing", "x.md"),
	} {
		if err := CheckPath(p, cur); err != nil {
			t.Errorf("CheckPath(%q) = %v, want nil", p, err)
		}
	}
}

func TestCheckPathLinks(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{"doc.md": "", "real/a.png": "PNG"})
	cur := filepath.Join(root, "doc.md")
	link := func(target, name string) {
		t.Helper()
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	// A link to a network share, as a file and as a folder further up.
	link(`//evil/share/x.png`, "img.png")
	link(`//evil/share`, "assets")
	// Links that stay local, relative and absolute, chained.
	link("real", "rel")
	link(filepath.Join(root, "rel"), "abs")
	// A loop.
	link("loop2", "loop1")
	link("loop1", "loop2")

	for _, name := range []string{"img.png", "assets/x.png"} {
		if err := CheckPath(filepath.Join(root, filepath.FromSlash(name)), cur); !errors.Is(err, ErrUnsupported) {
			t.Errorf("CheckPath(%s) = %v, want ErrUnsupported", name, err)
		}
		if _, err := Resolve(root, cur, name); !errors.Is(err, ErrUnsupported) {
			t.Errorf("Resolve(%s) = %v, want ErrUnsupported", name, err)
		}
	}
	for _, name := range []string{"rel/a.png", "abs/a.png"} {
		if err := CheckPath(filepath.Join(root, filepath.FromSlash(name)), cur); err != nil {
			t.Errorf("CheckPath(%s) = %v", name, err)
		}
	}
	if err := CheckPath(filepath.Join(root, "loop1"), cur); !errors.Is(err, ErrUnsupported) {
		t.Errorf("link loop: %v", err)
	}
}

func TestReadRegular(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{"a.md": "hello", "dir/": ""})
	if b, err := ReadRegular(filepath.Join(root, "a.md"), 5); err != nil || string(b) != "hello" {
		t.Fatalf("ReadRegular = %q, %v", b, err)
	}
	if _, err := ReadRegular(filepath.Join(root, "a.md"), 4); err == nil {
		t.Error("read past the limit")
	}
	if _, err := ReadRegular(filepath.Join(root, "dir"), 100); !errors.Is(err, ErrUnsupported) {
		t.Errorf("folder: %v", err)
	}
}

func TestImageOutsideFolder(t *testing.T) {
	base := t.TempDir()
	write(t, base, map[string]string{"repo/docs/doc.md": "", "repo/img/in.png": "PNG", "other/out.png": "PNG"})
	root := filepath.Join(base, "repo")
	cur := filepath.Join(root, "docs", "doc.md")
	if _, err := ImageDataURI(root, cur, "../img/in.png"); err != nil {
		t.Errorf("image inside the folder: %v", err)
	}
	if _, err := ImageDataURI(root, cur, "../../other/out.png"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("image outside the folder: %v", err)
	}
}

func TestRenderDropsImageMaps(t *testing.T) {
	html, err := NewRenderer().Render([]byte(`<img src="a.png" usemap="#m"><map name="m"><area shape="rect" coords="0,0,9,9" href="https://evil.example"></map>` + "\n\nPress <kbd>Ctrl</kbd>\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"<map", "<area", "usemap", "evil.example"} {
		if strings.Contains(html, bad) {
			t.Errorf("%q survived:\n%s", bad, html)
		}
	}
	if !strings.Contains(html, "<kbd>Ctrl</kbd>") {
		t.Errorf("kbd dropped:\n%s", html)
	}
}

func TestRenderTimeout(t *testing.T) {
	r := NewRenderer()
	r.timeout = 50 * time.Millisecond
	// Unclosed links make goldmark's work grow with the square of the size.
	if _, err := r.Render([]byte(strings.Repeat("[a](", 40000))); !errors.Is(err, ErrTooSlow) {
		t.Errorf("err = %v, want ErrTooSlow", err)
	}
}

func TestWithinAndSamePath(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "a", "b")
	for p, want := range map[string]bool{
		root:                              true,
		filepath.Join(root, "c", "d.md"):  true,
		filepath.Join(root, "..", "x.md"): false,
		filepath.Join(root+"c", "x.md"):   false,
	} {
		if got := Within(root, p); got != want {
			t.Errorf("Within(%s) = %v", p, got)
		}
	}
	if !SamePath("/a/b", "/a/b") {
		t.Error("SamePath")
	}
}

func FuzzResolve(f *testing.F) {
	root := f.TempDir()
	write(&testing.T{}, root, map[string]string{"doc.md": "", "sub/x.md": ""})
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
			if uncShare(target.Path) != "" || CheckPath(target.Path, cur) != nil {
				t.Fatalf("Resolve(%q) returned an unsafe path %q", href, target.Path)
			}
		}
	})
}
