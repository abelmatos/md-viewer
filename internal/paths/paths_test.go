package paths

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/abelmatos/md-viewer/internal/testfs"
)

func TestUNCShare(t *testing.T) {
	for p, want := range map[string]string{
		`\\srv\docs\a.md`:    `\\srv\docs`,
		"//srv/docs/a.md":    `\\srv\docs`,
		`\\?\UNC\srv\docs\a`: `\\srv\docs`,
		`\\srv`:              `\\srv`,
		`\\?\C:\x`:           `\\?\C:`,
		`C:\docs\a.md`:       "",
		"/docs/a.md":         "",
		"docs/a.md":          "",
	} {
		if got := uncShare(p); got != want {
			t.Errorf("uncShare(%q) = %q, want %q", p, got, want)
		}
	}
}

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
		if _, err := CheckPath(p, cur); !errors.Is(err, ErrUnsupported) {
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
		if _, err := CheckPath(p, cur); err != nil {
			t.Errorf("CheckPath(%q) = %v, want nil", p, err)
		}
	}
}

func TestCheckPathLinks(t *testing.T) {
	root := t.TempDir()
	testfs.Write(t, root, map[string]string{"doc.md": "", "real/a.png": "PNG"})
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
		if _, err := CheckPath(filepath.Join(root, filepath.FromSlash(name)), cur); !errors.Is(err, ErrUnsupported) {
			t.Errorf("CheckPath(%s) = %v, want ErrUnsupported", name, err)
		}
	}
	for _, name := range []string{"rel/a.png", "abs/a.png"} {
		if _, err := CheckPath(filepath.Join(root, filepath.FromSlash(name)), cur); err != nil {
			t.Errorf("CheckPath(%s) = %v", name, err)
		}
	}
	if _, err := CheckPath(filepath.Join(root, "loop1"), cur); !errors.Is(err, ErrUnsupported) {
		t.Errorf("link loop: %v", err)
	}
}

func TestReadRegular(t *testing.T) {
	root := t.TempDir()
	testfs.Write(t, root, map[string]string{"a.md": "hello", "dir/": ""})
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

func TestDeviceAndVolumeNames(t *testing.T) {
	for _, n := range []string{"COM¹.png", "lpt³", "COM².md"} {
		if !isDeviceName(n) {
			t.Errorf("%s not a device", n)
		}
	}
	for vol, want := range map[string]bool{
		"C:": true, `\\?\C:`: true, `\\?\Volume{0a1b2c3d-0000-0000-0000-100000000000}`: true,
		`\\?\UNC\srv\s`: false, `\\srv\s`: false, `\\.\C:`: false, `\\?\GLOBALROOT`: false,
	} {
		if got := isLocalVolume(vol); got != want {
			t.Errorf("isLocalVolume(%q) = %v", vol, got)
		}
	}
}
