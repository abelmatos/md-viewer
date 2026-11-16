//go:build windows

package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/jchv/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

const windowClass = "MDViewerWindow"

// window is the app's only top-level window and its WebView2. Its fields are
// used on the UI thread only, except queue.
type window struct {
	hwnd    uintptr
	browser *edge.Chromium
	page    string // the app's page, shown with NavigateToString

	ownLoad  bool // the next navigation is the app's own page
	restores int  // how often the page had to be put back

	mu    sync.Mutex
	queue []func()
}

// theWindow lets the window procedure, a callback created by
// windows.NewCallback, reach the window.
var theWindow *window

// post runs f on the UI thread after the current message. Safe from any
// goroutine. Posting to the window (not the thread) keeps it working while a
// dialog runs its own modal message loop.
func (w *window) post(f func()) {
	w.mu.Lock()
	w.queue = append(w.queue, f)
	w.mu.Unlock()
	_, _, _ = procPostMessageW.Call(w.hwnd, wmApp, 0, 0)
}

func (w *window) drain() {
	w.mu.Lock()
	q := w.queue
	w.queue = nil
	w.mu.Unlock()
	for _, f := range q {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("panic on the UI thread: %v", r)
				}
			}()
			f()
		}()
	}
}

func wndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	w := theWindow
	switch msg {
	case wmApp:
		if w != nil {
			w.drain()
		}
		return 0
	case wmSize:
		if w != nil && w.browser != nil {
			w.browser.Resize()
		}
	case wmActivate:
		// Keep keyboard focus in the page; DefWindowProc would move it to
		// the bare window.
		if w != nil && w.browser != nil && wParam&0xFFFF != waInactive {
			w.browser.Focus()
			return 0
		}
	case wmSetFocus:
		if w != nil && w.browser != nil {
			w.browser.Focus()
			return 0
		}
	case wmMove:
		if w != nil && w.browser != nil {
			_ = w.browser.NotifyParentWindowPositionChanged()
		}
	case wmDpiChanged:
		// Moved to a monitor with another scale: take the suggested rect.
		// lParam points to a RECT; reinterpret it without a uintptr-to-
		// pointer conversion, which go vet rejects.
		r := *(**windows.Rect)(unsafe.Pointer(&lParam))
		x, y, cx, cy := uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top) //nolint:gosec // G115: Win32 reads these as int; negative x/y are valid
		_, _, _ = procSetWindowPos.Call(hwnd, 0, x, y, cx, cy, swpNoZOrder|swpNoActivate)
		return 0
	case wmClose:
		// Closed while WebView2 is still starting: go-webview2's Embed
		// waits in its own message loop and would crash on the WM_QUIT
		// that destroying the window posts, so end here.
		if w == nil || w.browser == nil {
			startupDone() // not a startup failure; no message box
			log.Print("closed during startup")
			os.Exit(0)
		}
	case wmDestroy:
		_, _, _ = procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   windows.Handle
	icon       windows.Handle
	cursor     windows.Handle
	background windows.Handle
	menuName   *uint16
	className  *uint16
	iconSm     windows.Handle
}

type winMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
	private uint32
}

// createWindow registers the class and creates the (visible) main window,
// painted in bg until the page draws, with a dark title bar if dark.
func createWindow(bg uint32, dark bool) (uintptr, error) {
	var inst windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &inst); err != nil {
		return 0, err
	}
	icon, _, _ := procLoadImageW.Call(uintptr(inst), iconID, imageIcon, 0, 0, lrDefaultSize|lrShared)
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	brush, _, _ := procCreateSolidBrush.Call(uintptr(bg))
	className := utf16(windowClass)
	wc := wndClassEx{
		wndProc:    windows.NewCallback(wndProc),
		instance:   inst,
		icon:       windows.Handle(icon),
		cursor:     windows.Handle(cursor),
		background: windows.Handle(brush),
		className:  className,
		iconSm:     windows.Handle(icon),
	}
	wc.size = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return 0, fmt.Errorf("RegisterClassEx: %w", err)
	}

	cx, cy := windowSize()
	sw, _, _ := procGetSystemMetrics.Call(smCxScreen)
	sh, _, _ := procGetSystemMetrics.Call(smCyScreen)
	x, y := (int(sw)-int(cx))/2, (int(sh)-int(cy))/2 //nolint:gosec // G115: screen sizes fit in int
	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(utf16(appTitle))),
		wsOverlappedWindow,
		uintptr(x), uintptr(y), uintptr(cx), uintptr(cy), //nolint:gosec // G115: Win32 reads these as int
		0, 0, uintptr(inst), 0,
	)
	if hwnd == 0 {
		return 0, fmt.Errorf("CreateWindowEx: %w", err)
	}
	// Before showing it, or Windows 10 paints the title bar light first.
	setDarkTitleBar(hwnd, dark)
	_, _, _ = procShowWindow.Call(hwnd, windows.SW_SHOWNORMAL)
	_, _, _ = procUpdateWindow.Call(hwnd)
	return hwnd, nil
}

// windowSize scales the default size to the screen's DPI, keeping the window
// within 90% of the screen.
func windowSize() (w, h uint) {
	dpi := uintptr(96)
	if procGetDpiForSystem.Find() == nil { // Windows 10 1607 and later
		if d, _, _ := procGetDpiForSystem.Call(); d != 0 {
			dpi = d
		}
	}
	sw, _, _ := procGetSystemMetrics.Call(smCxScreen)
	sh, _, _ := procGetSystemMetrics.Call(smCyScreen)
	w = min(uint(windowWidth*dpi/96), uint(sw*9/10))
	h = min(uint(windowHeight*dpi/96), uint(sh*9/10))
	return w, h
}

// embed creates the WebView2 in the window and locks it to the app's page.
func (w *window) embed(dataPath string, debug bool, bg [3]int, onMessage func(string)) error {
	b := edge.NewChromium()
	b.DataPath = dataPath
	b.MessageCallback = onMessage
	b.WebResourceRequestedCallback = w.onRequest
	b.NavigationCompletedCallback = w.onNavigated
	if !b.Embed(w.hwnd) {
		return errors.New("the WebView2 runtime failed to start (see the log)")
	}
	w.browser = b
	// Only the app's own page may load as a document: any other navigation,
	// e.g. a link that slipped past the page, gets an empty 403 instead of
	// the remote page, and onNavigated then puts the app's page back.
	b.AddWebResourceRequestedFilter("*", edge.COREWEBVIEW2_WEB_RESOURCE_CONTEXT_DOCUMENT)

	if settings, err := b.GetSettings(); err == nil {
		_ = settings.PutAreDefaultContextMenusEnabled(debug)
		_ = settings.PutAreDevToolsEnabled(debug)
		_ = settings.PutIsStatusBarEnabled(false)
	}
	if c := b.GetController(); c != nil {
		if c2 := c.GetICoreWebView2Controller2(); c2 != nil {
			_ = c2.PutDefaultBackgroundColor(edge.COREWEBVIEW2_COLOR{A: 255, R: uint8(bg[0]), G: uint8(bg[1]), B: uint8(bg[2])}) //nolint:gosec // G115: 0..255
		}
	}
	b.Resize()
	return nil
}

// load shows the app's page.
func (w *window) load() {
	w.ownLoad = true
	w.browser.NavigateToString(w.page)
}

// onRequest blocks every document load except the app's own page, which
// arrives as about:blank or data: (NavigateToString).
func (w *window) onRequest(req *edge.ICoreWebView2WebResourceRequest, args *edge.ICoreWebView2WebResourceRequestedEventArgs) {
	uri, err := req.GetUri()
	if err == nil && (strings.HasPrefix(uri, "about:") || strings.HasPrefix(uri, "data:")) {
		return
	}
	log.Printf("blocked a page load: %.200s", uri)
	resp, err := w.browser.Environment().CreateWebResourceResponse([]byte("Blocked by MD Viewer"), 403, "Forbidden", "Content-Type: text/plain")
	if err != nil {
		log.Printf("block page load: %v", err)
		return
	}
	_ = args.PutResponse(resp)
}

// onNavigated notices when something other than the app's page was loaded
// (a blocked link, or a reload) and puts the app's page back; the page then
// asks Go for the current state.
func (w *window) onNavigated(*edge.ICoreWebView2, *edge.ICoreWebView2NavigationCompletedEventArgs) {
	startupDone()
	if w.ownLoad {
		w.ownLoad = false
		return
	}
	w.restores++
	if w.restores > 20 {
		log.Print("the page keeps being replaced; giving up")
		return
	}
	log.Print("the page was replaced; restoring it")
	w.post(w.load)
}

// loop runs the message loop until the window closes.
func (w *window) loop() {
	var m winMsg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 { //nolint:gosec // G115: GetMessage returns a BOOL; 0 is WM_QUIT, -1 an error
			return
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func (w *window) setTitle(title string) {
	t, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	_, _, _ = procSetWindowTextW.Call(w.hwnd, uintptr(unsafe.Pointer(t)))
}
