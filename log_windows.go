//go:build windows

package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// The app has no console (-H windowsgui), so logs go to
// %LOCALAPPDATA%\md-viewer\md-viewer.log. While WebView2 starts (between
// startupBegin and the page's first load), only go-webview2 logs, and only
// to report a failure, often with log.Fatal, which would otherwise exit
// without a word: those lines are also shown in a message box.

const maxLogSize = 1 << 20

var starting atomic.Bool

func logPath() string { return filepath.Join(localDir(), "md-viewer.log") }

// startupBegin and startupDone bracket WebView2's startup.
func startupBegin() { starting.Store(true) }
func startupDone()  { starting.Store(false) }

type startupWriter struct{ file io.Writer }

func (w startupWriter) Write(p []byte) (int, error) {
	if w.file != nil {
		_, _ = w.file.Write(p)
	}
	if starting.Load() {
		showError("MD Viewer couldn't start the WebView2 runtime:\n\n" + strings.TrimSpace(string(p)) + "\n\nLog: " + logPath())
	}
	return len(p), nil
}

// openLog sends the log to the log file (rotating it past maxLogSize) and
// returns a function that closes it.
func openLog() func() {
	path := logPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		log.SetOutput(startupWriter{})
		return func() {}
	}
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxLogSize {
		_ = os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // G304: fixed path under %LOCALAPPDATA%
	if err != nil {
		log.SetOutput(startupWriter{})
		return func() {}
	}
	log.SetOutput(startupWriter{file: f})
	return func() { _ = f.Close() }
}
