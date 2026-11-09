package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/abelmatos/md-viewer/internal/doc"
)

// maxDocSize is the largest Markdown file the viewer opens.
const maxDocSize = 16 << 20

// Platform is what the app needs from the operating system.
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
	// Nothing to show: no file open, or the user cancelled a dialog.
	Empty bool `json:"empty,omitempty"`
}

// App holds the viewer's state. Its exported-to-JS methods run on the UI
// thread, but a mutex keeps it safe if that ever changes.
type App struct {
	mu       sync.Mutex
	platform Platform
	renderer *doc.Renderer
	root     string // folder shown in the sidebar
	history  doc.History
	shown    string // path of the document currently in the page
}

// NewApp returns an app with nothing open.
func NewApp(p Platform) *App {
	return &App{platform: p, renderer: doc.NewRenderer()}
}

// Open shows a file or folder chosen by the user (command line or dialog).
// A folder shows its README, or its first Markdown file. A file shows its
// repository (or its folder) in the sidebar.
func (a *App) Open(path string) (*View, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.open(path)
}

func (a *App) open(path string) (*View, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}

	var file string
	newRoot := a.root
	if fi.IsDir() {
		newRoot = abs
	} else {
		if !doc.IsMarkdown(abs) {
			return nil, fmt.Errorf("not a Markdown file: %s", abs)
		}
		file = abs
		if a.root == "" || !within(a.root, abs) {
			newRoot = doc.RepoRoot(filepath.Dir(abs))
		}
	}

	var tree *doc.Node
	var truncated bool
	if newRoot != a.root {
		if tree, truncated, err = doc.Tree(newRoot); err != nil {
			return nil, err
		}
		a.root = newRoot
	}
	if file == "" {
		if file = doc.FolderIndex(abs); file == "" && tree != nil {
			file = doc.FirstFile(tree)
		}
	}
	if file == "" {
		a.history = doc.History{}
		a.shown = ""
		a.platform.SetTitle(appTitle)
		return &View{Empty: true, Tree: tree, Truncated: truncated}, nil
	}

	v, err := a.visit(doc.Entry{Path: file})
	if err != nil {
		return nil, err
	}
	v.Tree, v.Truncated = tree, truncated
	return v, nil
}

// within reports whether path is root or inside it.
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Navigate follows a link clicked in the page. scroll is the page's scroll
// offset, kept so Back returns to it. A nil View means the link was handed
// to another app and the page stays as it is.
func (a *App) Navigate(href string, scroll float64) (*View, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	cur, ok := a.history.Current()
	if !ok {
		return nil, errors.New("no document open")
	}
	t, err := doc.Resolve(a.root, cur.Path, href)
	if err != nil {
		return nil, err
	}
	switch t.Kind {
	case doc.External:
		return nil, a.platform.OpenURL(t.URL)
	case doc.Local:
		return nil, a.platform.Reveal(t.Path)
	case doc.Anchor:
		a.history.SetScroll(scroll)
		return a.visit(doc.Entry{Path: cur.Path, Fragment: t.Fragment})
	default:
		a.history.SetScroll(scroll)
		return a.visit(doc.Entry{Path: t.Path, Fragment: t.Fragment})
	}
}

// Show opens a file from the sidebar.
func (a *App) Show(path string, scroll float64) (*View, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.history.SetScroll(scroll)
	return a.visit(doc.Entry{Path: path})
}

// Back and Forward move through the history.
func (a *App) Back(scroll float64) (*View, error) {
	return a.step(scroll, (*doc.History).Back)
}

func (a *App) Forward(scroll float64) (*View, error) {
	return a.step(scroll, (*doc.History).Forward)
}

func (a *App) step(scroll float64, move func(*doc.History) (doc.Entry, bool)) (*View, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.history.SetScroll(scroll)
	e, ok := move(&a.history)
	if !ok {
		return nil, nil
	}
	return a.show(e)
}

// Reload re-reads the current document and the sidebar, keeping the scroll
// position.
func (a *App) Reload(scroll float64) (*View, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var tree *doc.Node
	var truncated bool
	if a.root != "" {
		var err error
		if tree, truncated, err = doc.Tree(a.root); err != nil {
			return nil, err
		}
	}
	cur, ok := a.history.Current()
	if !ok {
		return &View{Empty: true, Tree: tree, Truncated: truncated}, nil
	}
	a.shown = "" // force a re-render
	v, err := a.show(doc.Entry{Path: cur.Path, Scroll: scroll})
	if err != nil {
		return nil, err
	}
	v.Tree, v.Truncated = tree, truncated
	return v, nil
}

// PickFile and PickFolder open a dialog and show what the user chose. They
// return nil if the user cancelled.
func (a *App) PickFile() (*View, error) {
	p, err := a.platform.PickFile()
	if err != nil || p == "" {
		return nil, err
	}
	return a.Open(p)
}

func (a *App) PickFolder() (*View, error) {
	p, err := a.platform.PickFolder()
	if err != nil || p == "" {
		return nil, err
	}
	return a.Open(p)
}

// Image returns a local image used by the document at docPath as a data: URI.
func (a *App) Image(docPath, src string) (string, error) {
	a.mu.Lock()
	root := a.root
	a.mu.Unlock()
	return doc.ImageDataURI(root, docPath, src)
}

// visit adds e to the history and shows it.
func (a *App) visit(e doc.Entry) (*View, error) {
	v, err := a.render(e)
	if err != nil {
		return nil, err
	}
	a.history.Visit(e)
	a.finish(v, e.Path)
	return v, nil
}

// show shows e, which is already in the history.
func (a *App) show(e doc.Entry) (*View, error) {
	v, err := a.render(e)
	if err != nil {
		return nil, err
	}
	a.finish(v, e.Path)
	return v, nil
}

func (a *App) finish(v *View, path string) {
	a.shown = path
	v.CanBack, v.CanForward = a.history.CanBack(), a.history.CanForward()
	a.platform.SetTitle(filepath.Base(path) + " - " + appTitle)
}

// render builds the view for e, skipping the work when e's document is
// already in the page.
func (a *App) render(e doc.Entry) (*View, error) {
	v := &View{Path: e.Path, Name: filepath.Base(e.Path), Fragment: e.Fragment, Scroll: e.Scroll}
	if e.Path == a.shown {
		v.SameDoc = true
		return v, nil
	}
	src, err := readLimited(e.Path)
	if err != nil {
		return nil, err
	}
	if v.HTML, err = a.renderer.Render(src); err != nil {
		return nil, err
	}
	return v, nil
}

func readLimited(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.Size() > maxDocSize {
		return nil, fmt.Errorf("%s is too large (%d MB)", filepath.Base(path), fi.Size()>>20)
	}
	return os.ReadFile(path) //nolint:gosec // G304: opening the files the user picks is the point of a viewer
}
