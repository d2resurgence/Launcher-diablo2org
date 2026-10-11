package wine

import "os/exec"

// detach has nothing to do: Wine isn't used on Windows.
func detach(cmd *exec.Cmd) {}
