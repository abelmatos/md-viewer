//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// The title bar is painted in the toolbar's colours, so the two read as one
// surface. The page sends its colours (mdTheme) when it loads and whenever
// Windows switches between light and dark.

// themeColors are the toolbar's background, text and border colours as
// [r, g, b], read from the page's CSS.
type themeColors struct {
	Dark   bool   `json:"dark"`
	Bg     [3]int `json:"bg"`
	Fg     [3]int `json:"fg"`
	Border [3]int `json:"border"`
}

// colorRef packs [r, g, b] as a Win32 COLORREF (0x00BBGGRR).
func colorRef(c [3]int) uint32 {
	clamp := func(v int) uint32 { return uint32(max(0, min(255, v))) } //nolint:gosec // G115: clamped to 0..255
	return clamp(c[0]) | clamp(c[1])<<8 | clamp(c[2])<<16
}

func dwmSet(hwnd uintptr, attr uint32, value uint32) {
	// Unsupported attributes (caption colours before Windows 11) just fail.
	_ = windows.DwmSetWindowAttribute(windows.HWND(hwnd), attr, unsafe.Pointer(&value), uint32(unsafe.Sizeof(value)))
}

// setDarkTitleBar switches the title bar between the light and dark system
// look. This is all Windows 10 supports.
func setDarkTitleBar(hwnd uintptr, dark bool) {
	v := uint32(0)
	if dark {
		v = 1
	}
	dwmSet(hwnd, windows.DWMWA_USE_IMMERSIVE_DARK_MODE, v)
}

// applyTheme paints the title bar in the page's toolbar colours.
func applyTheme(hwnd uintptr, t themeColors) {
	setDarkTitleBar(hwnd, t.Dark)
	dwmSet(hwnd, windows.DWMWA_CAPTION_COLOR, colorRef(t.Bg))
	dwmSet(hwnd, windows.DWMWA_TEXT_COLOR, colorRef(t.Fg))
	dwmSet(hwnd, windows.DWMWA_BORDER_COLOR, colorRef(t.Border))
}

// systemDark reports whether Windows apps use the dark theme, so the title
// bar starts out right before the page has loaded.
func systemDark() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer func() { _ = k.Close() }()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return err == nil && v == 0
}
