package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Settings are the layout choices kept between launches.
type Settings struct {
	SideWidth  int  `json:"sideWidth,omitempty"`
	SideHidden bool `json:"sideHidden,omitempty"`
}

// Sanity limits for a saved sidebar width, in CSS pixels. The page applies
// the real ones: at least 180, at most half the window.
const (
	minSideWidth = 180
	maxSideWidth = 4000
)

// settingsStore reads and writes Settings as JSON at path.
type settingsStore struct {
	path string
}

// Load returns the saved settings, or the defaults if there are none or the
// file is unreadable.
func (s settingsStore) Load() Settings {
	var v Settings
	b, err := os.ReadFile(s.path)
	if err != nil || json.Unmarshal(b, &v) != nil {
		return Settings{}
	}
	return v.clean()
}

// Save writes v, replacing the file atomically.
func (s settingsStore) Save(v Settings) error {
	b, err := json.MarshalIndent(v.clean(), "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// clean drops out-of-range values, so the default width is used instead.
func (v Settings) clean() Settings {
	if v.SideWidth < minSideWidth || v.SideWidth > maxSideWidth {
		v.SideWidth = 0
	}
	return v
}
