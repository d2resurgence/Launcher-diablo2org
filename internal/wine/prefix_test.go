package wine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeRuntime is a wine that records its arguments and, for wineboot,
// makes system.reg as the real one does.
func fakeRuntime(t *testing.T, version string) (*Runtime, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in wine is a shell script")
	}
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	wine := filepath.Join(dir, "wine")
	os.WriteFile(wine, []byte(`#!/bin/sh
echo "$*" >> "`+calls+`"
[ "$1" = wineboot ] && touch "$WINEPREFIX/system.reg"
case "$*" in *"/c ver"*) echo "Microsoft Windows 10.0.19045";; esac
exit 0
`), 0o755)
	server := filepath.Join(dir, "wineserver")
	os.WriteFile(server, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	return &Runtime{Version: version, Wine: wine, Wineserver: server}, calls
}

func readCalls(path string) []string {
	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

func TestPrefixEnsure(t *testing.T) {
	rt, calls := fakeRuntime(t, "11.0_1")
	p := &Prefix{Dir: filepath.Join(t.TempDir(), "prefix"), Runtime: rt}
	ctx := context.Background()

	changed, err := p.Ensure(ctx)
	if err != nil || !changed {
		t.Fatalf("first Ensure: %v, %v", changed, err)
	}
	if got := readCalls(calls); len(got) != 1 || got[0] != "wineboot -i" {
		t.Errorf("first run calls %q", got)
	}
	if target, err := os.Readlink(filepath.Join(p.Dir, "dosdevices", "z:")); err != nil || target != "/" {
		t.Errorf("z: -> %q, %v", target, err)
	}

	// Ready: nothing runs.
	os.Remove(calls)
	if changed, err := p.Ensure(ctx); err != nil || changed || readCalls(calls) != nil {
		t.Errorf("second Ensure: %v, %v, calls %q", changed, err, readCalls(calls))
	}

	// A new runtime updates the prefix.
	rt.Version = "11.0_2"
	if changed, err := p.Ensure(ctx); err != nil || !changed {
		t.Fatalf("after an upgrade: %v, %v", changed, err)
	}
	if got := readCalls(calls); len(got) != 1 || got[0] != "wineboot -u" {
		t.Errorf("upgrade calls %q", got)
	}

	// A missing Z: drive is put back.
	z := filepath.Join(p.Dir, "dosdevices", "z:")
	os.Remove(z)
	os.Symlink("/tmp", z)
	if _, err := p.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	if target, _ := os.Readlink(z); target != "/" {
		t.Errorf("z: -> %q after repair", target)
	}

	if err := p.Check32(ctx); err != nil {
		t.Errorf("Check32: %v", err)
	}
	if v, err := p.WindowsVersion(ctx); err != nil || v != "Microsoft Windows 10.0.19045" {
		t.Errorf("WindowsVersion = %q, %v", v, err)
	}
}

func TestSetupRechecksAfterFailure(t *testing.T) {
	rt, calls := fakeRuntime(t, "11.0_1")
	// A wine that can't run 32-bit programs.
	os.WriteFile(rt.Wine, []byte(`#!/bin/sh
echo "$*" >> "`+calls+`"
[ "$1" = wineboot ] && touch "$WINEPREFIX/system.reg"
case "$*" in *syswow64*) exit 1;; esac
exit 0
`), 0o755)

	s := Setup{PrefixDir: filepath.Join(t.TempDir(), "prefix")}
	_, err := s.ready(context.Background(), rt, s.PrefixDir, func(Progress) {})
	if !errors.Is(err, ErrNo32Bit) {
		t.Fatalf("a wine without 32-bit support: %v", err)
	}
	// The next try checks again rather than trusting the prefix.
	if _, err := s.ready(context.Background(), rt, s.PrefixDir, func(Progress) {}); err == nil {
		t.Fatal("accepted on the second try")
	}
}

func TestCheck32CutShort(t *testing.T) {
	rt, calls := fakeRuntime(t, "11.0_1")
	// A wine whose 32-bit check hangs.
	os.WriteFile(rt.Wine, []byte(`#!/bin/sh
echo "$*" >> "`+calls+`"
[ "$1" = wineboot ] && touch "$WINEPREFIX/system.reg"
case "$*" in *syswow64*) exec sleep 30;; esac
exit 0
`), 0o755)
	defer func(d time.Duration) { checkTimeout = d }(checkTimeout)
	checkTimeout = 200 * time.Millisecond

	rt.Version = "system wine-9.0"
	s := Setup{PrefixDir: filepath.Join(t.TempDir(), "prefix")}
	_, err := s.trySystem(context.Background(), rt, func(Progress) {})
	if err == nil || errors.Is(err, ErrNo32Bit) {
		t.Fatalf("a timed-out check: %v, want an error that isn't ErrNo32Bit", err)
	}
	if s.rejected(rt.Version) {
		t.Error("a timed-out check rejected the system's Wine")
	}

	// Nor when the player cancels.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &Prefix{Dir: s.SystemPrefixDir(), Runtime: rt}
	if err := p.Check32(ctx); err == nil || errors.Is(err, ErrNo32Bit) {
		t.Errorf("a cancelled check: %v", err)
	}

	// A Wine that runs the check and fails it is rejected.
	os.WriteFile(rt.Wine, []byte(`#!/bin/sh
[ "$1" = wineboot ] && touch "$WINEPREFIX/system.reg"
case "$*" in *syswow64*) exit 1;; esac
exit 0
`), 0o755)
	if _, err := s.trySystem(context.Background(), rt, func(Progress) {}); !errors.Is(err, ErrNo32Bit) {
		t.Fatalf("a failed check: %v", err)
	}
	if !s.rejected(rt.Version) {
		t.Error("a Wine without 32-bit support wasn't rejected")
	}
}

func TestSystemWineRejection(t *testing.T) {
	s := Setup{PrefixDir: filepath.Join(t.TempDir(), "prefix")}
	if s.SystemPrefixDir() == s.PrefixDir {
		t.Fatal("system Wine shares the launcher's prefix")
	}
	if s.rejected("system wine-9.0") {
		t.Fatal("rejected before any try")
	}
	s.reject("system wine-9.0")
	if !s.rejected("system wine-9.0") {
		t.Error("rejection not remembered")
	}
	// A different version, such as after the player updates Wine, is tried.
	if s.rejected("system wine-10.0") {
		t.Error("another version rejected")
	}
}
