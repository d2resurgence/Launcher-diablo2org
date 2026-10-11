package wine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
		if rt, err := SystemWine(ctx); err == nil {
			p, err := s.ready(ctx, rt, report)
			if err == nil {
				return p, nil
			}
			slog.Info("not using the system's Wine", "version", rt.Version, "err", err)
		} else if !errors.Is(err, ErrNoRuntime) {
			slog.Info("not using the system's Wine", "err", err)
		}
	}

	if s.Manager.Pin.URL == "" {
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

	return s.ready(ctx, rt, report)
}

// ready makes the prefix ready for rt. The 32-bit check runs when the
// prefix was made or updated: it takes seconds, and is the same answer
// until the runtime changes.
func (s Setup) ready(ctx context.Context, rt *Runtime, report func(Progress)) (*Prefix, error) {
	p := &Prefix{Dir: s.PrefixDir, Runtime: rt, Debug: s.Debug, Log: s.Log}
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
