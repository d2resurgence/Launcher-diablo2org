package basegame

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/diablo2org/launcher/internal/install"
)

// TestRealInstall downloads and installs the real game, for checking a
// machine by hand. It's skipped unless LAUNCHER_E2E_LEGACY_DL names a
// blizzard-legacy-dl. The keys come from the environment, never the code:
//
//	LAUNCHER_E2E_LEGACY_DL=bin/legacy-dl/windows-amd64/blizzard-legacy-dl.exe
//	LAUNCHER_E2E_BASE=C:\Games\Diablo II     (empty or new)
//	LAUNCHER_E2E_CDKEY=…  LAUNCHER_E2E_CDKEY_EXPANSION=…
//	LAUNCHER_E2E_VERSION=1.13c               (optional)
//
//	go test ./internal/basegame -run TestRealInstall -v -timeout 2h
func TestRealInstall(t *testing.T) {
	exe := os.Getenv("LAUNCHER_E2E_LEGACY_DL")
	if exe == "" {
		t.Skip("LAUNCHER_E2E_LEGACY_DL isn't set")
	}
	exe, _ = filepath.Abs(exe)
	base, _ := filepath.Abs(os.Getenv("LAUNCHER_E2E_BASE"))

	in := Installer{Exe: exe, CacheDir: filepath.Join(filepath.Dir(base), "legacy-dl-cache")}
	o := Options{
		Version:      os.Getenv("LAUNCHER_E2E_VERSION"),
		BaseDir:      base,
		ClassicKey:   os.Getenv("LAUNCHER_E2E_CDKEY"),
		ExpansionKey: os.Getenv("LAUNCHER_E2E_CDKEY_EXPANSION"),
	}

	last := time.Now()
	res, err := in.Install(context.Background(), o, func(p Progress) {
		if p.Total == 0 || time.Since(last) > 10*time.Second {
			t.Logf("%s %s %d/%d", p.Product, p.Stage, p.Done, p.Total)
			last = time.Now()
		}
	})
	for _, w := range res.Warnings {
		t.Logf("warning: %s", w)
	}
	if err != nil {
		t.Fatal(err)
	}

	r, err := install.Check(base, true)
	if err != nil || !r.OK() {
		t.Fatalf("Check: %+v, %v", r, err)
	}
	t.Logf("installed in %s; cache %d MB", base, in.CacheSize()>>20)
}
