package wine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallHint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "os-release")
	os.WriteFile(path, []byte("NAME=\"Linux Mint\"\nID=linuxmint\nID_LIKE=\"ubuntu debian\"\n"), 0o644)
	if got := installHint(osRelease(path)); got != "sudo apt install wine" {
		t.Errorf("Mint: %q", got)
	}

	tests := map[string][]string{
		"sudo dnf install wine": {"fedora"},
		"sudo pacman -S wine":   {"endeavouros", "arch"},
	}
	for want, ids := range tests {
		if got := installHint(ids); got != want {
			t.Errorf("%v: %q", ids, got)
		}
	}
	if got := installHint(nil); got == "" {
		t.Error("no hint for an unknown distribution")
	}
}
