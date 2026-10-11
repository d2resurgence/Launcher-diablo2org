//go:build !darwin && !linux

package wine

import "context"

// DefaultPin is empty: there is no runtime to download on this platform.
var DefaultPin Pin

// NeedsRosetta is false off macOS.
func NeedsRosetta(ctx context.Context) bool { return false }

// InstallRosetta has nothing to do off macOS.
func InstallRosetta(ctx context.Context) error { return nil }

// SystemWine isn't used on this platform.
func SystemWine(ctx context.Context) (*Runtime, error) { return nil, ErrNoRuntime }

// InstallHint has nothing to suggest on this platform.
func InstallHint() string { return "" }
