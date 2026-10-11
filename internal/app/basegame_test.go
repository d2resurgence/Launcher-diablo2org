package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallFolder(t *testing.T) {
	empty := t.TempDir()
	if got := installFolder(empty); got != empty {
		t.Errorf("empty folder: %q", got)
	}

	full := t.TempDir()
	os.WriteFile(filepath.Join(full, "notes.txt"), nil, 0o644)
	if got := installFolder(full); got != filepath.Join(full, "Diablo II") {
		t.Errorf("folder with files: %q", got)
	}

	missing := filepath.Join(t.TempDir(), "new")
	if got := installFolder(missing); got != missing {
		t.Errorf("new folder: %q", got)
	}
}
