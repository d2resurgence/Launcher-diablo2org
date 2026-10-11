package wine

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// DefaultPin is the runtime the launcher downloads on this platform.
var DefaultPin = MacPin

// NeedsRosetta reports whether Wine needs Rosetta 2 here and it isn't
// installed. The macOS Wine build is x86_64; Apple Silicon runs it through
// Rosetta.
func NeedsRosetta(ctx context.Context) bool {
	if runtime.GOARCH != "arm64" {
		return false
	}
	return exec.CommandContext(ctx, "arch", "-x86_64", "/usr/bin/true").Run() != nil
}

// InstallRosetta installs Rosetta 2, asking for an administrator's password
// through the standard macOS prompt.
func InstallRosetta(ctx context.Context) error {
	const script = `do shell script "/usr/sbin/softwareupdate --install-rosetta --agree-to-license" with administrator privileges`
	out, err := exec.CommandContext(ctx, "osascript", "-e", script).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		// osascript's -128 is the player pressing Cancel.
		if strings.Contains(msg, "(-128)") {
			return fmt.Errorf("installing Rosetta was cancelled")
		}
		return fmt.Errorf("installing Rosetta: %w: %s", err, msg)
	}
	if NeedsRosetta(ctx) {
		return fmt.Errorf("Rosetta still isn't available after installing it")
	}
	return nil
}

// SystemWine is never used on macOS: the launcher's own runtime is.
func SystemWine(ctx context.Context) (*Runtime, error) {
	return nil, ErrNoRuntime
}

// InstallHint is what to tell a player whose system can't run the
// downloaded Wine.
func InstallHint() string {
	return ""
}
