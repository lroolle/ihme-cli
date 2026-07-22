// Package clip copies text to the system clipboard, best-effort.
package clip

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Copy puts text on the system clipboard. It shells out to the
// platform tool (pbcopy, xclip) and reports an error when none is
// available — callers treat success as a convenience, not a promise.
func Copy(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		cmd = exec.Command("xclip", "-selection", "clipboard")
	default:
		return fmt.Errorf("clipboard not supported on %s", runtime.GOOS)
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
