package install

import (
	"os"
	"path/filepath"
	"testing"
)

func base(t *testing.T, names ...string) string {
	t.Helper()

	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("archive "+n), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

func TestCheck(t *testing.T) {
	dir := base(t, "d2data.mpq", "d2sfx.mpq", "d2speech.mpq", "d2music.mpq")

	r, err := Check(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if r.OK() || len(r.Missing) != 1 || r.Missing[0] != "d2exp.mpq" {
		t.Errorf("expansion check: missing %v, want only d2exp.mpq", r.Missing)
	}
	if len(r.MissingOptional) != 5 {
		t.Errorf("optional missing = %v", r.MissingOptional)
	}

	// Classic doesn't need the expansion archives.
	if r, _ := Check(dir, false); !r.OK() {
		t.Errorf("classic check failed: %v", r.Missing)
	}

	if _, err := Check(filepath.Join(dir, "nope"), true); err == nil {
		t.Error("missing folder not reported")
	}
}

func TestLinkArchives(t *testing.T) {
	b := base(t, "d2data.mpq", "d2sfx.mpq", "d2speech.mpq", "d2exp.mpq")
	server := ServerDir(b, "slashdiablo")

	if err := LinkArchives(b, server, true, false); err != nil {
		t.Skipf("hard links not available: %v", err)
	}

	for _, n := range []string{"d2data.mpq", "d2exp.mpq"} {
		src, _ := os.Stat(filepath.Join(b, n))
		dst, err := os.Stat(filepath.Join(server, n))
		if err != nil || !os.SameFile(src, dst) {
			t.Errorf("%s is not linked: %v", n, err)
		}
	}

	if _, err := os.Stat(filepath.Join(server, "d2music.mpq")); !os.IsNotExist(err) {
		t.Error("linked an archive the base doesn't have")
	}

	// Running again is a no-op, and a stale file gets replaced by a link.
	os.Remove(filepath.Join(server, "d2sfx.mpq"))
	os.WriteFile(filepath.Join(server, "d2sfx.mpq"), []byte("stale"), 0o644)

	if err := LinkArchives(b, server, true, false); err != nil {
		t.Fatal(err)
	}

	src, _ := os.Stat(filepath.Join(b, "d2sfx.mpq"))
	dst, _ := os.Stat(filepath.Join(server, "d2sfx.mpq"))
	if !os.SameFile(src, dst) {
		t.Error("stale copy was not replaced with a link")
	}
}

func TestCopyArchives(t *testing.T) {
	b := base(t, "d2data.mpq", "d2sfx.mpq")
	server := filepath.Join(t.TempDir(), "srv")

	if err := CopyArchives(b, server, false, false); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(server, "d2data.mpq"))
	if err != nil || string(data) != "archive d2data.mpq" {
		t.Errorf("copy = %q, %v", data, err)
	}

	r, _ := Check(b, false)
	if CopySize(r) != int64(len("archive d2data.mpq")+len("archive d2sfx.mpq")) {
		t.Errorf("CopySize = %d", CopySize(r))
	}
}

func TestWarning(t *testing.T) {
	t.Setenv("ProgramFiles", `C:\Program Files`)
	t.Setenv("ProgramFiles(x86)", `C:\Program Files (x86)`)

	if Warning(`C:\Program Files (x86)\Diablo II`) == "" {
		t.Error("no warning for Program Files (x86)")
	}
	if Warning(`C:\Games\Diablo II`) != "" {
		t.Error("warning for a normal folder")
	}
	if Warning(`C:\Program Files Extra\Diablo II`) != "" {
		t.Error("prefix match on a similarly named folder")
	}
}

// base114 is a base whose d2sfx.mpq has a 1.14 (attributes), as Blizzard's
// current installer leaves; the other archives are placeholders.
func base114(t *testing.T) string {
	t.Helper()
	b := base(t, "d2data.mpq", "d2speech.mpq", "d2exp.mpq")
	data, err := os.ReadFile(filepath.Join("testdata", "d2sfx-114.mpq"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b, "d2sfx.mpq"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLinkArchivesPre114Server(t *testing.T) {
	b := base114(t)
	server := ServerDir(b, "slashdiablo")
	before, _ := os.ReadFile(filepath.Join(b, "d2sfx.mpq"))

	if err := LinkArchives(b, server, true, true); err != nil {
		t.Skipf("hard links not available: %v", err)
	}

	// d2sfx.mpq is a corrected copy; the rest are links.
	src, _ := os.Stat(filepath.Join(b, "d2sfx.mpq"))
	dst, err := os.Stat(filepath.Join(server, "d2sfx.mpq"))
	if err != nil || os.SameFile(src, dst) {
		t.Fatalf("d2sfx.mpq should be a copy: %v", err)
	}
	if dst.Size() != src.Size() || needsFix(filepath.Join(server, "d2sfx.mpq")) {
		t.Error("the copy isn't corrected, or differs in size")
	}
	d, _ := os.Stat(filepath.Join(server, "d2data.mpq"))
	if s, _ := os.Stat(filepath.Join(b, "d2data.mpq")); !os.SameFile(s, d) {
		t.Error("d2data.mpq isn't linked")
	}

	// The base is never changed.
	if after, _ := os.ReadFile(filepath.Join(b, "d2sfx.mpq")); string(after) != string(before) || !needsFix(filepath.Join(b, "d2sfx.mpq")) {
		t.Error("the base archive changed")
	}

	// Running again keeps the corrected copy.
	stamp := dst.ModTime()
	if err := LinkArchives(b, server, true, true); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.Stat(filepath.Join(server, "d2sfx.mpq")); !again.ModTime().Equal(stamp) || os.SameFile(src, again) {
		t.Error("the corrected copy was rewritten or replaced")
	}

	// A link left by an older launcher is replaced with a corrected copy.
	os.Remove(filepath.Join(server, "d2sfx.mpq"))
	os.Link(filepath.Join(b, "d2sfx.mpq"), filepath.Join(server, "d2sfx.mpq"))
	if err := LinkArchives(b, server, true, true); err != nil {
		t.Fatal(err)
	}
	if needsFix(filepath.Join(server, "d2sfx.mpq")) {
		t.Error("the old link wasn't replaced")
	}
	if after, _ := os.ReadFile(filepath.Join(b, "d2sfx.mpq")); string(after) != string(before) {
		t.Error("replacing the link changed the base archive")
	}
}

func TestLinkArchives114Server(t *testing.T) {
	b := base114(t)
	server := ServerDir(b, "pd2")

	// A 1.14 server reads the 1.14 archive as it is: linked.
	if err := LinkArchives(b, server, true, false); err != nil {
		t.Skipf("hard links not available: %v", err)
	}
	src, _ := os.Stat(filepath.Join(b, "d2sfx.mpq"))
	dst, _ := os.Stat(filepath.Join(server, "d2sfx.mpq"))
	if !os.SameFile(src, dst) {
		t.Error("d2sfx.mpq isn't linked for a 1.14 server")
	}
}

func TestLinkArchivesPre114Base(t *testing.T) {
	b := base(t, "d2data.mpq", "d2speech.mpq", "d2exp.mpq")
	data, _ := os.ReadFile(filepath.Join("testdata", "d2sfx-113.mpq"))
	os.WriteFile(filepath.Join(b, "d2sfx.mpq"), data, 0o644)
	server := ServerDir(b, "slashdiablo")

	// A pre-1.14 base needs nothing: linked as always.
	if err := LinkArchives(b, server, true, true); err != nil {
		t.Skipf("hard links not available: %v", err)
	}
	src, _ := os.Stat(filepath.Join(b, "d2sfx.mpq"))
	dst, _ := os.Stat(filepath.Join(server, "d2sfx.mpq"))
	if !os.SameFile(src, dst) {
		t.Error("a pre-1.14 d2sfx.mpq wasn't linked")
	}
}

func TestCopyArchivesPre114Server(t *testing.T) {
	b := base114(t)
	server := filepath.Join(t.TempDir(), "srv")

	// A plain copy left from before is corrected too.
	os.MkdirAll(server, 0o755)
	plain, _ := os.ReadFile(filepath.Join(b, "d2sfx.mpq"))
	os.WriteFile(filepath.Join(server, "d2sfx.mpq"), plain, 0o644)

	if err := CopyArchives(b, server, true, true); err != nil {
		t.Fatal(err)
	}
	if needsFix(filepath.Join(server, "d2sfx.mpq")) {
		t.Error("the copied d2sfx.mpq isn't corrected")
	}
	if data, _ := os.ReadFile(filepath.Join(server, "d2data.mpq")); string(data) != "archive d2data.mpq" {
		t.Errorf("d2data.mpq = %q", data)
	}
}

func TestCopyArchives114ServerKeepsCopy(t *testing.T) {
	b := base114(t)
	server := filepath.Join(t.TempDir(), "srv")
	if err := CopyArchives(b, server, true, false); err != nil {
		t.Fatal(err)
	}
	first, _ := os.Stat(filepath.Join(server, "d2sfx.mpq"))

	// A 1.14 server's plain copy of the 1.14 archive is right as it is: not
	// copied again on the next update.
	if err := CopyArchives(b, server, true, false); err != nil {
		t.Fatal(err)
	}
	again, _ := os.Stat(filepath.Join(server, "d2sfx.mpq"))
	if !again.ModTime().Equal(first.ModTime()) || !needsFix(filepath.Join(server, "d2sfx.mpq")) {
		t.Error("the 1.14 copy was rewritten or changed")
	}
}
