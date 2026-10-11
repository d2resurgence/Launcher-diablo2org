package wine

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/diablo2org/launcher/internal/launch"
)

// ErrNotReady means Wine hasn't been set up yet, or needs setting up again,
// such as after an update: the setup screen does that.
var ErrNotReady = errors.New("Wine needs setting up before the game can start")

// Host runs the game under Wine: it holds the prefix once Wine is set up,
// and gives the launcher a registry, a starter and path mapping for it.
type Host struct {
	Setup Setup
	// TempDir holds files passed to Wine programs, such as .reg files.
	TempDir string

	mu     sync.Mutex
	prefix *Prefix
}

// Prefix is the ready prefix, or ErrNotReady.
func (h *Host) Prefix() (*Prefix, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.prefix == nil {
		return nil, ErrNotReady
	}
	return h.prefix, nil
}

// Ready reports whether the game can start now, checking for a prefix an
// earlier run set up. It doesn't download or create anything.
func (h *Host) Ready(ctx context.Context) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.prefix != nil {
		return true
	}
	p, err := h.Setup.Quick(ctx)
	if err != nil {
		return false
	}
	h.prefix = p
	return true
}

// Run sets Wine up, as Setup.Run, and keeps the prefix.
func (h *Host) Run(ctx context.Context, progress func(Progress)) error {
	p, err := h.Setup.Run(ctx, progress)
	if err != nil {
		return err
	}

	h.mu.Lock()
	h.prefix = p
	h.mu.Unlock()
	return nil
}

// Registry is the game's registry in the prefix.
func (h *Host) Registry() *Registry {
	return &Registry{Prefix: h.Prefix, TempDir: h.TempDir}
}

// Paths maps host paths to drive Z:, for registry values.
func Paths() launch.Paths {
	return launch.Paths{
		ToGame: WinPath,
		FromGame: func(p string) (string, bool) {
			host, err := UnixPath(p)
			return host, err == nil
		},
		Sep: `\`,
	}
}

// Start starts a box under Wine, from the box's folder, and lets it run.
//
// With d2gl on in DirectDraw mode, Wine's own ddraw must give way to the one
// d2gl ships; in Glide mode (-3dfx), the game loads d2gl's glide3x.dll
// itself. With d2gl off, the game runs windowed: full screen under Wine
// changes the display mode, which goes badly on macOS.
func (h *Host) Start(b launch.Box) error {
	p, err := h.Prefix()
	if err != nil {
		return err
	}

	args, overrides := startArgs(b)
	cmd := exec.Command(p.Runtime.Wine, append([]string{b.Exe}, args...)...)
	cmd.Dir = b.Dir
	cmd.Env = p.Env(overrides...)
	cmd.Stdout, cmd.Stderr = p.log(), p.log()
	cmd.WaitDelay = waitDelay
	detach(cmd)

	if err := cmd.Start(); err != nil {
		return err
	}
	// Reaped in the background, so it doesn't linger as a zombie until the
	// launcher exits.
	go func() {
		err := cmd.Wait()
		if errors.Is(err, exec.ErrWaitDelay) {
			err = nil
		}
		slog.Info("box exited", "exe", filepath.Base(b.Exe), "err", err)
	}()
	return nil
}

// startArgs is a box's flags and extra DLL overrides under Wine.
func startArgs(b launch.Box) (args, overrides []string) {
	args = slices.Clone(b.Args)
	switch {
	case b.D2GL && !hasFlag(args, "-3dfx"):
		overrides = append(overrides, "ddraw=n,b")
	case !b.D2GL && !hasFlag(args, "-w"):
		args = append(args, "-w")
	}
	return args, overrides
}

func hasFlag(args []string, flag string) bool {
	return slices.ContainsFunc(args, func(a string) bool { return strings.EqualFold(a, flag) })
}

// Quick is the prefix an earlier Run left ready for the current runtime, or
// ErrNotReady. It doesn't download or create anything.
func (s Setup) Quick(ctx context.Context) (*Prefix, error) {
	if NeedsRosetta(ctx) {
		return nil, ErrNeedsRosetta
	}

	if s.UseSystem {
		if rt, err := SystemWine(ctx); err == nil && !s.rejected(rt.Version) {
			if p := s.readyIn(rt, s.SystemPrefixDir()); p != nil {
				return p, nil
			}
		}
	}
	if rt, err := s.Manager.Installed(ctx); err == nil {
		if p := s.readyIn(rt, s.PrefixDir); p != nil {
			return p, nil
		}
	}
	return nil, ErrNotReady
}

// readyIn is the prefix in dir if rt last set it up, or nil.
func (s Setup) readyIn(rt *Runtime, dir string) *Prefix {
	data, err := os.ReadFile(filepath.Join(dir, marker))
	if err != nil || strings.TrimSpace(string(data)) != rt.Version {
		return nil
	}
	if _, err := os.Stat(filepath.Join(dir, "system.reg")); err != nil {
		return nil
	}
	return &Prefix{Dir: dir, Runtime: rt, Debug: s.Debug, Log: s.Log}
}
