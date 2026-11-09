package doc

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
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
	// folder), not from anywhere on the drive.
	if !Within(root, t.Path) && !Within(filepath.Dir(current), t.Path) {
		return "", fmt.Errorf("%w: image outside the folder: %s", ErrUnsupported, src)
	}
	data, err := ReadRegular(t.Path, MaxImageSize)
	if err != nil {
		return "", err
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}
