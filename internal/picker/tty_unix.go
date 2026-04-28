//go:build !windows

package picker

import (
	"os"
	"syscall"
	"unsafe"
)

// isTerminalFD asks the kernel for a TTY-only ioctl (TIOCGWINSZ). It returns
// true only when the call succeeds, which is exactly what an interactive
// terminal allows. /dev/null and pipes both fail with ENOTTY.
func isTerminalFD(f *os.File) bool {
	if f == nil {
		return false
	}
	var ws struct {
		Row, Col, X, Y uint16
	}
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		f.Fd(),
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(&ws)),
	)
	return errno == 0
}
