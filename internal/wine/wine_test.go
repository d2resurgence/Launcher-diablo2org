package wine

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ulikunitz/xz"

	"github.com/diablo2org/launcher/internal/fetch"
)

type entry struct {
	name, body, link string
	typ              byte
	mode             int64
}

func archive(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	xw, err := xz.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(xw)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Typeflag: e.typ, Mode: e.mode, Linkname: e.link, Size: int64(len(e.body))}
		if h.Mode == 0 {
			h.Mode = 0o644
		}
		if e.typ != tar.TypeReg {
			h.Size = 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.typ == tar.TypeReg {
			tw.Write([]byte(e.body))
		}
	}
	tw.Close()
	xw.Close()
	return buf.Bytes()
}

func TestUnpack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need privileges on Windows")
	}
	dest := filepath.Join(t.TempDir(), "wine")
	data := archive(t,
		entry{name: "Wine Stable.app/", typ: tar.TypeDir, mode: 0o755},
		entry{name: "Wine Stable.app/Contents/Resources/wine/bin/wine", typ: tar.TypeReg, body: "#!", mode: 0o755},
		entry{name: "Wine Stable.app/Contents/Resources/wine/lib/libwine.1.0.dylib", typ: tar.TypeReg, body: "lib"},
		entry{name: "Wine Stable.app/Contents/Resources/wine/lib/libwine.dylib", typ: tar.TypeSymlink, link: "libwine.1.0.dylib"},
		entry{name: "Wine Stable.app/Contents/Resources/wine/bin/wine64", typ: tar.TypeLink, link: "Wine Stable.app/Contents/Resources/wine/bin/wine"},
	)
	if err := Unpack(bytes.NewReader(data), dest); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(dest, "Wine Stable.app/Contents/Resources/wine/bin/wine")
	info, err := os.Stat(bin)
	if err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Errorf("wine isn't executable: %v %v", info.Mode(), err)
	}
	lib := filepath.Join(dest, "Wine Stable.app/Contents/Resources/wine/lib/libwine.dylib")
	if target, err := os.Readlink(lib); err != nil || target != "libwine.1.0.dylib" {
		t.Errorf("symlink = %q, %v", target, err)
	}
	if b, err := os.ReadFile(filepath.Join(dest, "Wine Stable.app/Contents/Resources/wine/bin/wine64")); err != nil || string(b) != "#!" {
		t.Errorf("hard link = %q, %v", b, err)
	}

	if err := Unpack(bytes.NewReader(data), dest); err == nil {
		t.Error("unpacked over an existing folder")
	}
}

func TestUnpackRefusesEscapes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need privileges on Windows")
	}
	tests := map[string][]entry{
		"absolute":         {{name: "/etc/evil", typ: tar.TypeReg, body: "x"}},
		"dot dot":          {{name: "a/../../evil", typ: tar.TypeReg, body: "x"}},
		"backslash":        {{name: `a\..\evil`, typ: tar.TypeReg, body: "x"}},
		"absolute link":    {{name: "link", typ: tar.TypeSymlink, link: "/etc"}},
		"link leaves":      {{name: "a/link", typ: tar.TypeSymlink, link: "../../x"}},
		"hard link leaves": {{name: "h", typ: tar.TypeLink, link: "../x"}},
		"through a link": {
			{name: "out", typ: tar.TypeSymlink, link: "."},
			{name: "inner", typ: tar.TypeSymlink, link: "out"},
			{name: "inner/evil", typ: tar.TypeReg, body: "x"},
		},
		"device": {{name: "dev", typ: tar.TypeChar}},
	}
	for name, entries := range tests {
		dest := filepath.Join(t.TempDir(), "wine")
		if err := Unpack(bytes.NewReader(archive(t, entries...)), dest); err == nil {
			t.Errorf("%s: unpacked", name)
		}
	}
}

func TestPaths(t *testing.T) {
	tests := map[string]string{
		"/":                                    `Z:\`,
		"/Users/p/Diablo II/slashdiablo/Save/": `Z:\Users\p\Diablo II\slashdiablo\Save`,
		"/home/p/.local/share/diablo2org":      `Z:\home\p\.local\share\diablo2org`,
	}
	for unix, win := range tests {
		if got := WinPath(unix); got != win {
			t.Errorf("WinPath(%q) = %q, want %q", unix, got, win)
		}
		back, err := UnixPath(win)
		if err != nil || back != strings.TrimSuffix(unix, "/") && !(unix == "/" && back == "/") {
			t.Errorf("UnixPath(%q) = %q, %v", win, back, err)
		}
	}

	if got, _ := UnixPath(`z:\Games\Save\`); got != "/Games/Save" {
		t.Errorf("lower-case drive: %q", got)
	}
	for _, bad := range []string{`C:\Games`, `Z:Games`, "", `\\server\share`} {
		if _, err := UnixPath(bad); err == nil {
			t.Errorf("UnixPath(%q) accepted", bad)
		}
	}
}

func TestEnv(t *testing.T) {
	parent := []string{"HOME=/home/p", "WINEPREFIX=/home/p/.wine", "WINEDEBUG=+all", "WINEARCH=win32", "PATH=/usr/bin"}
	env := Env(parent, "/data/prefix", "", "ddraw=n,b")

	want := []string{
		"HOME=/home/p", "PATH=/usr/bin",
		"WINEPREFIX=/data/prefix",
		"WINEDEBUG=-all",
		"WINEDLLOVERRIDES=mscoree,mshtml=;ddraw=n,b",
	}
	if !slices.Equal(env, want) {
		t.Errorf("Env =\n%q\nwant\n%q", env, want)
	}

	if env := Env(nil, "/p", "+relay"); !slices.Contains(env, "WINEDEBUG=+relay") || !slices.Contains(env, "WINEDLLOVERRIDES=mscoree,mshtml=") {
		t.Errorf("Env with a debug setting = %q", env)
	}
}

func TestVersions(t *testing.T) {
	tests := map[string]bool{
		"wine-11.0":          true,
		"wine-9.0 (Staging)": true,
		"wine-9.22":          true,
		"wine-8.21":          false,
		"wine-10.0-rc1":      true,
		"something else":     false,
	}
	for v, ok := range tests {
		if atLeast(v, [2]int{9, 0}) != ok {
			t.Errorf("atLeast(%q) = %v", v, !ok)
		}
	}

	out := "wine: error while loading shared libraries: libgnutls.so.30: cannot open shared object file"
	if got := missingLibrary(out); got != "libgnutls.so.30" {
		t.Errorf("missingLibrary = %q", got)
	}
}

// fakeWine writes a stand-in wine and wineserver into root/name/bin.
func fakeWine(t *testing.T, version string) []entry {
	t.Helper()
	script := "#!/bin/sh\necho wine-" + version + "\n"
	return []entry{
		{name: "Wine Test.app/Contents/Resources/wine/bin/wine", typ: tar.TypeReg, body: script, mode: 0o755},
		{name: "Wine Test.app/Contents/Resources/wine/bin/wineserver", typ: tar.TypeReg, body: script, mode: 0o755},
	}
}

func TestInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in wine is a shell script")
	}
	data := archive(t, fakeWine(t, "11.0")...)
	sum := sha256.Sum256(data)

	var requests int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.ServeContent(w, r, "wine.tar.xz", fetchTime, bytes.NewReader(data))
	}))
	defer srv.Close()

	root := t.TempDir()
	m := Manager{
		Dir:      filepath.Join(root, "runtime", "wine"),
		CacheDir: filepath.Join(root, "cache", "wine"),
		Pin:      Pin{Version: "11.0_1", URL: srv.URL + "/wine.tar.xz", Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])},
		Client:   fetch.New([]string{"127.0.0.1"}, fetch.WithHTTPClient(srv.Client())),
	}

	// An older runtime stays until the new one runs.
	old := filepath.Join(m.Dir, "10.0")
	os.MkdirAll(old, 0o755)

	rt, err := m.Install(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if rt.Version != "11.0_1" || !strings.HasPrefix(rt.Wine, filepath.Join(m.Dir, "11.0_1")) || filepath.Base(rt.Wineserver) != "wineserver" {
		t.Errorf("runtime %+v", rt)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("the old runtime wasn't removed")
	}
	if _, err := os.Stat(filepath.Join(m.CacheDir, "wine.tar.xz")); !os.IsNotExist(err) {
		t.Error("the download was left in the cache")
	}

	// Already there: nothing downloaded.
	before := requests
	if _, err := m.Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if requests != before {
		t.Error("downloaded again")
	}
}

func TestInstallRefusesBrokenRuntime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in wine is a shell script")
	}
	// A wine that fails: the runtime isn't moved into place.
	data := archive(t,
		entry{name: "w/bin/wine", typ: tar.TypeReg, body: "#!/bin/sh\nexit 1\n", mode: 0o755},
		entry{name: "w/bin/wineserver", typ: tar.TypeReg, body: "#!/bin/sh\n", mode: 0o755},
	)
	sum := sha256.Sum256(data)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "w.tar.xz", fetchTime, bytes.NewReader(data))
	}))
	defer srv.Close()

	root := t.TempDir()
	m := Manager{
		Dir:      filepath.Join(root, "runtime"),
		CacheDir: filepath.Join(root, "cache"),
		Pin:      Pin{Version: "11.0_1", URL: srv.URL + "/w.tar.xz", Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])},
		Client:   fetch.New([]string{"127.0.0.1"}, fetch.WithHTTPClient(srv.Client())),
	}
	keep := filepath.Join(m.Dir, "10.0")
	os.MkdirAll(keep, 0o755)

	if _, err := m.Install(context.Background(), nil); err == nil {
		t.Fatal("installed a wine that doesn't run")
	}
	if _, err := os.Stat(filepath.Join(m.Dir, "11.0_1")); !os.IsNotExist(err) {
		t.Error("the broken runtime was moved into place")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("the working runtime was removed")
	}
}

func TestPins(t *testing.T) {
	for _, p := range []Pin{MacPin, LinuxPin} {
		c := fetch.New(Hosts)
		if err := c.Allowed(p.URL); err != nil {
			t.Errorf("%s: %v", p.URL, err)
		}
		if b, err := hex.DecodeString(p.SHA256); err != nil || len(b) != sha256.Size {
			t.Errorf("%s: bad sha256", p.URL)
		}
		if p.Size <= 0 || !strings.HasSuffix(p.Name(), ".tar.xz") {
			t.Errorf("%s: size %d, name %q", p.URL, p.Size, p.Name())
		}
	}
}

var fetchTime = time.Unix(1700000000, 0)
