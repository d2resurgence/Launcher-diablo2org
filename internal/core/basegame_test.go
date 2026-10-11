package core

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/diablo2org/launcher/internal/basegame"
)

func TestBaseSetupUnavailable(t *testing.T) {
	h := newHarness(t)

	s := h.m.BaseSetup()
	if s.Available || s.Unavailable == "" {
		t.Errorf("no installer, but %+v", s)
	}
	if s.DefaultVersion != "1.13c" || len(s.Versions) == 0 {
		t.Errorf("versions %q, default %q", s.Versions, s.DefaultVersion)
	}

	if _, err := h.m.InstallBase(context.Background(), basegame.Options{}, nil); err == nil {
		t.Error("installed with no installer")
	}
}

func TestInstallBase(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in legacy-dl is a shell script")
	}
	h := newHarness(t)

	// Writes the archives into --game, relative to where it's run.
	exe := filepath.Join(t.TempDir(), "blizzard-legacy-dl")
	script := `#!/bin/sh
while [ $# -gt 0 ]; do [ "$1" = --game ] && game="$2"; shift; done
mkdir -p "$game"
for a in d2data d2sfx d2speech d2exp; do echo mpq > "$game/$a.mpq"; done
echo "the game is in $game" >&2
`
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	h.m.SetBaseInstaller(BaseInstaller{
		Installer:  basegame.Installer{Exe: exe, CacheDir: filepath.Join(root, "cache", "legacy-dl")},
		DefaultDir: filepath.Join(root, "Diablo II"),
	})

	s := h.m.BaseSetup()
	if !s.Available || s.DefaultDir != filepath.Join(root, "Diablo II") || s.Owner == "" {
		t.Fatalf("setup %+v", s)
	}

	base := filepath.Join(root, "Diablo II")
	o := basegame.Options{BaseDir: base, ClassicKey: "x", ExpansionKey: "y"}
	if _, err := h.m.InstallBase(context.Background(), o, nil); err != nil {
		t.Fatal(err)
	}
	if h.m.Base() != base {
		t.Errorf("base = %q, want the new install", h.m.Base())
	}
	if r, _, err := h.m.CheckBase(); err != nil || !r.OK() {
		t.Errorf("new base: %+v, %v", r, err)
	}

	// The choice is saved.
	state, err := h.store.Load()
	if err != nil || state.BasePath != base {
		t.Errorf("saved base %q, %v", state.BasePath, err)
	}
}
