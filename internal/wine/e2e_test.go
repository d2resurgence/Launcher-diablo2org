package wine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/diablo2org/launcher/internal/fetch"
)

// TestRealSetup downloads the pinned Wine (or uses the system's, on Linux),
// makes a prefix and runs cmd /c ver in it, for checking a machine by hand.
// It's skipped unless LAUNCHER_E2E_WINE names an empty or new folder:
//
//	LAUNCHER_E2E_WINE=/tmp/wine-e2e go test ./internal/wine -run TestRealSetup -v -timeout 30m
func TestRealSetup(t *testing.T) {
	root := os.Getenv("LAUNCHER_E2E_WINE")
	if root == "" {
		t.Skip("LAUNCHER_E2E_WINE isn't set")
	}
	root, _ = filepath.Abs(root)

	log, err := os.Create(filepath.Join(t.TempDir(), "wine.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		log.Close()
		if t.Failed() {
			data, _ := os.ReadFile(log.Name())
			t.Logf("wine.log:\n%s", data)
		}
	}()

	s := Setup{
		Manager: Manager{
			Dir:      filepath.Join(root, "runtime", "wine"),
			CacheDir: filepath.Join(root, "cache", "wine"),
			Pin:      DefaultPin,
			Client:   fetch.New(Hosts),
		},
		PrefixDir: filepath.Join(root, "prefix"),
		Log:       log,
		UseSystem: os.Getenv("LAUNCHER_E2E_WINE_SYSTEM") != "",
	}

	ctx := context.Background()
	start := time.Now()
	last := time.Now()
	p, err := s.Run(ctx, func(pr Progress) {
		if pr.Total == 0 || time.Since(last) > 5*time.Second {
			t.Logf("%s %d/%d MB", pr.Step, pr.Done>>20, pr.Total>>20)
			last = time.Now()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("set up in %s: %s at %s", time.Since(start).Round(time.Second), p.Runtime.Version, p.Runtime.Wine)

	v, err := p.WindowsVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("cmd /c ver: %s", v)

	if target, err := os.Readlink(filepath.Join(p.Dir, "dosdevices", "z:")); err != nil || target != "/" {
		t.Errorf("z: -> %q, %v", target, err)
	}

	// A second run finds everything ready.
	start = time.Now()
	if _, err := s.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	t.Logf("second run: %s", time.Since(start).Round(time.Millisecond))
}
