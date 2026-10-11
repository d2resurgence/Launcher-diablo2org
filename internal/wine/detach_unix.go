//go:build !windows

package wine

import (
	"os/exec"
	"syscall"
)

// detach starts cmd in its own process group, so a signal to the launcher,
// such as Ctrl+C in a terminal, doesn't end the game with it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
