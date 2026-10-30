//go:build windows

package ui

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	user32      = syscall.NewLazyDLL("user32.dll")
	messageBoxW = user32.NewProc("MessageBoxW")
)

func notifyFailure(err error) {
	if err == nil {
		return
	}
	caption, _ := syscall.UTF16PtrFromString("cthrone")
	text, _ := syscall.UTF16PtrFromString(err.Error() + "\n\nSee %AppData%\\cthrone\\gui.log for details.")
	messageBoxW.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(caption)), 0x00000010)
}
