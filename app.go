package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/abelmatos/md-viewer/internal/doc"
)

// Platform is what the app needs from the operating system. Its methods may
// be called from any goroutine except PickFile and PickFolder, which the
// bridge calls on the UI thread.
type Platform interface {
	// PickFile and PickFolder show an open dialog; "" means cancelled.
	PickFile() (string, error)
	PickFolder() (string, error)
	// OpenURL opens an http(s) or mailto link in the default app.
	OpenURL(url string) error
	// Reveal shows a file or folder in Explorer.
	Reveal(path string) error
	SetTitle(title string)
}

// View is what the page shows. The page calls the app's methods and renders
// the View they return.
type View struct {
	Path     string  `json:"path,omitempty"`
	Name     string  `json:"name,omitempty"`
	HTML     string  `json:"html"`
	Fragment string  `json:"fragment,omitempty"`
	Scroll   float64 `json:"scroll,omitempty"`
	// SameDoc means the document is already shown: only scroll.
	SameDoc    bool `json:"sameDoc,omitempty"`
	CanBack    bool `json:"canBack"`
	CanForward bool `json:"canForward"`
	// Tree is set only when the sidebar changes (another folder was opened,
	// or on reload).
	Tree      *doc.Node `json:"tree,omitempty"`
	Truncated bool      `json:"truncated,omitempty"`
	// Nothing to show: no file open, or the folder has no Markdown files.
	Empty bool `json:"empty,omitempty"`
}

// App holds the viewer's state. The bridge calls its methods from several
// goroutines. Slow work (reading, rendering, listing a folder) runs without
// the lock, on a snapshot of the state; the result is committed only if no
// other operation started meanwhile, so the newest click wins and a failure
// leaves the app as it was.
type App struct {
	platform Platform
	renderer *doc.Renderer

	mu      sync.Mutex
	gen     uint64 // bumped by every operation that changes what is shown
	root    string // folder shown in the sidebar
	history doc.History
	shown   string // path of the document currently in the page
}

// errSuperseded is returned to an operation that a newer one overtook; the
// page ignores it.
var errSuperseded = errors.New("superseded")

// snapshot is the state an operation starts from.
type snapshot struct {
	gen    uint64
	root   string
	shown  string
	cur    doc.Entry
	hasCur bool
}

// NewApp returns an app with nothing open.
func NewApp(p Platform) *App {
	return &App{platform: p, renderer: doc.NewRenderer()}
}

// begin starts an operation.
func (a *App) begin() snapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.gen++
	cur, ok := a.history.Current()
	return snapshot{gen: a.gen, root: a.root, shown: a.shown, cur: cur, hasCur: ok}
}

// commit runs f under the lock if no other operation started since s.
func (a *App) commit(s snapshot, f func()) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.gen != s.gen {
		return errSuperseded
	}
	f()
	return nil
}

// Open shows a file or folder the user chose (command line or dialog), so
// its path is trusted. A folder shows its README, or its first Markdown file.
// A file shows its repository (or its folder) in the sidebar.
func (a *App) Open(path string) (*View, error) {
	s := a.begin()
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	var file string
	newRoot := s.root
	if fi.IsDir() {
		newRoot = abs
	} else {
		if !doc.IsMarkdown(abs) {
			return nil, fmt.Errorf("not a Markdown file: %s", abs)
		}
		file = abs
		if s.root == "" || !doc.Within(s.root, abs) {
			newRoot = doc.RepoRoot(filepath.Dir(abs))
		}
	}

	var tree *doc.Node
	var truncated bool
	rootChanged := !doc.SamePath(newRoot, s.root)
	if rootChanged {
		if tree, truncated, err = doc.Tree(newRoot); err != nil {
			return nil, err
		}
	}
	if file == "" {
		if file = doc.FolderIndex(abs); file == "" && tree != nil {
			file = doc.FirstFile(tree)
		}
	}
	if file == "" {
		v := &View{Empty: true, Tree: tree, Truncated: truncated}
		return v, a.commit(s, func() {
			a.root, a.history, a.shown = newRoot, doc.History{}, ""
			a.platform.SetTitle(appTitle)
		})
	}

	e := doc.Entry{Path: file}
	v, err := a.render(e, s.shown, rootChanged)
	if err != nil {
		return nil, err // nothing changed
	}
	v.Tree, v.Truncated = tree, truncated
	return v, a.commit(s, func() {
		a.root = newRoot
		a.history.Visit(e)
		a.shown = e.Path
		a.describe(v)
	})
}

// Navigate follows a link clicked in the page. scroll is the page's scroll
// offset, kept so Back returns to it. A nil View means the link was handed
// to another app and the page stays as it is.
func (a *App) Navigate(href string, scroll float64) (*View, error) {
	s := a.begin()
	if !s.hasCur {
		return nil, errors.New("no document open")
	}
	t, err := doc.Resolve(s.root, s.cur.Path, href)
	if err != nil {
		return nil, err
	}
	switch t.Kind {
	case doc.External:
		return nil, a.platform.OpenURL(t.URL)
	case doc.Local:
		return nil, a.platform.Reveal(t.Path)
	case doc.Anchor:
		return a.visit(s, doc.Entry{Path: s.cur.Path, Fragment: t.Fragment}, scroll)
	default:
		return a.visit(s, doc.Entry{Path: t.Path, Fragment: t.Fragment}, scroll)
	}
}

// Show opens a file from the sidebar. The path comes from the page, so it
// must be a Markdown file inside the open folder.
func (a *App) Show(path string, scroll float64) (*View, error) {
	s := a.begin()
	if s.root == "" || !filepath.IsAbs(path) || !doc.IsMarkdown(path) || !doc.Within(s.root, path) {
		return nil, fmt.Errorf("%w: %s", doc.ErrUnsupported, path)
	}
	if err := doc.CheckPath(path, s.root); err != nil {
		return nil, err
	}
	return a.visit(s, doc.Entry{Path: path}, scroll)
}

// visit renders e and, if that works, adds it to the history.
func (a *App) visit(s snapshot, e doc.Entry, scroll float64) (*View, error) {
	v, err := a.render(e, s.shown, false)
	if err != nil {
		return nil, err
	}
	return v, a.commit(s, func() {
		a.history.SetScroll(scroll)
		a.history.Visit(e)
		a.shown = e.Path
		a.describe(v)
	})
}

// Back and Forward move through the history.
func (a *App) Back(scroll float64) (*View, error) {
	return a.step(scroll, -1)
}

func (a *App) Forward(scroll float64) (*View, error) {
	return a.step(scroll, +1)
}

func (a *App) step(scroll float64, delta int) (*View, error) {
	s := a.begin()
	a.mu.Lock()
	e, ok := a.history.Peek(delta)
	a.mu.Unlock()
	if !ok {
		return nil, nil
	}
	v, err := a.render(e, s.shown, false)
	if err != nil {
		return nil, err // the history stays where it was
	}
	return v, a.commit(s, func() {
		a.history.SetScroll(scroll)
		a.history.Move(delta)
		a.shown = e.Path
		a.describe(v)
	})
}

// Reload re-reads the current document and the sidebar, keeping the scroll
// position.
func (a *App) Reload(scroll float64) (*View, error) {
	return a.full(a.begin(), &scroll)
}

// Resume returns everything the page needs to show the current state from
// scratch: after the page itself was reloaded.
func (a *App) Resume() (*View, error) {
	return a.full(a.begin(), nil)
}

// full re-reads the sidebar and the current document. A nil scroll keeps
// the history's.
func (a *App) full(s snapshot, scroll *float64) (*View, error) {
	var tree *doc.Node
	var truncated bool
	if s.root != "" {
		var err error
		if tree, truncated, err = doc.Tree(s.root); err != nil {
			return nil, err
		}
	}
	if !s.hasCur {
		return &View{Empty: true, Tree: tree, Truncated: truncated}, nil
	}
	e := doc.Entry{Path: s.cur.Path, Scroll: s.cur.Scroll}
	if scroll != nil {
		e.Scroll = *scroll
	}
	v, err := a.render(e, s.shown, true)
	if err != nil {
		return nil, err
	}
	v.Tree, v.Truncated = tree, truncated
	return v, a.commit(s, func() {
		a.shown = e.Path
		a.describe(v)
	})
}

// Image returns a local image used by the shown document as a data: URI.
// docPath must be that document: the page names it so a late request from
// a previous document fails instead of resolving against the wrong folder.
func (a *App) Image(docPath, src string) (string, error) {
	a.mu.Lock()
	root, shown := a.root, a.shown
	a.mu.Unlock()
	if shown == "" || !doc.SamePath(docPath, shown) {
		return "", errors.New("not the shown document")
	}
	return doc.ImageDataURI(root, shown, src)
}

// describe fills in the history buttons and sets the window title. It runs
// under the lock, after the state changed.
func (a *App) describe(v *View) {
	v.CanBack, v.CanForward = a.history.CanBack(), a.history.CanForward()
	a.platform.SetTitle(v.Name + " - " + appTitle)
}

// render builds the view for e without touching the app's state. Unless
// force is set, it skips the work when e's document is the one shown.
func (a *App) render(e doc.Entry, shown string, force bool) (*View, error) {
	v := &View{Path: e.Path, Name: filepath.Base(e.Path), Fragment: e.Fragment, Scroll: e.Scroll}
	if !force && shown != "" && doc.SamePath(e.Path, shown) {
		v.SameDoc = true
		return v, nil
	}
	src, err := doc.ReadRegular(e.Path, doc.MaxDocSize)
	if err != nil {
		return nil, err
	}
	if v.HTML, err = a.renderer.Render(src); err != nil {
		return nil, err
	}
	return v, nil
}
