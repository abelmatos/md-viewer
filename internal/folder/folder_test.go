package folder

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abelmatos/md-viewer/internal/testfs"
)

func TestTree(t *testing.T) {
	root := t.TempDir()
	testfs.Write(t, root, map[string]string{
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
	tree, truncated, err := Tree(context.Background(), root)
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
