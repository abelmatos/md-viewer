package doc

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Limits for listing a folder, so opening a huge one (or a whole drive)
// stays fast: Markdown files listed, entries visited, and time spent.
const (
	MaxTreeFiles   = 5000
	MaxTreeEntries = 100000
	MaxTreeTime    = 3 * time.Second
)

// Node is a folder or Markdown file in the sidebar.
type Node struct {
	Name     string  `json:"name"`
	Path     string  `json:"path"`
	Dir      bool    `json:"dir,omitempty"`
	Children []*Node `json:"children,omitempty"`
}

// Tree lists the Markdown files under root, keeping only folders that contain
// some. Hidden folders (.git, .github…) and node_modules are skipped.
// truncated is true when a limit cut the listing short.
func Tree(root string) (tree *Node, truncated bool, err error) {
	tree = &Node{Name: filepath.Base(root), Path: root, Dir: true}
	dirs := map[string]*Node{root: tree}
	count, visited := 0, 0
	deadline := time.Now().Add(MaxTreeTime)
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		visited++
		if visited > MaxTreeEntries || (visited%256 == 0 && time.Now().After(deadline)) {
			truncated = true
			return filepath.SkipAll
		}
		if err != nil {
			if p == root {
				return err
			}
			return nil // unreadable subfolder: skip it
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (strings.HasPrefix(name, ".") || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !IsMarkdown(name) {
			return nil
		}
		if count >= MaxTreeFiles {
			truncated = true
			return filepath.SkipAll
		}
		count++
		parent := ensureDir(dirs, root, filepath.Dir(p))
		parent.Children = append(parent.Children, &Node{Name: name, Path: p})
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	sortTree(tree)
	return tree, truncated, nil
}

// ensureDir returns the node for dir, creating it and its parents up to root.
func ensureDir(dirs map[string]*Node, root, dir string) *Node {
	if n, ok := dirs[dir]; ok {
		return n
	}
	n := &Node{Name: filepath.Base(dir), Path: dir, Dir: true}
	dirs[dir] = n
	parent := ensureDir(dirs, root, filepath.Dir(dir))
	parent.Children = append(parent.Children, n)
	return n
}

// sortTree puts folders first, then files, each alphabetically ignoring case.
func sortTree(n *Node) {
	sort.Slice(n.Children, func(i, j int) bool {
		a, b := n.Children[i], n.Children[j]
		if a.Dir != b.Dir {
			return a.Dir
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	for _, c := range n.Children {
		if c.Dir {
			sortTree(c)
		}
	}
}

// FirstFile returns the first Markdown file in the tree, or "".
func FirstFile(n *Node) string {
	var dirs []*Node
	for _, c := range n.Children {
		if !c.Dir {
			return c.Path
		}
		dirs = append(dirs, c)
	}
	for _, d := range dirs {
		if p := FirstFile(d); p != "" {
			return p
		}
	}
	return ""
}

// IsDir reports whether path is an existing folder.
func IsDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
