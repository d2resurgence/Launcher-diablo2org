package wine

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// DefaultPin is the runtime the launcher downloads on this platform. The
// pinned build is amd64; elsewhere only the system's Wine can be used.
var DefaultPin = func() Pin {
	if runtime.GOARCH == "amd64" {
		return LinuxPin
	}
	return Pin{}
}()

// NeedsRosetta is false off macOS.
func NeedsRosetta(ctx context.Context) bool { return false }

// InstallRosetta has nothing to do off macOS.
func InstallRosetta(ctx context.Context) error { return nil }

// SystemWine is the player's own Wine, if it's on the PATH and recent
// enough. Whether it runs 32-bit programs is only known once a prefix is
// made; see Prefix.Check32.
func SystemWine(ctx context.Context) (*Runtime, error) {
	wine, err := exec.LookPath("wine")
	if err != nil {
		return nil, ErrNoRuntime
	}
	server, err := exec.LookPath("wineserver")
	if err != nil {
		return nil, ErrNoRuntime
	}

	rt := &Runtime{Wine: wine, Wineserver: server, System: true}
	v, err := rt.WineVersion(ctx)
	if err != nil {
		return nil, err
	}
	if !atLeast(v, MinSystemVersion) {
		return nil, fmt.Errorf("the system's %s is older than %d.%d", v, MinSystemVersion[0], MinSystemVersion[1])
	}
	rt.Version = "system " + v
	return rt, nil
}

// InstallHint is the command that installs Wine on this distribution, for a
// player whose system can't run the downloaded Wine.
func InstallHint() string {
	return installHint(osRelease("/etc/os-release"))
}

func installHint(ids []string) string {
	for _, id := range ids {
		switch id {
		case "debian", "ubuntu", "linuxmint", "pop":
			return "sudo apt install wine"
		case "fedora", "rhel", "centos":
			return "sudo dnf install wine"
		case "arch", "manjaro", "endeavouros":
			return "sudo pacman -S wine"
		case "opensuse", "suse":
			return "sudo zypper install wine"
		}
	}
	return "Install Wine with your distribution's package manager."
}

// osRelease is the distribution's ID followed by its ID_LIKE entries.
func osRelease(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var id, like []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"'`)
		switch k {
		case "ID":
			id = []string{v}
		case "ID_LIKE":
			like = strings.Fields(v)
		}
	}
	return append(id, like...)
}
