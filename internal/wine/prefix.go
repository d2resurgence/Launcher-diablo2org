package wine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Timeouts for the prefix's setup steps. wineboot -i on a first run, with
// Rosetta translating Wine for the first time, can take a couple of minutes.
var (
	bootTimeout  = 3 * time.Minute
	checkTimeout = time.Minute
)

// marker records which runtime last set the prefix up.
const marker = ".launcher-wine"

// Prefix is the launcher's private Wine prefix: its own Windows registry and
// drive C:, separate from any the player uses.
type Prefix struct {
	Dir     string
	Runtime *Runtime
	// Debug is WINEDEBUG; "" means DefaultDebug.
	Debug string
	// Log receives the output of every Wine call. Nil discards it.
	Log io.Writer
}

// Env is the environment for Wine calls in this prefix, with extra DLL
// overrides.
func (p *Prefix) Env(overrides ...string) []string {
	return Env(os.Environ(), p.Dir, p.Debug, overrides...)
}

// waitDelay is how long Wait keeps reading a finished Wine call's output.
// wine may start the wineserver, which inherits the output pipe and, when
// persistent, outlives the call: without a limit, Wait would wait for the
// server to exit.
const waitDelay = 2 * time.Second

// Command is wine with args in this prefix, logging its output.
func (p *Prefix) Command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, p.Runtime.Wine, args...)
	cmd.Env = p.Env()
	cmd.Stdout = p.log()
	cmd.Stderr = p.log()
	cmd.WaitDelay = waitDelay
	return cmd
}

func (p *Prefix) log() io.Writer {
	if p.Log == nil {
		return io.Discard
	}
	return p.Log
}

// output runs wine with args and returns what it printed, logging it too.
func (p *Prefix) output(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var buf bytes.Buffer
	cmd := p.Command(ctx, args...)
	cmd.Stdout = io.MultiWriter(&buf, p.log())
	cmd.Stderr = cmd.Stdout
	err := run(cmd)
	if ctx.Err() == context.DeadlineExceeded {
		return buf.String(), fmt.Errorf("wine %s took longer than %s: %w", strings.Join(args, " "), timeout, context.DeadlineExceeded)
	}
	return buf.String(), err
}

// run runs cmd. A call that succeeded but left a wineserver holding its
// output open, so that WaitDelay cut the reading short, succeeded.
func run(cmd *exec.Cmd) error {
	err := cmd.Run()
	if errors.Is(err, exec.ErrWaitDelay) {
		return nil
	}
	return err
}

// Ensure makes the prefix ready: created on first use, updated when the
// runtime has changed since, and with drive Z: mapped to /. It reports
// whether it created or updated it.
func (p *Prefix) Ensure(ctx context.Context) (changed bool, err error) {
	want := p.Runtime.Version
	have, markErr := os.ReadFile(filepath.Join(p.Dir, marker))
	_, regErr := os.Stat(filepath.Join(p.Dir, "system.reg"))

	switch {
	case markErr != nil || regErr != nil:
		if err := os.MkdirAll(p.Dir, 0o755); err != nil {
			return false, err
		}
		if err := p.boot(ctx, "-i"); err != nil {
			return false, fmt.Errorf("setting up Wine: %w", err)
		}
		changed = true
	case strings.TrimSpace(string(have)) != want:
		if err := p.boot(ctx, "-u"); err != nil {
			return false, fmt.Errorf("updating Wine's settings: %w", err)
		}
		changed = true
	}

	if err := p.ensureZ(); err != nil {
		return changed, err
	}
	return changed, os.WriteFile(filepath.Join(p.Dir, marker), []byte(want+"\n"), 0o644)
}

// Forget makes the next Ensure check the prefix over, such as after a
// runtime failed in it.
func (p *Prefix) Forget() {
	os.Remove(filepath.Join(p.Dir, marker))
}

// boot runs wineboot, then waits for the wineserver to finish writing the
// registry.
func (p *Prefix) boot(ctx context.Context, flag string) error {
	if _, err := p.output(ctx, bootTimeout, "wineboot", flag); err != nil {
		return err
	}
	return p.wait(ctx)
}

// wait waits for every program in the prefix to end and the wineserver to
// exit, which is when it has saved the registry.
func (p *Prefix) wait(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, bootTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, p.Runtime.Wineserver, "-w")
	cmd.Env = p.Env()
	cmd.Stdout, cmd.Stderr = p.log(), p.log()
	cmd.WaitDelay = waitDelay
	return run(cmd)
}

// ensureZ checks drive Z: is the host's root, as a fresh prefix has it, and
// puts it back if a player's tinkering removed it. Every path the launcher
// hands to Windows programs is on Z:.
func (p *Prefix) ensureZ() error {
	z := filepath.Join(p.Dir, "dosdevices", "z:")
	if target, err := os.Readlink(z); err == nil && target == "/" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(z), 0o755); err != nil {
		return err
	}
	if err := os.Remove(z); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Symlink("/", z)
}

// WindowsVersion runs cmd /c ver: proof the prefix runs programs.
func (p *Prefix) WindowsVersion(ctx context.Context) (string, error) {
	return p.ver(ctx, "cmd")
}

// Check32 runs the 32-bit cmd.exe: the game is 32-bit, and some Wine builds,
// such as a distribution's 64-bit-only package, can't run it. A check cut
// short, by a timeout or the player cancelling, says nothing either way and
// isn't ErrNo32Bit.
func (p *Prefix) Check32(ctx context.Context) error {
	_, err := p.ver(ctx, `C:\windows\syswow64\cmd.exe`)
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
		return err
	}
	return fmt.Errorf("%w: %w", ErrNo32Bit, err)
}

// ErrNo32Bit means a Wine can't run 32-bit programs, as the game is.
var ErrNo32Bit = errors.New("this Wine can't run 32-bit programs")

func (p *Prefix) ver(ctx context.Context, cmd string) (string, error) {
	out, err := p.output(ctx, checkTimeout, cmd, "/c", "ver")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); strings.Contains(line, "Windows") {
			return line, nil
		}
	}
	return "", fmt.Errorf("cmd /c ver printed no version: %q", strings.TrimSpace(out))
}
