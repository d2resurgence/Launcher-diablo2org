//go:build darwin || linux

package main

import (
	"log/slog"
	"path/filepath"
	"runtime"

	"github.com/diablo2org/launcher/internal/fetch"
	"github.com/diablo2org/launcher/internal/logs"
	"github.com/diablo2org/launcher/internal/wine"
)

// newWineHost runs the game under Wine: the launcher's own runtime and
// prefix in the app data folder, or on Linux the system's Wine when it will
// do. Its output goes to logs/wine.log.
func newWineHost(dataDir, root string) *wine.Host {
	if root == "" {
		root = dataDir
	}

	setup := wine.Setup{
		Manager: wine.Manager{
			Dir:      filepath.Join(root, "runtime", "wine"),
			CacheDir: filepath.Join(root, "cache", "wine"),
			Pin:      wine.DefaultPin,
			Client:   fetch.New(wine.Hosts),
		},
		PrefixDir: filepath.Join(root, "prefix"),
		UseSystem: runtime.GOOS == "linux",
	}
	if w, err := logs.OpenWine(dataDir); err == nil {
		setup.Log = w
	} else {
		slog.Warn("wine log", "err", err)
	}

	return &wine.Host{Setup: setup, TempDir: filepath.Join(root, "tmp")}
}
