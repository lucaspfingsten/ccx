//go:build windows

package picker

import (
	"os"
	"syscall"
	"unsafe"
)

// isTerminalFD on Windows calls GetConsoleMode, which only succeeds on a
// console handle (not a redirected pipe or file).
func isTerminalFD(f *os.File) bool {
	if f == nil {
		return false
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getConsoleMode := kernel32.NewProc("GetConsoleMode")
	var mode uint32
	r, _, _ := getConsoleMode.Call(f.Fd(), uintptr(unsafe.Pointer(&mode)))
	return r != 0
}
