package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/diablo2org/launcher/internal/core"
	"github.com/diablo2org/launcher/internal/wine"
)

// WineSetupResult is how setting Wine up ended.
type WineSetupResult struct {
	OK           bool   `json:"ok"`
	Cancelled    bool   `json:"cancelled"`
	NeedsRosetta bool   `json:"needsRosetta"`
	Error        string `json:"error"`
}

// WineStatus is whether the game needs Wine here, and whether it's ready.
func (s *ServerService) WineStatus(ctx context.Context) core.WineStatus {
	return s.m.WineStatus(ctx)
}

// SetupWine downloads Wine and makes the prefix as needed, sending
// wine:progress events. Cancelling the call stops it.
func (s *ServerService) SetupWine(ctx context.Context) WineSetupResult {
	slog.Info("set up wine")
	err := s.m.SetupWine(ctx, func(p wine.Progress) {
		s.host.Emit("wine:progress", p)
	})

	switch {
	case err == nil:
		slog.Info("wine ready")
		return WineSetupResult{OK: true}
	case errors.Is(err, context.Canceled):
		slog.Info("wine setup cancelled")
		return WineSetupResult{Cancelled: true}
	case errors.Is(err, wine.ErrNeedsRosetta):
		return WineSetupResult{NeedsRosetta: true, Error: err.Error()}
	default:
		logged("set up wine", "", err)
		return WineSetupResult{Error: err.Error()}
	}
}

// InstallRosetta installs Rosetta 2 through macOS's administrator prompt.
func (s *ServerService) InstallRosetta(ctx context.Context) error {
	slog.Info("install rosetta")
	return logged("install rosetta", "", s.m.InstallRosetta(ctx))
}
