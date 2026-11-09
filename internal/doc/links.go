package doc

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Kind says what a link points to.
type Kind int

const (
	// Anchor is a heading or footnote in the current document.
	Anchor Kind = iota + 1
	// Markdown is a Markdown file to show in the viewer.
	Markdown
	// External is a web or mail link for the default browser or mail app.
	External
	// Local is any other file or folder on disk; it is shown in Explorer,
	// never run.
	Local
)

// Target is a resolved link.
type Target struct {
	Kind     Kind
	Path     string // Markdown and Local
	Fragment string // Anchor and Markdown, without "#"
	URL      string // External
}

// ErrUnsupported is returned for links with a scheme the viewer won't follow,
// such as javascript: or file:.
var ErrUnsupported = errors.New("unsupported link")

var markdownExts = map[string]bool{".md": true, ".markdown": true, ".mdown": true, ".mkd": true, ".mkdn": true}

// IsMarkdown reports whether path has a Markdown file extension.
func IsMarkdown(path string) bool {
	return markdownExts[strings.ToLower(filepath.Ext(path))]
}

// indexNames are the files shown when a link or the user opens a folder.
var indexNames = []string{"README.md", "readme.md", "Readme.md", "index.md", "INDEX.md"}

// FolderIndex returns the README or index file of dir, or "" if it has none.
func FolderIndex(dir string) string {
	for _, n := range indexNames {
		p := filepath.Join(dir, n)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p
		}
	}
	return ""
}

// Resolve works out where href, found in the document at current, points.
// Relative paths are relative to current's folder; paths starting with "/"
// are relative to root, as on GitHub.
func Resolve(root, current, href string) (Target, error) {
	href = strings.TrimSpace(href)
	if href == "" {
		return Target{}, ErrUnsupported
	}
	if frag, ok := strings.CutPrefix(href, "#"); ok {
		return Target{Kind: Anchor, Fragment: unescape(frag)}, nil
	}
	if u, err := url.Parse(href); err == nil && len(u.Scheme) > 1 { // one letter is a drive, as in C:\docs
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "mailto":
			return Target{Kind: External, URL: href}, nil
		}
		return Target{}, fmt.Errorf("%w: %s", ErrUnsupported, u.Scheme)
	}

	p, frag, _ := strings.Cut(href, "#")
	p, _, _ = strings.Cut(p, "?")
	p = unescape(p)
	// A network path is judged on the link's own text too, before joining
	// it to anything (CheckPath below judges the final path).
	if err := checkText(p, current); err != nil && uncShare(p) != "" {
		return Target{}, err
	}
	p = filepath.FromSlash(p)
	switch {
	case filepath.VolumeName(p) != "":
		// C:\docs\a.md, or a network path that CheckPath rules on
	case strings.HasPrefix(p, string(filepath.Separator)):
		if root == "" {
			root = filepath.Dir(current)
		}
		p = filepath.Join(root, p)
	default:
		p = filepath.Join(filepath.Dir(current), p)
	}
	if err := CheckPath(p, current); err != nil {
		return Target{}, err
	}

	fi, err := os.Stat(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Target{}, fmt.Errorf("not found: %s", p)
		}
		return Target{}, err
	}
	if fi.IsDir() {
		if idx := FolderIndex(p); idx != "" {
			return Target{Kind: Markdown, Path: idx, Fragment: unescape(frag)}, nil
		}
		return Target{Kind: Local, Path: p}, nil
	}
	if IsMarkdown(p) {
		return Target{Kind: Markdown, Path: p, Fragment: unescape(frag)}, nil
	}
	return Target{Kind: Local, Path: p}, nil
}

// uncShare returns the \\server\share prefix of a network path (either slash
// style, including \\?\UNC\ paths), or "" for any other path.
func uncShare(p string) string {
	isSep := func(c byte) bool { return c == '\\' || c == '/' }
	// \??\ is the NT object namespace, which reaches UNC\ and devices too.
	if len(p) >= 4 && isSep(p[0]) && p[1] == '?' && p[2] == '?' && isSep(p[3]) {
		return `\??\`
	}
	if len(p) < 2 || !isSep(p[0]) || !isSep(p[1]) {
		return ""
	}
	parts := strings.FieldsFunc(p[2:], func(r rune) bool { return r == '\\' || r == '/' })
	if len(parts) >= 2 && (parts[0] == "?" || parts[0] == ".") {
		if strings.EqualFold(parts[1], "UNC") {
			parts = parts[2:]
		} else {
			return `\\` + parts[0] + `\` + parts[1] // device path, e.g. \\?\C:
		}
	}
	switch len(parts) {
	case 0:
		return `\\`
	case 1:
		return `\\` + parts[0]
	}
	return `\\` + parts[0] + `\` + parts[1]
}

// isDrive reports whether vol is a plain drive letter such as "C:".
func isDrive(vol string) bool {
	return len(vol) == 2 && vol[1] == ':' && (vol[0]|0x20 >= 'a' && vol[0]|0x20 <= 'z')
}

func unescape(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		return u
	}
	return s
}

// RepoRoot returns the closest folder at or above dir that contains .git,
// or dir itself if there is none.
func RepoRoot(dir string) string {
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return dir
		}
		d = parent
	}
}
