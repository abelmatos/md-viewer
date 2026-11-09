//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// MessageBox flags.
const (
	mbOK            = 0x00000000
	mbIconError     = 0x00000010
	mbSetForeground = 0x00010000
)

func showError(msg string) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	t, _ := windows.UTF16PtrFromString(strings.ReplaceAll(msg, "\x00", ""))
	c, _ := windows.UTF16PtrFromString(appTitle)
	_, _ = windows.MessageBox(0, t, c, mbOK|mbIconError|mbSetForeground)
}

// shellOpen runs ShellExecute "open" on file. Callers pass only http(s) and
// mailto links, folders, or explorer.exe, never a document's own files.
func shellOpen(hwnd uintptr, file, args string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	f, err := windows.UTF16PtrFromString(file)
	if err != nil {
		return err
	}
	var a *uint16
	if args != "" {
		if a, err = windows.UTF16PtrFromString(args); err != nil {
			return err
		}
	}
	return windows.ShellExecute(windows.Handle(hwnd), verb, f, a, nil, windows.SW_SHOWNORMAL)
}

func explorerPath() string {
	if dir, err := windows.GetWindowsDirectory(); err == nil {
		return filepath.Join(dir, "explorer.exe")
	}
	return filepath.Join(os.Getenv("SystemRoot"), "explorer.exe")
}

// The common item dialog (IFileOpenDialog), called through its COM vtable.
var (
	clsidFileOpenDialog = windows.GUID{Data1: 0xDC1C5A9C, Data2: 0xE88A, Data3: 0x4DDE, Data4: [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	iidIFileOpenDialog  = windows.GUID{Data1: 0xD57C7288, Data2: 0xD4AD, Data3: 0x4768, Data4: [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60}}
)

// Vtable slots: IUnknown, then IModalWindow, IFileDialog and IShellItem.
const (
	vtRelease        = 2
	vtShow           = 3
	vtSetFileTypes   = 4
	vtSetOptions     = 9
	vtGetOptions     = 10
	vtSetTitle       = 17
	vtGetResult      = 20
	vtGetDisplayName = 5 // IShellItem
)

const (
	clsctxInprocServer  = 0x1
	fosPickFolders      = 0x20
	fosForceFileSystem  = 0x40
	fosPathMustExist    = 0x800
	fosFileMustExist    = 0x1000
	sigdnFileSysPath    = 0x80058000
	hresultCancelled    = 0x800704C7 // HRESULT_FROM_WIN32(ERROR_CANCELLED)
	hresultSuccess      = 0
	maxComVtableEntries = 32
)

// comObject is a COM interface pointer: its first word points to the vtable.
type comObject struct {
	vtbl *[maxComVtableEntries]uintptr
}

// call invokes a vtable method. uintptrescapes keeps pointer arguments
// alive, and off the movable stack, for the duration of the call.
//
//go:uintptrescapes
func (o *comObject) call(slot int, args ...uintptr) uint32 {
	r, _, _ := syscall.SyscallN(o.vtbl[slot], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return uint32(r) //nolint:gosec // G115: HRESULT is 32 bits
}

type filterSpec struct {
	name, spec *uint16
}

// pickPath shows the Windows open dialog for a Markdown file, or a folder,
// and returns the chosen path, or "" if the user cancelled. It must run on
// the UI thread, where COM is initialised.
func pickPath(owner uintptr, folder bool) (string, error) {
	var dlg *comObject
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidFileOpenDialog)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIFileOpenDialog)), uintptr(unsafe.Pointer(&dlg)))
	if hr != hresultSuccess || dlg == nil {
		return "", fmt.Errorf("open dialog: CoCreateInstance failed (0x%08X)", hr)
	}
	defer dlg.call(vtRelease)

	var opts uint32
	dlg.call(vtGetOptions, uintptr(unsafe.Pointer(&opts)))
	opts |= fosForceFileSystem | fosPathMustExist
	title := "Open a Markdown file"
	var filters []filterSpec
	if folder {
		opts |= fosPickFolders
		title = "Open a folder"
	} else {
		opts |= fosFileMustExist
		filters = []filterSpec{
			{utf16("Markdown files"), utf16("*.md;*.markdown;*.mdown;*.mkd;*.mkdn")},
			{utf16("All files"), utf16("*.*")},
		}
		dlg.call(vtSetFileTypes, uintptr(len(filters)), uintptr(unsafe.Pointer(&filters[0])))
	}
	dlg.call(vtSetOptions, uintptr(opts))
	dlg.call(vtSetTitle, uintptr(unsafe.Pointer(utf16(title))))

	switch hr := dlg.call(vtShow, owner); hr {
	case hresultSuccess:
	case hresultCancelled:
		return "", nil
	default:
		return "", fmt.Errorf("open dialog failed (0x%08X)", hr)
	}
	runtime.KeepAlive(filters)

	var item *comObject
	if hr := dlg.call(vtGetResult, uintptr(unsafe.Pointer(&item))); hr != hresultSuccess || item == nil {
		return "", fmt.Errorf("open dialog: no result (0x%08X)", hr)
	}
	defer item.call(vtRelease)
	var p *uint16
	if hr := item.call(vtGetDisplayName, sigdnFileSysPath, uintptr(unsafe.Pointer(&p))); hr != hresultSuccess || p == nil {
		return "", errors.New("open dialog: the chosen item has no file system path")
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(p))
	return windows.UTF16PtrToString(p), nil
}

func utf16(s string) *uint16 {
	p, _ := windows.UTF16PtrFromString(s)
	return p
}
