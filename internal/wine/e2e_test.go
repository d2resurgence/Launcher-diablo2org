package wine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/diablo2org/launcher/internal/fetch"
	"github.com/diablo2org/launcher/internal/launch"
	"github.com/diablo2org/launcher/internal/spec"
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

// TestRealLaunch runs a launch in the prefix TestRealSetup made: the Battle.net
// settings go into the prefix's registry and a stand-in Game.exe (Wine's own
// 32-bit hostname.exe) starts. Run TestRealSetup with the same folder first.
func TestRealLaunch(t *testing.T) {
	root := os.Getenv("LAUNCHER_E2E_WINE")
	if root == "" {
		t.Skip("LAUNCHER_E2E_WINE isn't set")
	}
	root, _ = filepath.Abs(root)
	ctx := context.Background()

	h := &Host{
		Setup: Setup{
			Manager:   Manager{Dir: filepath.Join(root, "runtime", "wine"), Pin: DefaultPin},
			PrefixDir: filepath.Join(root, "prefix"),
		},
		TempDir: filepath.Join(root, "tmp"),
	}
	if !h.Ready(ctx) {
		t.Fatal("no ready prefix: run TestRealSetup first")
	}

	// A base and server folder with a space and an accent, as players have.
	base := filepath.Join(root, "Diablo Ï")
	server := filepath.Join(base, "slashdiablo")
	os.MkdirAll(server, 0o755)
	exe, err := os.ReadFile(filepath.Join(root, "prefix", "drive_c", "windows", "syswow64", "hostname.exe"))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(server, "Game.exe"), exe, 0o755)

	p := &spec.Profile{
		Name:     "SlashDiablo",
		Game:     spec.Game{Version: "1.13c", Saves: "isolated"},
		Gateways: []spec.Gateway{{Host: "play.slashdiablo.net", Timezone: -6, Name: "Slash Diablo", Realm: "Slash Diablo"}},
		Launch:   spec.Launch{Exe: "Game.exe", MaxInstances: 2},
	}

	l := launch.NewWithPaths(h.Registry(), h.Start, Paths())
	start := time.Now()
	if err := l.Launch(ctx, base, server, p, launch.Choices{Instances: 2}, time.Second); err != nil {
		t.Fatal(err)
	}
	t.Logf("launched two boxes in %s", time.Since(start).Round(time.Millisecond))

	// Read back through a fresh registry, as the next launch would.
	reg := h.Registry()
	gw, err := reg.GatewayList()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("gateways: %q", gw)
	if len(gw) < 5 || gw[len(gw)-3] != "play.slashdiablo.net" {
		t.Errorf("gateways = %q", gw)
	}
	save, _ := reg.String("Save Path")
	if want := WinPath(filepath.Join(server, "Save")) + `\`; save != want {
		t.Errorf("Save Path = %q, want %q", save, want)
	}
	if ip, _ := reg.String("BNETIP"); ip != "play.slashdiablo.net" {
		t.Errorf("BNETIP = %q", ip)
	}
	t.Logf("Save Path: %s", save)
}
