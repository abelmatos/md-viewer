//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/jchv/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
)

// Default window size in CSS pixels; scaled to the screen's DPI.
const (
	windowWidth  = 1200
	windowHeight = 850
	iconID       = 1 // RT_GROUP_ICON #1 in winres/winres.json
)

const webView2Download = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"

// Page background before the page draws, matching --bg in ui/index.html.
var (
	darkBg  = [3]int{0x0f, 0x0f, 0x0f}
	lightBg = [3]int{0xff, 0xff, 0xff}
)

func run(arg string, debug bool) int {
	// Resolve implicitly loaded DLLs from System32 only, so a planted
	// WebView2Loader.dll next to the exe (e.g. in Downloads) is never loaded;
	// go-webview2 then uses its embedded loader.
	if err := windows.SetDefaultDllDirectories(windows.LOAD_LIBRARY_SEARCH_SYSTEM32); err != nil {
		showError(fmt.Sprintf("Can't restrict DLL loading: %v", err))
		return 1
	}
	closeLog := openLog()
	defer closeLog()
	log.Printf("md-viewer %s starting", version)

	v, err := webviewloader.GetInstalledVersion()
	if err != nil || v == "" {
		log.Printf("WebView2 runtime not found: %v", err)
		showError("MD Viewer needs the Microsoft Edge WebView2 Runtime, which comes with Windows 11 " +
			"and recent Windows 10.\n\nInstall it from " + webView2Download)
		return 1
	}
	log.Printf("WebView2 runtime %s", v)

	dark := systemDark()
	bg := lightBg
	if dark {
		bg = darkBg
	}
	hwnd, err := createWindow(colorRef(bg), dark)
	if err != nil {
		fatal(err)
		return 1
	}
	w := &window{hwnd: hwnd}
	theWindow = w

	bridge, err := NewBridge(w.post, func(js string) { w.browser.Eval(js) })
	if err != nil {
		fatal(err)
		return 1
	}
	if w.page, err = shellHTML(bridge.Key()); err != nil {
		fatal(err)
		return 1
	}
	p := &winPlatform{w: w, notify: bridge.Notify}
	bridge.RegisterApp(NewApp(p), p, defaultSettingsStore(), arg)

	startupBegin()
	if err := w.embed(webviewDataDir(), debug, bg, bridge.Handle); err != nil {
		fatal(err)
		return 1
	}
	w.load()
	w.loop()
	log.Print("closed")
	return 0
}

// fatal logs err and tells the user, pointing to the log.
func fatal(err error) {
	startupDone() // one message box is enough
	log.Printf("fatal: %v", err)
	showError(fmt.Sprintf("MD Viewer couldn't start: %v\n\nLog: %s", err, logPath()))
}

// defaultSettingsStore uses %APPDATA%\md-viewer\settings.json.
func defaultSettingsStore() *settingsStore {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return &settingsStore{path: filepath.Join(dir, "md-viewer", "settings.json")}
}

// webviewDataDir keeps WebView2's profile out of the exe's folder.
func webviewDataDir() string {
	return filepath.Join(localDir(), "WebView2")
}

// localDir is %LOCALAPPDATA%\md-viewer.
func localDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "md-viewer")
}

// winPlatform implements Platform with Win32 calls. Its methods may be
// called from any goroutine (the dialogs from the UI thread) and hand window
// work to the UI thread.
type winPlatform struct {
	w      *window
	notify func(error)
}

func (p *winPlatform) SetTitle(title string) { p.w.post(func() { p.w.setTitle(title) }) }

func (p *winPlatform) SetTheme(t Theme) { p.w.post(func() { applyTheme(p.w.hwnd, t) }) }

func (p *winPlatform) PickFile() (string, error) { return pickPath(p.w.hwnd, false) }

func (p *winPlatform) PickFolder() (string, error) { return pickPath(p.w.hwnd, true) }

func (p *winPlatform) OpenURL(url string) error {
	p.w.post(func() {
		if err := shellOpen(p.w.hwnd, url, ""); err != nil {
			p.notify(fmt.Errorf("open %s: %w", url, err))
		}
	})
	return nil
}

func (p *winPlatform) Reveal(path string) error {
	p.w.post(func() {
		var err error
		if fi, statErr := os.Stat(path); statErr == nil && fi.IsDir() {
			err = shellOpen(p.w.hwnd, path, "") // opens the folder in Explorer
		} else {
			err = shellOpen(p.w.hwnd, explorerPath(), `/select,"`+path+`"`)
		}
		if err != nil {
			p.notify(fmt.Errorf("show %s: %w", path, err))
		}
	})
	return nil
}
