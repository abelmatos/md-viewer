package main

import (
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"strings"

	"github.com/abelmatos/md-viewer/internal/render"
)

const appTitle = "MD Viewer"

//go:embed ui/index.html
var indexHTML string

// shellHTML returns the page with the code-highlighting styles inlined, the
// bridge key, and a fresh nonce, so only the page's own script runs: the
// Content Security Policy blocks any script that slips into a document.
func shellHTML(key string) (string, error) {
	css, err := render.CodeCSS()
	if err != nil {
		return "", err
	}
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	nonce := base64.RawURLEncoding.EncodeToString(b)
	return strings.NewReplacer("{{NONCE}}", nonce, "{{CODE_CSS}}", css, "{{KEY}}", key).Replace(indexHTML), nil
}
