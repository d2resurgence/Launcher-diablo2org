package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPinFile(t *testing.T) {
	data, err := os.ReadFile("../legacy-dl.json")
	if err != nil {
		t.Fatal(err)
	}
	pin, err := parse(data)
	if err != nil {
		t.Fatal(err)
	}

	// Every platform the launcher is packaged for.
	for _, target := range []string{"windows/amd64", "windows/arm64", "darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64"} {
		goos, goarch, _ := strings.Cut(target, "/")
		f, err := pin.File(goos, goarch)
		if err != nil {
			t.Errorf("%s: %v", target, err)
			continue
		}
		if !strings.HasPrefix(f.URL, pin.URL) || !strings.Contains(f.URL, pin.Version) {
			t.Errorf("%s: url %s isn't under the pinned release", target, f.URL)
		}
	}

	if _, err := pin.File("plan9", "386"); err == nil {
		t.Error("an unknown target has a binary")
	}
}

func TestParseRejects(t *testing.T) {
	const hash = "0327761eddf0ce3554ccfd29fd64b0da023c510f6b69cfb3265dfc7e269b8962"
	license := `"license":{"url":"https://example.com/LICENSE","sha256":"` + hash + `"}`

	tests := map[string]string{
		"http url":      `{"version":"v1","url":"http://example.com/",` + license + `}`,
		"no slash":      `{"version":"v1","url":"https://example.com",` + license + `}`,
		"bad hash":      `{"version":"v1","url":"https://example.com/",` + license + `,"assets":{"linux/amd64":{"name":"x","sha256":"abc"}}}`,
		"path in name":  `{"version":"v1","url":"https://example.com/",` + license + `,"assets":{"linux/amd64":{"name":"../x","sha256":"` + hash + `"}}}`,
		"unknown field": `{"version":"v1","url":"https://example.com/",` + license + `,"extra":1}`,
		"upper hash":    `{"version":"v1","url":"https://example.com/",` + license + `,"assets":{"linux/amd64":{"name":"x","sha256":"` + strings.ToUpper(hash) + `"}}}`,
	}
	for name, data := range tests {
		if _, err := parse([]byte(data)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestWriteChecksHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bin", "tool")
	data := []byte("binary")

	if err := write(data, sum([]byte("other")), path, 0o755); err == nil {
		t.Fatal("wrote a file whose hash doesn't match")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a mismatched file was left behind")
	}

	if err := write(data, sum(data), path, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "binary" {
		t.Fatalf("read back %q, %v", got, err)
	}

	// Already there with the right hash: no download is attempted.
	if err := fetch(File{URL: "https://invalid.invalid/", SHA256: sum(data)}, path, 0o755); err != nil {
		t.Fatalf("refetched a file that was already there: %v", err)
	}
}
