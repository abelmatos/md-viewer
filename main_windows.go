//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

// Default window size in CSS pixels; scaled to the screen's DPI.
const (
	windowWidth  = 1200
	windowHeight = 850
	iconID       = 1 // RT_GROUP_ICON #1 in winres/winres.json
)

const webView2Download = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"

func run(arg string, debug bool) int {
	// Resolve implicitly loaded DLLs from System32 only, so a planted
	// WebView2Loader.dll next to the exe (e.g. in Downloads) is never loaded;
	// go-webview2 then uses its embedded loader.
	if err := windows.SetDefaultDllDirectories(windows.LOAD_LIBRARY_SEARCH_SYSTEM32); err != nil {
		showError(fmt.Sprintf("Can't restrict DLL loading: %v", err))
		return 1
	}
	page, err := shellHTML()
	if err != nil {
		showError(err.Error())
		return 1
	}

	w, h := windowSize()
	wv := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     debug,
		DataPath:  webviewDataDir(),
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  appTitle,
			Width:  w,
			Height: h,
			IconId: iconID,
			Center: true,
		},
	})
	if wv == nil {
		showError("MD Viewer needs the Microsoft Edge WebView2 Runtime, which comes with Windows 11 " +
			"and recent Windows 10.\n\nInstall it from " + webView2Download)
		return 1
	}
	defer wv.Destroy()

	p := &winPlatform{wv: wv}
	app := NewApp(p)
	bindings := map[string]any{
		"mdStart": func() (*View, error) {
			if arg == "" {
				return &View{Empty: true}, nil
			}
			return app.Open(arg)
		},
		"mdNavigate": app.Navigate,
		"mdShow":     app.Show,
		"mdBack":     app.Back,
		"mdForward":  app.Forward,
		"mdReload":   app.Reload,
		"mdImage":    app.Image,
		// Dialogs run a modal loop, which must not happen inside a WebView2
		// event handler, where bindings run. They are queued to run after it
		// returns and deliver their result to the page themselves.
		"mdPickFile":   func() { p.later(app.PickFile) },
		"mdPickFolder": func() { p.later(app.PickFolder) },
	}
	for name, f := range bindings {
		if err := wv.Bind(name, f); err != nil {
			showError(fmt.Sprintf("bind %s: %v", name, err))
			return 1
		}
	}
	wv.SetHtml(page)
	wv.Run()
	return 0
}

// windowSize scales the default size to the screen's DPI, keeping the window
// within 90% of the screen.
func windowSize() (w, h uint) {
	dpi, _, _ := procGetDpiForSystem.Call()
	if dpi == 0 {
		dpi = 96
	}
	sw, _, _ := procGetSystemMetrics.Call(smCxScreen)
	sh, _, _ := procGetSystemMetrics.Call(smCyScreen)
	w = min(uint(windowWidth*dpi/96), uint(sw*9/10))
	h = min(uint(windowHeight*dpi/96), uint(sh*9/10))
	return w, h
}

// webviewDataDir keeps WebView2's profile out of the exe's folder.
func webviewDataDir() string {
	base, err := os.UserCacheDir() // %LOCALAPPDATA%
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "md-viewer", "WebView2")
}

// winPlatform implements Platform with Win32 calls.
type winPlatform struct {
	wv webview2.WebView
}

func (p *winPlatform) hwnd() uintptr { return uintptr(p.wv.Window()) }

func (p *winPlatform) SetTitle(title string) { p.wv.SetTitle(title) }

func (p *winPlatform) PickFile() (string, error) { return pickPath(p.hwnd(), false) }

func (p *winPlatform) PickFolder() (string, error) { return pickPath(p.hwnd(), true) }

func (p *winPlatform) OpenURL(url string) error {
	p.wv.Dispatch(func() {
		if err := shellOpen(p.hwnd(), url, ""); err != nil {
			p.fail(fmt.Errorf("open %s: %w", url, err))
		}
	})
	return nil
}

func (p *winPlatform) Reveal(path string) error {
	p.wv.Dispatch(func() {
		var err error
		if fi, statErr := os.Stat(path); statErr == nil && fi.IsDir() {
			err = shellOpen(p.hwnd(), path, "") // opens the folder in Explorer
		} else {
			err = shellOpen(p.hwnd(), explorerPath(), `/select,"`+path+`"`)
		}
		if err != nil {
			p.fail(fmt.Errorf("show %s: %w", path, err))
		}
	})
	return nil
}

// later runs f after the current event handler returns and sends its view or
// error to the page.
func (p *winPlatform) later(f func() (*View, error)) {
	p.wv.Dispatch(func() {
		v, err := f()
		if err != nil {
			p.fail(err)
			return
		}
		if v == nil {
			return // cancelled
		}
		b, err := json.Marshal(v)
		if err != nil {
			p.fail(err)
			return
		}
		p.wv.Eval("window.mdDeliver(" + string(b) + ")")
	})
}

func (p *winPlatform) fail(err error) {
	b, _ := json.Marshal(err.Error())
	p.wv.Eval("window.mdFail(" + string(b) + ")")
}
