// Package basegame installs the Diablo II base game, classic and Lord of
// Destruction, from Blizzard's own CDN, by running the blizzard-legacy-dl
// that ships beside the launcher. The launcher never hosts or mirrors
// Blizzard's files: the payloads come from Blizzard, and older versions are
// reached with the patches legacy-dl fetches from its own patch source.
package basegame

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/diablo2org/launcher/internal/cdkey"
	"github.com/diablo2org/launcher/internal/install"
)

// DefaultVersion is the version installed unless the player picks another.
const DefaultVersion = "1.13c"

// Versions are the versions the launcher offers, newest first. Each is a
// Lord of Destruction patch legacy-dl's patch source carries.
var Versions = []string{"1.14d", "1.13d", "1.13c", "1.12a", "1.11b", "1.10", "1.09d"}

// Name is legacy-dl's file name on this platform.
var Name = "blizzard-legacy-dl" + exeSuffix()

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// Find looks for legacy-dl where the packages put it: beside the launcher
// on Windows and Linux, in Contents/Resources/bin in the macOS app.
func Find() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)

	for _, p := range []string{
		filepath.Join(dir, Name),
		filepath.Join(dir, "..", "Resources", "bin", Name),
	} {
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			return p, nil
		}
	}

	return "", fmt.Errorf("%s isn't installed beside the launcher; reinstalling the launcher should restore it", Name)
}

// Options is what to install, and where.
type Options struct {
	// Version is the game version, one of Versions.
	Version string
	// BaseDir is the folder to install into. It must be empty or new.
	BaseDir string
	// ClassicKey and ExpansionKey are the player's own CD keys, written into
	// the game's archives as Blizzard's installer would. They're required
	// but not checked: nobody can check them any more.
	ClassicKey   string
	ExpansionKey string
	// Owner is the name stored beside the keys. Optional.
	Owner string
}

// Normalise tidies the keys and owner the way the player's typing needs.
func (o Options) Normalise() Options {
	o.ClassicKey = cdkey.Normalise(o.ClassicKey)
	o.ExpansionKey = cdkey.Normalise(o.ExpansionKey)
	o.Owner = strings.TrimSpace(o.Owner)
	if o.Version == "" {
		o.Version = DefaultVersion
	}
	return o
}

// Check reports what's wrong with normalised options, if anything.
func (o Options) Check() error {
	if !slices.Contains(Versions, o.Version) {
		return fmt.Errorf("version %q isn't one the launcher can install", o.Version)
	}
	if !filepath.IsAbs(o.BaseDir) {
		return errors.New("the Diablo II folder must be a full path")
	}
	if o.ClassicKey == "" {
		return errors.New("enter your Diablo II CD key")
	}
	if o.ExpansionKey == "" {
		return errors.New("enter your Lord of Destruction CD key")
	}
	return nil
}

// args is legacy-dl's command line. The game and cache folders are given
// relative to the folder it runs in; see workDir.
//
// The keys go on the command line, so other processes of this user can see
// them in the process list while the install runs. legacy-dl has no other
// way to take them; they're never written anywhere else by the launcher.
func args(o Options, game string) []string {
	a := []string{
		"install", "D2XP", o.Version,
		"--game", game,
		"--jobs", "8",
		// Servers ship Windows binaries, so always the Windows payload,
		// whatever this machine runs.
		"--os", "WIN",
		"--platform", "win32",
		"--cdkey", o.ClassicKey,
		"--cdkey-expansion", o.ExpansionKey,
	}
	if o.Owner != "" {
		a = append(a, "--owner", o.Owner)
	}
	return a
}

// Installer runs legacy-dl.
type Installer struct {
	// Exe is legacy-dl; see Find.
	Exe string
	// CacheDir holds the downloaded payloads, so a cancelled or failed
	// install resumes. It's only needed until the install is done.
	CacheDir string
	// Log receives legacy-dl's output, keys redacted. Nil is slog.Default.
	Log *slog.Logger
}

// Errors an install can end with, for the frontend to explain.
var (
	// ErrHasGame means the folder already holds Diablo II: it should be used
	// as an existing install instead.
	ErrHasGame = errors.New("this folder already holds Diablo II: choose \"I already have Diablo II installed\" to use it")
	// ErrNotEmpty means the folder holds other files.
	ErrNotEmpty = errors.New("choose an empty or new folder to install Diablo II into")
	// ErrDownload means Blizzard's CDN couldn't be reached or refused.
	ErrDownload = errors.New("couldn't download Diablo II from Blizzard. Check your connection and try again: the download carries on where it stopped")
	// ErrPatchSource means the patch for an older version couldn't be had.
	ErrPatchSource = errors.New("couldn't download the patch for this version: the patch server may be down. Try again later")
)

// marker is left in the base folder while an install is under way, so an
// unfinished install can be resumed into a folder that's no longer empty.
const marker = ".launcher-installing"

// Result is what a finished install has to say.
type Result struct {
	// Warnings are legacy-dl's notes on the keys, such as one that doesn't
	// decode. They don't stop the install.
	Warnings []string
}

// Install installs the game. It can take a long time; cancelling ctx stops
// it, and installing again resumes the download. progress may be nil.
func (in Installer) Install(ctx context.Context, o Options, progress func(Progress)) (Result, error) {
	o = o.Normalise()
	if err := o.Check(); err != nil {
		return Result{}, err
	}
	if err := checkTarget(o.BaseDir); err != nil {
		return Result{}, err
	}
	if in.CacheDir == "" {
		return Result{}, errors.New("no cache folder for the download")
	}

	if err := os.MkdirAll(o.BaseDir, 0o755); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(filepath.Join(o.BaseDir, marker), nil, 0o644); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(in.CacheDir, 0o755); err != nil {
		return Result{}, err
	}

	dir, game, cache := workDir(o.BaseDir, in.CacheDir)
	cmd := exec.Command(in.Exe, args(o, game)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "BLIZZARD_LEGACY_DL_CACHE="+cache)

	log := in.Log
	if log == nil {
		log = slog.Default()
	}
	out := newOutput(progress, func(line string) {
		log.Info("legacy-dl", "line", line)
	}, o.ClassicKey, o.ExpansionKey)
	cmd.Stdout = out
	cmd.Stderr = out

	log.Info("installing the base game", "version", o.Version, "dir", o.BaseDir)
	runErr := run(ctx, cmd)
	out.Flush()
	res := Result{Warnings: out.warnings}

	if ctx.Err() != nil {
		return res, ctx.Err()
	}
	if runErr != nil {
		return res, out.err(runErr)
	}

	report, err := install.Check(o.BaseDir, true)
	if err != nil {
		return res, err
	}
	if !report.OK() {
		return res, fmt.Errorf("the install finished without %s", strings.Join(report.Missing, ", "))
	}

	os.Remove(filepath.Join(o.BaseDir, marker))
	return res, nil
}

// run runs cmd, killing it and anything it started if ctx is cancelled.
func run(ctx context.Context, cmd *exec.Cmd) error {
	g, err := startGroup(cmd)
	if err != nil {
		return err
	}
	defer g.close()

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			g.kill()
		case <-done:
		}
	}()

	err = cmd.Wait()
	close(done)
	return err
}

// checkTarget allows a new folder, an empty one, or one an unfinished install
// left. A folder that holds the game already is an existing install.
func checkTarget(dir string) error {
	info, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is a file, not a folder", dir)
	}

	if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	for _, e := range entries {
		for _, a := range install.Archives {
			if strings.EqualFold(e.Name(), a.Name) {
				return ErrHasGame
			}
		}
	}
	return ErrNotEmpty
}

// workDir picks the folder legacy-dl runs in and the game and cache folders
// relative to it: the nearest folder holding both. legacy-dl makes folders
// by walking down from the root, which suits relative paths everywhere; an
// absolute Windows path, with its drive letter, is the case it's least
// likely to get right. Folders on different drives stay absolute.
func workDir(game, cache string) (dir, gameRel, cacheRel string) {
	game, cache = filepath.Clean(game), filepath.Clean(cache)

	for dir = filepath.Dir(game); ; dir = filepath.Dir(dir) {
		g, gErr := filepath.Rel(dir, game)
		c, cErr := filepath.Rel(dir, cache)
		if gErr == nil && cErr == nil && filepath.IsLocal(g) && filepath.IsLocal(c) {
			return dir, g, c
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return cache, game, cache
		}
	}
}

// CacheSize is how much disk space the download cache uses.
func (in Installer) CacheSize() int64 {
	var total int64
	filepath.WalkDir(in.CacheDir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// ClearCache deletes the download cache. A later install downloads again.
func (in Installer) ClearCache() error {
	if in.CacheDir == "" || filepath.Dir(in.CacheDir) == in.CacheDir {
		return errors.New("no cache folder")
	}
	return os.RemoveAll(in.CacheDir)
}
