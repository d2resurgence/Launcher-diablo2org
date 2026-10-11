// Package wine runs the game's Windows binaries on macOS and Linux: it finds
// or downloads a Wine runtime, keeps a private prefix for the launcher, and
// maps paths between the host and Windows.
package wine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/diablo2org/launcher/internal/fetch"
)

// Pin is a Wine build the launcher downloads, fixed by size and SHA-256.
type Pin struct {
	Version string
	URL     string
	Size    int64
	SHA256  string
}

// Name is the archive's file name.
func (p Pin) Name() string {
	return p.URL[strings.LastIndex(p.URL, "/")+1:]
}

// MacPin is WineHQ's macOS build of Wine Stable, from Gcenx/macOS_Wine_builds:
// x86_64, running 32- and 64-bit Windows programs, on Apple Silicon through
// Rosetta 2.
var MacPin = Pin{
	Version: "11.0_1",
	URL:     "https://github.com/Gcenx/macOS_Wine_builds/releases/download/11.0_1/wine-stable-11.0_1-osx64.tar.xz",
	Size:    185303032,
	SHA256:  "b50dc50ec7f41d58b115a6b685d4d1315ba3c797bd3aa0f49213f2703cb82388",
}

// LinuxPin is Kron4ek/Wine-Builds' amd64-wow64 build, which runs 32-bit
// programs without 32-bit host libraries. It still needs the usual system
// libraries and glibc 2.27 or later.
var LinuxPin = Pin{
	Version: "11.0",
	URL:     "https://github.com/Kron4ek/Wine-Builds/releases/download/11.0/wine-11.0-amd64-wow64.tar.xz",
	Size:    73144724,
	SHA256:  "39574efa1132c3ca0d5c77dd2eddbe4a49cca0d6cc2c290ff4924493a1c40314",
}

// Hosts are where pinned runtimes download from.
var Hosts = append([]string{"github.com"}, fetch.GitHubAssetHosts...)

// Runtime is a Wine installation.
type Runtime struct {
	// Version is the pin's version, or "system" plus wine --version.
	Version    string
	Wine       string
	Wineserver string
	// System is Wine the player installed, not one the launcher manages.
	System bool
}

// MinSystemVersion is the oldest system Wine the launcher uses.
var MinSystemVersion = [2]int{9, 0}

// Manager keeps the launcher's Wine runtimes under Dir, one folder per
// version, and downloads into CacheDir.
type Manager struct {
	Dir      string
	CacheDir string
	Pin      Pin
	Client   *fetch.Client
}

// Installed returns the pinned runtime if it's already unpacked and runs.
func (m Manager) Installed(ctx context.Context) (*Runtime, error) {
	dir := filepath.Join(m.Dir, m.Pin.Version)
	rt, err := find(dir)
	if err != nil {
		return nil, err
	}
	rt.Version = m.Pin.Version
	if _, err := rt.WineVersion(ctx); err != nil {
		return nil, err
	}
	return rt, nil
}

// Install downloads and unpacks the pinned runtime, unless it's already
// there and runs. Older versions are removed only once the new one runs.
// A cancelled or failed download resumes next time.
func (m Manager) Install(ctx context.Context, progress fetch.Progress) (*Runtime, error) {
	if rt, err := m.Installed(ctx); err == nil {
		return rt, nil
	}

	if err := os.MkdirAll(m.CacheDir, 0o755); err != nil {
		return nil, err
	}
	archive := filepath.Join(m.CacheDir, m.Pin.Name())
	if err := m.Client.Resume(ctx, m.Pin.URL, archive, m.Pin.Size, m.Pin.SHA256, progress); err != nil {
		return nil, fmt.Errorf("downloading Wine: %w", err)
	}

	final := filepath.Join(m.Dir, m.Pin.Version)
	tmp := final + ".tmp"
	if err := os.RemoveAll(tmp); err != nil {
		return nil, err
	}
	f, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	err = Unpack(f, tmp)
	f.Close()
	if err != nil {
		os.RemoveAll(tmp)
		return nil, fmt.Errorf("unpacking Wine: %w", err)
	}

	rt, err := find(tmp)
	if err == nil {
		_, err = rt.WineVersion(ctx)
	}
	if err != nil {
		os.RemoveAll(tmp)
		return nil, fmt.Errorf("the downloaded Wine doesn't run: %w", err)
	}

	// A broken unpack of this version, if any, goes; then the new one moves
	// into place. Wine's own paths are relative, so moving it is safe.
	if err := os.RemoveAll(final); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, final); err != nil {
		return nil, err
	}
	os.Remove(archive)
	m.removeOthers()

	return m.Installed(ctx)
}

// removeOthers deletes runtimes of other versions.
func (m Manager) removeOthers() {
	entries, err := os.ReadDir(m.Dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && e.Name() != m.Pin.Version {
			os.RemoveAll(filepath.Join(m.Dir, e.Name()))
		}
	}
}

// find locates bin/wine and bin/wineserver in an unpacked runtime, without
// relying on how the archive names its top folder (such as the .app name).
func find(root string) (*Runtime, error) {
	var rt Runtime
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Base(filepath.Dir(p)) != "bin" {
			return nil
		}
		switch d.Name() {
		case "wine":
			if rt.Wine == "" {
				rt.Wine = p
			}
		case "wineserver":
			if rt.Wineserver == "" {
				rt.Wineserver = p
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if rt.Wine == "" || rt.Wineserver == "" {
		return nil, fmt.Errorf("no bin/wine and bin/wineserver in %s", root)
	}
	if filepath.Dir(rt.Wine) != filepath.Dir(rt.Wineserver) {
		return nil, fmt.Errorf("wine and wineserver are in different folders in %s", root)
	}
	return &rt, nil
}

// versionPattern finds the version in wine --version, such as "wine-11.0"
// or "wine-9.0 (Staging)".
var versionPattern = regexp.MustCompile(`wine-(\d+)\.(\d+)`)

// WineVersion runs wine --version.
func (rt *Runtime) WineVersion(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, rt.Wine, "--version").CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if missing := missingLibrary(text); missing != "" {
			return "", &MissingLibraryError{Library: missing}
		}
		if text != "" {
			return "", fmt.Errorf("%w: %s", err, firstLine(text))
		}
		return "", err
	}
	if !versionPattern.MatchString(text) {
		return "", fmt.Errorf("unexpected wine --version: %q", firstLine(text))
	}
	return text, nil
}

// atLeast reports whether a wine --version is at least min.
func atLeast(version string, min [2]int) bool {
	m := versionPattern.FindStringSubmatch(version)
	if m == nil {
		return false
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	return major > min[0] || major == min[0] && minor >= min[1]
}

// MissingLibraryError means a downloaded Wine can't find a system library.
type MissingLibraryError struct{ Library string }

func (e *MissingLibraryError) Error() string {
	return "Wine needs " + e.Library + ", which this system doesn't have"
}

var missingLibPattern = regexp.MustCompile(`error while loading shared libraries: ([^:\s]+)`)

func missingLibrary(output string) string {
	if m := missingLibPattern.FindStringSubmatch(output); m != nil {
		return m[1]
	}
	return ""
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// ErrNoRuntime means no Wine is available on this platform.
var ErrNoRuntime = errors.New("no Wine runtime for this system")
