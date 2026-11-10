// Package links works out what a document points to: other Markdown
// files, web and mail links, local files and images. Every path it returns
// has passed paths.CheckPath.
package links

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/abelmatos/md-viewer/internal/paths"
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
	Real     string // Path with links resolved (Markdown and Local)
	Fragment string // Anchor and Markdown, without "#"
	URL      string // External
}

// indexNames are the files shown when a link or the user opens a folder.
var indexNames = []string{"README.md", "readme.md", "Readme.md", "index.md", "INDEX.md"}

// FolderIndex returns the README or index file of dir, or "" if it has
// none. Each candidate passes paths.CheckPath (with current as the document, or
// the folder itself when the user opened it) before it is looked at: a
// README.md can be a link to a network share.
func FolderIndex(dir, current string) string {
	for _, n := range indexNames {
		p := filepath.Join(dir, n)
		real, err := paths.CheckPath(p, current)
		if err != nil {
			continue
		}
		if fi, err := os.Stat(real); err == nil && fi.Mode().IsRegular() {
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
		return Target{}, paths.ErrUnsupported
	}
	if frag, ok := strings.CutPrefix(href, "#"); ok {
		return Target{Kind: Anchor, Fragment: unescape(frag)}, nil
	}
	if u, err := url.Parse(href); err == nil && len(u.Scheme) > 1 { // one letter is a drive, as in C:\docs
		switch strings.ToLower(u.Scheme) {
		case "http", "https":
			return Target{Kind: External, URL: href}, nil
		case "mailto":
			return Target{Kind: External, URL: cleanMailto(u)}, nil
		}
		return Target{}, fmt.Errorf("%w: %s", paths.ErrUnsupported, u.Scheme)
	}

	p, frag, _ := strings.Cut(href, "#")
	p, _, _ = strings.Cut(p, "?")
	p = unescape(p)
	// A network path is judged on the link's own text too, before joining
	// it to anything (paths.CheckPath below judges the final path).
	if err := paths.CheckRaw(p, current); err != nil {
		return Target{}, err
	}
	p = filepath.FromSlash(p)
	switch {
	case filepath.VolumeName(p) != "":
		// C:\docs\a.md, or a network path that paths.CheckPath rules on
	case strings.HasPrefix(p, string(filepath.Separator)):
		if root == "" {
			root = filepath.Dir(current)
		}
		p = filepath.Join(root, p)
	default:
		p = filepath.Join(filepath.Dir(current), p)
	}
	real, err := paths.CheckPath(p, current)
	if err != nil {
		return Target{}, err
	}

	fi, err := os.Stat(real)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Target{}, fmt.Errorf("not found: %s", p)
		}
		return Target{}, err
	}
	if fi.IsDir() {
		if idx := FolderIndex(p, current); idx != "" {
			idxReal, err := paths.CheckPath(idx, current)
			if err != nil {
				return Target{}, err
			}
			return Target{Kind: Markdown, Path: idx, Real: idxReal, Fragment: unescape(frag)}, nil
		}
		return Target{Kind: Local, Path: p, Real: real}, nil
	}
	if paths.IsMarkdown(p) {
		return Target{Kind: Markdown, Path: p, Real: real, Fragment: unescape(frag)}, nil
	}
	return Target{Kind: Local, Path: p, Real: real}, nil
}

// cleanMailto keeps only the address and the subject, body, cc and bcc of a
// mailto: link. Some mail apps honour other fields, such as attach=, which
// a document could use to attach a local file to a mail.
func cleanMailto(u *url.URL) string {
	q := url.Values{}
	for k, v := range u.Query() {
		switch strings.ToLower(k) {
		case "subject", "body", "cc", "bcc":
			q[strings.ToLower(k)] = v
		}
	}
	out := url.URL{Scheme: "mailto", Opaque: u.Opaque, RawQuery: q.Encode()}
	return out.String()
}

func unescape(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		return u
	}
	return s
}

// RepoRoot returns the closest folder at or above dir that contains .git,
// or dir itself if there is none. .git is only looked at, never followed:
// it can be a link to a network share.
func RepoRoot(dir string) string {
	for d := dir; ; {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return dir
		}
		d = parent
	}
}
