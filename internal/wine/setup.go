package wine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Step is a part of setting Wine up, as the setup screen lists them.
type Step string

const (
	// StepWine finds or downloads the runtime.
	StepWine Step = "wine"
	// StepPrefix creates or updates the prefix.
	StepPrefix Step = "prefix"
)

// Progress is Wine setup's progress. Done and Total count download bytes in
// StepWine; both are 0 otherwise.
type Progress struct {
	Step  Step  `json:"step"`
	Done  int64 `json:"done"`
	Total int64 `json:"total"`
}

// ErrNeedsRosetta means Rosetta 2 must be installed first; see InstallRosetta.
var ErrNeedsRosetta = errors.New("Wine needs Rosetta 2, which isn't installed")

// Setup gets a runtime and a ready prefix.
type Setup struct {
	Manager   Manager
	PrefixDir string
	Debug     string
	Log       io.Writer
	// UseSystem tries the player's own Wine first (Linux).
	UseSystem bool
}

// Run sets Wine up, or checks it still is: quick once done. progress may be
// nil.
func (s Setup) Run(ctx context.Context, progress func(Progress)) (*Prefix, error) {
	report := func(p Progress) {
		if progress != nil {
			progress(p)
		}
	}

	if NeedsRosetta(ctx) {
		return nil, ErrNeedsRosetta
	}

	report(Progress{Step: StepWine})
	if s.UseSystem {
		rt, err := SystemWine(ctx)
		switch {
		case err == nil && s.rejected(rt.Version):
			// Known not to run 32-bit programs; not tried again.
		case err == nil:
			p, err := s.ready(ctx, rt, s.SystemPrefixDir(), report)
			if err == nil {
				return p, nil
			}
			slog.Info("not using the system's Wine", "version", rt.Version, "err", err)
			if errors.Is(err, ErrNo32Bit) {
				s.reject(rt.Version)
			}
		case !errors.Is(err, ErrNoRuntime):
			slog.Info("not using the system's Wine", "err", err)
		}
	}

	if s.Manager.Pin.URL == "" {
		if hint := InstallHint(); hint != "" {
			return nil, fmt.Errorf("%w. Install Wine: %s", ErrNoRuntime, hint)
		}
		return nil, ErrNoRuntime
	}
	total := s.Manager.Pin.Size
	rt, err := s.Manager.Install(ctx, func(done int64) {
		report(Progress{Step: StepWine, Done: done, Total: total})
	})
	if err != nil {
		var missing *MissingLibraryError
		if errors.As(err, &missing) {
			if hint := InstallHint(); hint != "" {
				return nil, fmt.Errorf("%w. Installing your system's Wine provides it: %s", err, hint)
			}
		}
		return nil, err
	}

	return s.ready(ctx, rt, s.PrefixDir, report)
}

// SystemPrefixDir is the prefix used with the system's Wine. It's kept
// apart from the one the launcher's own Wine uses, so an older system Wine
// never opens a prefix a newer Wine made (Wine can't take one back a
// version), and switching between them doesn't update one prefix back and
// forth.
func (s Setup) SystemPrefixDir() string {
	return s.PrefixDir + "-system"
}

// rejectedFile records a system Wine version that can't run 32-bit
// programs, so later runs go straight to the launcher's own.
func (s Setup) rejectedFile() string {
	return s.PrefixDir + "-system-rejected"
}

func (s Setup) rejected(version string) bool {
	data, err := os.ReadFile(s.rejectedFile())
	return err == nil && strings.TrimSpace(string(data)) == version
}

func (s Setup) reject(version string) {
	if err := os.WriteFile(s.rejectedFile(), []byte(version+"\n"), 0o644); err != nil {
		slog.Warn("remember the system's Wine", "err", err)
	}
}

// ready makes the prefix in dir ready for rt. The 32-bit check runs when
// the prefix was made or updated: it takes seconds, and is the same answer
// until the runtime changes.
func (s Setup) ready(ctx context.Context, rt *Runtime, dir string, report func(Progress)) (*Prefix, error) {
	p := &Prefix{Dir: dir, Runtime: rt, Debug: s.Debug, Log: s.Log}
	report(Progress{Step: StepPrefix})

	changed, err := p.Ensure(ctx)
	if err != nil {
		return nil, err
	}
	if changed {
		if err := p.Check32(ctx); err != nil {
			p.Forget()
			return nil, err
		}
	}
	return p, nil
}
