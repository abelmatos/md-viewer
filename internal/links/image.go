package links

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/abelmatos/md-viewer/internal/paths"
)

// MaxImageSize is the largest local image the viewer inlines.
const MaxImageSize = 20 << 20

var imageTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
	".bmp":  "image/bmp",
	".ico":  "image/x-icon",
	".avif": "image/avif",
}

// ImageDataURI reads the local image src, referenced from the document at
// current, and returns it as a data: URI. The page has no file access of its
// own, so local images reach it this way. Only image files inside root, or
// inside current's folder, are read.
func ImageDataURI(root, current, src string) (string, error) {
	t, err := Resolve(root, current, src)
	if err != nil {
		return "", err
	}
	if t.Kind != Local {
		return "", fmt.Errorf("not a local image: %s", src)
	}
	mime, ok := imageTypes[strings.ToLower(filepath.Ext(t.Path))]
	if !ok {
		return "", fmt.Errorf("not an image: %s", t.Path)
	}
	// Like GitHub, images come from the open folder (or the document's own
	// folder), not from anywhere on the drive. The resolved paths are
	// compared, so a link inside the folder can't point outside it.
	if !paths.InsideFolder(root, t.Real) && !paths.InsideFolder(filepath.Dir(current), t.Real) {
		return "", fmt.Errorf("%w: image outside the folder: %s", paths.ErrUnsupported, src)
	}
	data, err := paths.ReadRegular(t.Real, MaxImageSize)
	if err != nil {
		return "", err
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}
