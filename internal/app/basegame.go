package app

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/diablo2org/launcher/internal/basegame"
	"github.com/diablo2org/launcher/internal/core"
)

// BaseInstallRequest is the "Install Diablo II for me" form.
type BaseInstallRequest struct {
	Version      string `json:"version"`
	Dir          string `json:"dir"`
	ClassicKey   string `json:"classicKey"`
	ExpansionKey string `json:"expansionKey"`
	Owner        string `json:"owner"`
}

// BaseInstallResult is how an install ended.
type BaseInstallResult struct {
	OK        bool   `json:"ok"`
	Cancelled bool   `json:"cancelled"`
	Error     string `json:"error"`
	// Warnings are notes on the keys, such as one that doesn't decode.
	Warnings []string `json:"warnings"`
	// CacheBytes is the download cache the install left, which the player
	// can delete.
	CacheBytes int64 `json:"cacheBytes"`
}

// BaseSetup describes installing the base game.
func (s *ServerService) BaseSetup() core.BaseSetup {
	return s.m.BaseSetup()
}

// ChooseInstallFolder asks where to install Diablo II. A folder that isn't
// empty gets a Diablo II folder made inside it. "" means cancelled.
func (s *ServerService) ChooseInstallFolder() (string, error) {
	dir, err := s.host.PickFolder("Choose where to install Diablo II")
	if err != nil || dir == "" {
		return "", err
	}

	return installFolder(dir), nil
}

func installFolder(dir string) string {
	entries, err := os.ReadDir(dir)
	if err == nil && len(entries) > 0 {
		return filepath.Join(dir, "Diablo II")
	}

	return dir
}

// InstallBase installs the base game, sending base:progress events, and makes
// it the Diablo II folder. Cancelling the call stops it.
func (s *ServerService) InstallBase(ctx context.Context, req BaseInstallRequest) BaseInstallResult {
	// Never the keys.
	slog.Info("install base game", "version", req.Version, "dir", req.Dir)

	res, err := s.m.InstallBase(ctx, basegame.Options{
		Version:      req.Version,
		BaseDir:      req.Dir,
		ClassicKey:   req.ClassicKey,
		ExpansionKey: req.ExpansionKey,
		Owner:        req.Owner,
	}, func(p basegame.Progress) {
		s.host.Emit("base:progress", p)
	})

	out := BaseInstallResult{Warnings: res.Warnings, CacheBytes: s.m.BaseSetup().CacheBytes}
	if out.Warnings == nil {
		out.Warnings = []string{}
	}
	switch {
	case err == nil:
		slog.Info("base game installed", "dir", req.Dir)
		out.OK = true
	case errors.Is(err, context.Canceled):
		slog.Info("base game install cancelled")
		out.Cancelled = true
	default:
		logged("install base game", "", err)
		out.Error = err.Error()
	}

	return out
}

// ClearBaseCache deletes the base game's download cache.
func (s *ServerService) ClearBaseCache() error {
	return logged("clear base game cache", "", s.m.ClearBaseCache())
}
