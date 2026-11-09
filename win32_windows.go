//go:build windows

package main

import "golang.org/x/sys/windows"

// Win32 procedures used by the app. Only system DLLs are loaded (from
// System32).

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	ole32  = windows.NewLazySystemDLL("ole32.dll")

	procGetDpiForSystem  = user32.NewProc("GetDpiForSystem")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
)

const (
	smCxScreen = 0
	smCyScreen = 1
)
