package links

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/abelmatos/md-viewer/internal/paths"
)

// "?" starts a query string, so the prefixes are percent-encoded here.
//
// TestResolveNetworkVolumes checks the volume rule with Windows path parsing,
// which only applies when the tests run on Windows (as in CI).
func TestResolveNetworkVolumes(t *testing.T) {
	cur := filepath.Join(t.TempDir(), "doc.md")
	for _, href := range []string{
		`\\evil\share\x.png`,
		`//evil/share/x.png`,
		`\%3F%3F\UNC\evil\share\x.png`,
		`\\%3F\UNC\evil\share\x.png`,
		`\\.\pipe\evil`,
		`\\%3F\GLOBALROOT\Device\Mup\evil\share\x.png`,
	} {
		if _, err := Resolve("", cur, href); !errors.Is(err, paths.ErrUnsupported) {
			t.Errorf("Resolve(%q) error = %v, want paths.ErrUnsupported", href, err)
		}
	}
}
