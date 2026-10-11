package core

import (
	"context"
	"errors"

	"github.com/diablo2org/launcher/internal/wine"
)

// SetWine has the game run under Wine, set up by h: on macOS and Linux.
// d2gl is then on by default, where the player hasn't chosen.
func (m *Manager) SetWine(h *wine.Host) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.wine = h
	m.preferD2GL = h != nil
}

// WineStatus is whether the game needs Wine here, and whether it's ready.
type WineStatus struct {
	Needed bool `json:"needed"`
	Ready  bool `json:"ready"`
	// NeedsRosetta asks the player to install Rosetta 2 first (Apple
	// Silicon).
	NeedsRosetta bool `json:"needsRosetta"`
}

// WineStatus checks Wine without setting anything up.
func (m *Manager) WineStatus(ctx context.Context) WineStatus {
	h := m.wineHost()
	if h == nil {
		return WineStatus{}
	}
	if wine.NeedsRosetta(ctx) {
		return WineStatus{Needed: true, NeedsRosetta: true}
	}
	return WineStatus{Needed: true, Ready: h.Ready(ctx)}
}

// SetupWine downloads Wine and makes the prefix, as needed. Quick when
// it's already done.
func (m *Manager) SetupWine(ctx context.Context, progress func(wine.Progress)) error {
	h := m.wineHost()
	if h == nil {
		return nil
	}
	return h.Run(ctx, progress)
}

// InstallRosetta installs Rosetta 2, which macOS asks an administrator to
// agree to.
func (m *Manager) InstallRosetta(ctx context.Context) error {
	return wine.InstallRosetta(ctx)
}

func (m *Manager) wineHost() *wine.Host {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.wine
}

// ErrWineNotReady means Play needs Wine set up first, which the setup
// screen does.
var ErrWineNotReady = errors.New("Wine needs setting up first")
