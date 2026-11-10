//go:build unix

package paths

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"
)

func TestReadRegularFifo(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "pipe.md")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skip(err)
	}
	if _, err := ReadRegular(fifo, 100); !errors.Is(err, ErrUnsupported) {
		t.Errorf("fifo: %v", err)
	}
}
