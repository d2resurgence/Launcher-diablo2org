//go:build !darwin && !linux

package main

import "github.com/diablo2org/launcher/internal/wine"

// newWineHost is nil: the game runs natively.
func newWineHost(dataDir, root string) *wine.Host {
	return nil
}
