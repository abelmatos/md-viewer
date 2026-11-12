package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
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

// settingsStore reads and writes Settings as JSON at path. Saves may come
// in at once (holding Ctrl+B); they are written one at a time.
type settingsStore struct {
	path string
	mu   sync.Mutex
}

// Load returns the saved settings, or the defaults if there are none or the
// file is unreadable.
func (s *settingsStore) Load() Settings {
	var v Settings
	b, err := os.ReadFile(s.path)
	if err != nil || json.Unmarshal(b, &v) != nil {
		return Settings{}
	}
	return v.clean()
}

// Save writes v, replacing the file atomically.
func (s *settingsStore) Save(v Settings) error {
	b, err := json.MarshalIndent(v.clean(), "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// A temporary file of its own in the same folder, so the rename is
	// atomic and never takes another writer's file.
	f, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".*.tmp")
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if cErr := f.Close(); err == nil {
		err = cErr
	}
	if err == nil {
		err = os.Rename(f.Name(), s.path)
	}
	if err != nil {
		_ = os.Remove(f.Name())
	}
	return err
}

// clean drops out-of-range values, so the default width is used instead.
func (v Settings) clean() Settings {
	if v.SideWidth < minSideWidth || v.SideWidth > maxSideWidth {
		v.SideWidth = 0
	}
	return v
}
