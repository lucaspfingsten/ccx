// Package clipboard provides cross-platform copy-to-clipboard by shelling out
// to the platform's standard clipboard CLI (pbcopy / clip / wl-copy / xclip /
// xsel).
package clipboard

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

// ErrNoClipboard is returned when no supported clipboard tool is found on PATH.
var ErrNoClipboard = errors.New("no clipboard tool found (looked for pbcopy/clip/wl-copy/xclip/xsel)")

// Copy writes text to the system clipboard. Returns ErrNoClipboard if no
// supported tool is available, or the underlying error if the tool fails.
func Copy(text string) error {
	cmd := pickCommand()
	if cmd == nil {
		return ErrNoClipboard
	}
	c := exec.Command(cmd[0], cmd[1:]...)
	c.Stdin = strings.NewReader(text)
	return c.Run()
}

func pickCommand() []string {
	switch runtime.GOOS {
	case "darwin":
		if which("pbcopy") {
			return []string{"pbcopy"}
		}
	case "windows":
		if which("clip") {
			return []string{"clip"}
		}
	case "linux", "freebsd", "openbsd", "netbsd":
		if which("wl-copy") {
			return []string{"wl-copy"}
		}
		if which("xclip") {
			return []string{"xclip", "-selection", "clipboard"}
		}
		if which("xsel") {
			return []string{"xsel", "--clipboard", "--input"}
		}
	}
	return nil
}

func which(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
