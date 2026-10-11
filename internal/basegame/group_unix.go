//go:build !windows

package basegame

import (
	"os/exec"
	"syscall"
)

// group is a started process and anything it starts.
type group struct{ pid int }

// startGroup starts cmd in a process group of its own.
func startGroup(cmd *exec.Cmd) (*group, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &group{pid: cmd.Process.Pid}, nil
}

func (g *group) kill() {
	syscall.Kill(-g.pid, syscall.SIGKILL)
}

func (g *group) close() {}
