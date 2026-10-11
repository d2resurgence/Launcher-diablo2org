package basegame

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/diablo2org/launcher/internal/install"
)

const (
	classic   = "BCDE2467FGHJ89KM"
	expansion = "PRTVWX246789BCDEFGHJKMNPRT"
)

// The test binary stands in for legacy-dl when fakeMode is set.
const fakeMode = "BASEGAME_FAKE_LEGACY_DL"

func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeMode); mode != "" {
		os.Exit(fake(mode, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fake behaves like legacy-dl v0.2.0 off a terminal.
func fake(mode string, argv []string) int {
	game := ""
	for i, a := range argv {
		if a == "--game" && i+1 < len(argv) {
			game = argv[i+1]
		}
	}
	say := func(format string, a ...any) { fmt.Fprintf(os.Stderr, format, a...) }

	// A log line that carries the keys, as a careless tool might.
	say("args: %s\n", strings.Join(argv, " "))

	switch mode {
	case "hang":
		say("\n=== D2DV ===\n")
		time.Sleep(time.Minute)
		return 1
	case "cdn":
		say("\n=== D2DV ===\n  piece 3: HttpStatus after 4 tries\n\n8 pieces failed and none succeeded.\nerror: AllPiecesFailed\n")
		return 1
	}

	for _, product := range []string{"D2DV", "D2XP"} {
		say("\n=== %s ===\n", product)
		say("\r  25/50 pieces\r  50/50 pieces")
		say("\n50 pieces written, 0 failed -> %s/x\n", os.Getenv("BLIZZARD_LEGACY_DL_CACHE"))
		say("\ninstalling 120 operations from 1 archive(s) -> %s\n", game)
	}
	say("  !! the classic key does not decode — check it for a typo\n")
	say("\npatching to 1.13c\n  https://files.typeguru.nl/diablo/patches/pc/LODPatch_113c.exe\n")
	if mode == "nopatch" {
		say("  no patch archive for 1.13c (ConnectionRefused)\nerror: NoSuchVersion\n")
		return 1
	}

	if err := os.MkdirAll(game, 0o755); err != nil {
		say("error: %v\n", err)
		return 1
	}
	for _, a := range install.Archives {
		if err := os.WriteFile(filepath.Join(game, a.Name), []byte("mpq"), 0o644); err != nil {
			say("error: %v\n", err)
			return 1
		}
	}
	say("the game is in %s", game) // no line end: Flush handles it
	return 0
}

type recorder struct {
	mu       sync.Mutex
	progress []Progress
	logs     []string
}

func (r *recorder) installer(t *testing.T, mode string) Installer {
	t.Setenv(fakeMode, mode)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Installer{Exe: exe, CacheDir: filepath.Join(t.TempDir(), "cache", "legacy-dl")}
}

func (r *recorder) onProgress(p Progress) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.progress = append(r.progress, p)
}

func (r *recorder) onLog(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, line)
}

func options(base string) Options {
	return Options{BaseDir: base, ClassicKey: "bcde-2467-fghj-89km", ExpansionKey: expansion, Owner: " Player "}
}

func TestArgs(t *testing.T) {
	o := options("/games/Diablo II").Normalise()
	got := args(o, "Diablo II")
	want := []string{
		"install", "D2XP", "1.13c",
		"--game", "Diablo II",
		"--jobs", "8",
		"--os", "WIN", "--platform", "win32",
		"--cdkey", classic,
		"--cdkey-expansion", expansion,
		"--owner", "Player",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("args =\n%q\nwant\n%q", got, want)
	}

	o.Owner = ""
	if slices.Contains(args(o, "x"), "--owner") {
		t.Error("an empty owner is passed")
	}
}

func TestCheck(t *testing.T) {
	abs, _ := filepath.Abs("Diablo II")
	ok := options(abs).Normalise()
	if err := ok.Check(); err != nil {
		t.Fatal(err)
	}

	bad := map[string]Options{
		"no classic key":   {Version: "1.13c", BaseDir: abs, ExpansionKey: "x"},
		"no expansion key": {Version: "1.13c", BaseDir: abs, ClassicKey: "x"},
		"spaces only":      Options{Version: "1.13c", BaseDir: abs, ClassicKey: " - ", ExpansionKey: "x"}.Normalise(),
		"relative folder":  {Version: "1.13c", BaseDir: "Diablo II", ClassicKey: "x", ExpansionKey: "x"},
		"unknown version":  {Version: "1.15", BaseDir: abs, ClassicKey: "x", ExpansionKey: "x"},
		"flag as version":  {Version: "--game", BaseDir: abs, ClassicKey: "x", ExpansionKey: "x"},
	}
	for name, o := range bad {
		if err := o.Check(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	// Keys aren't validated: any non-empty text will do.
	junk := Options{BaseDir: abs, ClassicKey: "x", ExpansionKey: "y"}.Normalise()
	if err := junk.Check(); err != nil {
		t.Errorf("junk keys rejected: %v", err)
	}
}

func TestCheckTarget(t *testing.T) {
	root := t.TempDir()

	if err := checkTarget(filepath.Join(root, "new")); err != nil {
		t.Errorf("new folder: %v", err)
	}

	empty := filepath.Join(root, "empty")
	os.Mkdir(empty, 0o755)
	if err := checkTarget(empty); err != nil {
		t.Errorf("empty folder: %v", err)
	}

	game := filepath.Join(root, "game")
	os.Mkdir(game, 0o755)
	os.WriteFile(filepath.Join(game, "D2DATA.MPQ"), nil, 0o644)
	if err := checkTarget(game); !errors.Is(err, ErrHasGame) {
		t.Errorf("folder with the game: %v", err)
	}

	other := filepath.Join(root, "other")
	os.Mkdir(other, 0o755)
	os.WriteFile(filepath.Join(other, "notes.txt"), nil, 0o644)
	if err := checkTarget(other); !errors.Is(err, ErrNotEmpty) {
		t.Errorf("folder with other files: %v", err)
	}

	// An unfinished install of ours resumes, archives and all.
	os.WriteFile(filepath.Join(game, marker), nil, 0o644)
	if err := checkTarget(game); err != nil {
		t.Errorf("unfinished install: %v", err)
	}

	file := filepath.Join(root, "file")
	os.WriteFile(file, nil, 0o644)
	if err := checkTarget(file); err == nil {
		t.Error("a file was accepted as a folder")
	}
}

func TestWorkDir(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "diablo2org")

	dir, game, cache := workDir(filepath.Join(data, "Diablo II"), filepath.Join(data, "cache", "legacy-dl"))
	if dir != data || game != "Diablo II" || cache != filepath.Join("cache", "legacy-dl") {
		t.Errorf("same parent: %q %q %q", dir, game, cache)
	}

	dir, game, cache = workDir(filepath.Join(root, "Games", "Diablo II"), filepath.Join(data, "cache"))
	if dir != root || game != filepath.Join("Games", "Diablo II") || cache != filepath.Join("diablo2org", "cache") {
		t.Errorf("shared ancestor: %q %q %q", dir, game, cache)
	}
}

func TestInstall(t *testing.T) {
	var r recorder
	in := r.installer(t, "ok")
	base := filepath.Join(t.TempDir(), "Diablo II")

	res, err := in.Install(context.Background(), options(base), r.onProgress)
	if err != nil {
		t.Fatal(err)
	}
	if rep, err := install.Check(base, true); err != nil || !rep.OK() {
		t.Fatalf("base doesn't pass Check: %+v, %v", rep, err)
	}
	if _, err := os.Stat(filepath.Join(base, marker)); !os.IsNotExist(err) {
		t.Error("the in-progress marker was left behind")
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "classic key does not decode") {
		t.Errorf("warnings = %q", res.Warnings)
	}

	var stages []string
	for _, p := range r.progress {
		s := p.Product + " " + string(p.Stage)
		if p.Total > 0 {
			s += fmt.Sprintf(" %d/%d", p.Done, p.Total)
		}
		if len(stages) == 0 || stages[len(stages)-1] != s {
			stages = append(stages, s)
		}
	}
	want := []string{
		"D2DV download", "D2DV download 25/50", "D2DV download 50/50", "D2DV install",
		"D2XP download", "D2XP download 25/50", "D2XP download 50/50", "D2XP install",
		"D2XP patch",
	}
	if !slices.Equal(stages, want) {
		t.Errorf("progress =\n%q\nwant\n%q", stages, want)
	}

	// Installing again into the finished folder is the existing-install path.
	if _, err := in.Install(context.Background(), options(base), nil); !errors.Is(err, ErrHasGame) {
		t.Errorf("second install: %v", err)
	}
}

func TestInstallLogsNoKeys(t *testing.T) {
	var r recorder
	in := r.installer(t, "ok")
	base := filepath.Join(t.TempDir(), "Diablo II")

	out := newOutput(nil, r.onLog, classic, expansion)
	fmt.Fprintf(out, "args: install D2XP --cdkey %s --cdkey-expansion %s\n", "bcde-2467-fghj-89km", expansion)
	out.Flush()

	// And through a real run.
	logs := filepath.Join(t.TempDir(), "log.txt")
	f, _ := os.Create(logs)
	defer f.Close()
	in.Log = slog.New(slog.NewTextHandler(f, nil))
	if _, err := in.Install(context.Background(), options(base), nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(logs)
	all := strings.Join(r.logs, "\n") + "\n" + string(data)

	if !strings.Contains(all, "args: install D2XP") {
		t.Fatalf("legacy-dl's output wasn't logged: %s", all)
	}
	flat := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(all))
	for _, key := range []string{classic, expansion} {
		if strings.Contains(flat, key) {
			t.Errorf("a key reached the log:\n%s", all)
		}
	}
}

func TestInstallErrors(t *testing.T) {
	for mode, want := range map[string]error{"nopatch": ErrPatchSource, "cdn": ErrDownload} {
		var r recorder
		in := r.installer(t, mode)
		base := filepath.Join(t.TempDir(), "Diablo II")

		_, err := in.Install(context.Background(), options(base), nil)
		if !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", mode, err, want)
		}
		// The folder is left for a retry to resume.
		if err := checkTarget(base); err != nil {
			t.Errorf("%s: can't retry: %v", mode, err)
		}
	}
}

func TestInstallCancel(t *testing.T) {
	var r recorder
	in := r.installer(t, "hang")
	base := filepath.Join(t.TempDir(), "Diablo II")

	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{}, 1)
	progress := func(p Progress) {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	go func() {
		<-started
		cancel()
	}()

	begin := time.Now()
	_, err := in.Install(ctx, options(base), progress)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if time.Since(begin) > 20*time.Second {
		t.Fatal("cancelling didn't stop legacy-dl")
	}
}

func TestCache(t *testing.T) {
	in := Installer{CacheDir: filepath.Join(t.TempDir(), "legacy-dl")}
	os.MkdirAll(filepath.Join(in.CacheDir, "payload"), 0o755)
	os.WriteFile(filepath.Join(in.CacheDir, "payload", "piece"), make([]byte, 1000), 0o644)

	if got := in.CacheSize(); got != 1000 {
		t.Errorf("CacheSize = %d", got)
	}
	if err := in.ClearCache(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(in.CacheDir); !os.IsNotExist(err) {
		t.Error("cache still there")
	}
	if err := (Installer{}).ClearCache(); err == nil {
		t.Error("cleared with no cache folder set")
	}
}

// Output captured from blizzard-legacy-dl v0.2.0 (fetch d2dv --to 49, macOS),
// with an error line from the same binary.
func TestOutputRealCapture(t *testing.T) {
	var got []Progress
	var logged []string
	out := newOutput(func(p Progress) { got = append(got, p) }, func(l string) { logged = append(logged, l) })
	out.Write([]byte("\r  25/50 pieces\r  39/50 pieces\r  50/50 pieces\n50 pieces written, 0 failed -> ./D2-1.14b-Installer-enUS\n"))
	out.Write([]byte("no file 'x', and fetching product X failed\nerror: NoStub\n"))
	out.Flush()

	want := []Progress{{Stage: StageDownload, Done: 25, Total: 50}, {Stage: StageDownload, Done: 39, Total: 50}, {Stage: StageDownload, Done: 50, Total: 50}}
	if !slices.Equal(got, want) {
		t.Errorf("progress = %+v", got)
	}
	if len(logged) != 3 {
		t.Errorf("logged %q", logged)
	}
	if err := out.err(errors.New("exit status 1")); !errors.Is(err, ErrDownload) {
		t.Errorf("err = %v", err)
	}
}
