package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"strings"

	"github.com/diablo2org/launcher/internal/basegame"
)

// BaseInstaller is how the launcher installs the base game itself.
type BaseInstaller struct {
	basegame.Installer
	// DefaultDir is where it suggests installing.
	DefaultDir string
	// Unavailable says why it can't, such as legacy-dl missing.
	Unavailable string
}

// SetBaseInstaller lets the manager install the base game.
func (m *Manager) SetBaseInstaller(b BaseInstaller) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.baseInstaller = b
}

// BaseSetup is what the "Get Diablo II" step offers.
type BaseSetup struct {
	// Available is whether the launcher can install the game; when it
	// can't, Unavailable says why.
	Available   bool   `json:"available"`
	Unavailable string `json:"unavailable"`
	// DefaultDir is the suggested install folder.
	DefaultDir     string   `json:"defaultDir"`
	Versions       []string `json:"versions"`
	DefaultVersion string   `json:"defaultVersion"`
	// Owner is the name suggested to store beside the keys.
	Owner string `json:"owner"`
	// CacheBytes is the download cache left by an install.
	CacheBytes int64 `json:"cacheBytes"`
}

// BaseSetup describes installing the base game.
func (m *Manager) BaseSetup() BaseSetup {
	m.mu.Lock()
	b := m.baseInstaller
	m.mu.Unlock()

	s := BaseSetup{
		Available:      b.reason() == "",
		Unavailable:    b.reason(),
		DefaultDir:     b.DefaultDir,
		Versions:       basegame.Versions,
		DefaultVersion: basegame.DefaultVersion,
		Owner:          ownerName(),
	}
	if b.CacheDir != "" {
		s.CacheBytes = b.CacheSize()
	}

	return s
}

// ownerName is the user's name on this computer, for the owner field.
func ownerName() string {
	name := ""
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	if name == "" {
		name = os.Getenv("USERNAME")
	}
	if name == "" {
		name = os.Getenv("USER")
	}
	// Windows gives DOMAIN\name.
	if i := strings.LastIndex(name, `\`); i >= 0 {
		name = name[i+1:]
	}

	return name
}

// ErrInstalling means an install is already under way.
var ErrInstalling = errors.New("Diablo II is already being installed")

// InstallBase installs the base game and, once it passes its check, makes
// it the Diablo II folder. Cancelling ctx stops it; installing again
// resumes.
func (m *Manager) InstallBase(ctx context.Context, o basegame.Options, progress func(basegame.Progress)) (basegame.Result, error) {
	m.mu.Lock()
	b := m.baseInstaller
	if m.installingBase {
		m.mu.Unlock()
		return basegame.Result{}, ErrInstalling
	}
	if why := b.reason(); why != "" {
		m.mu.Unlock()
		return basegame.Result{}, errors.New(why)
	}
	m.installingBase = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.installingBase = false
		m.mu.Unlock()
	}()

	res, err := b.Install(ctx, o, progress)
	if err != nil {
		return res, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Kept even if saving fails: the game is installed, and the next save,
	// such as finishing the welcome screen, records it.
	m.state.BasePath = o.BaseDir
	if err := m.save(); err != nil {
		return res, fmt.Errorf("%w: %w", ErrBaseNotSaved, err)
	}
	return res, nil
}

// ErrBaseNotSaved means the game was installed but the launcher couldn't
// save that it's now the Diablo II folder. Installing again won't help: the
// folder is no longer empty.
var ErrBaseNotSaved = errors.New("Diablo II is installed, but the launcher couldn't save where")

// reason says why the base game can't be installed, or "" if it can.
func (b BaseInstaller) reason() string {
	switch {
	case b.Unavailable != "":
		return b.Unavailable
	case b.Exe == "":
		return "this build of the launcher can't install Diablo II"
	}
	return ""
}

// ClearBaseCache deletes the base game's download cache.
func (m *Manager) ClearBaseCache() error {
	m.mu.Lock()
	b := m.baseInstaller
	busy := m.installingBase
	m.mu.Unlock()

	if busy {
		return ErrInstalling
	}
	return b.ClearCache()
}
