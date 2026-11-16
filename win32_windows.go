//go:build windows

package main

import "golang.org/x/sys/windows"

// Win32 procedures and constants used by the app. Only system DLLs are
// loaded (from System32).

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")
	ole32  = windows.NewLazySystemDLL("ole32.dll")

	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procShowWindow       = user32.NewProc("ShowWindow")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
	procSetWindowTextW   = user32.NewProc("SetWindowTextW")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procLoadImageW       = user32.NewProc("LoadImageW")
	procLoadCursorW      = user32.NewProc("LoadCursorW")
	procGetDpiForSystem  = user32.NewProc("GetDpiForSystem") // Windows 10 1607+
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
)

const (
	wsOverlappedWindow = 0x00CF0000
	wmDestroy          = 0x0002
	wmMove             = 0x0003
	wmSize             = 0x0005
	wmClose            = 0x0010
	wmActivate         = 0x0006
	wmSetFocus         = 0x0007
	wmDpiChanged       = 0x02E0
	wmApp              = 0x8000
	waInactive         = 0
	imageIcon          = 1
	lrDefaultSize      = 0x00000040
	lrShared           = 0x00008000
	idcArrow           = 32512
	swpNoZOrder        = 0x0004
	swpNoActivate      = 0x0010
	smCxScreen         = 0
	smCyScreen         = 1
)
