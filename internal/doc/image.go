package doc

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
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
// own, so local images reach it this way. Only image files are read.
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
	f, err := os.Open(t.Path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, MaxImageSize+1))
	if err != nil {
		return "", err
	}
	if len(data) > MaxImageSize {
		return "", errors.New("image too large")
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}
