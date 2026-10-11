// Command legacydl fetches the blizzard-legacy-dl binary the launcher ships
// with, pinned by version and SHA-256 in build/legacy-dl.json, and its
// licence for the app's notices. A file already there with the pinned hash
// isn't fetched again.
//
//	go run ./build/legacydl -pin build/legacy-dl.json -os windows -arch amd64 -out bin/blizzard-legacy-dl.exe -license bin/blizzard-legacy-dl-LICENSE.txt
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// maxSize caps a download: the binaries are a few MB.
const maxSize = 64 << 20

// Pin is build/legacy-dl.json.
type Pin struct {
	Version string `json:"version"`
	// URL is the release's download folder; asset names are added to it.
	URL     string           `json:"url"`
	License File             `json:"license"`
	Assets  map[string]Asset `json:"assets"`
}

// File is a download with its expected hash.
type File struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

// Asset is one release binary, keyed in Pin.Assets by GOOS/GOARCH.
type Asset struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

func main() {
	pin := flag.String("pin", "build/legacy-dl.json", "the pin file")
	goos := flag.String("os", "", "target GOOS")
	goarch := flag.String("arch", "", "target GOARCH")
	out := flag.String("out", "", "where to write the binary")
	license := flag.String("license", "", "where to write the licence (optional)")
	flag.Parse()

	if err := run(*pin, *goos, *goarch, *out, *license); err != nil {
		fmt.Fprintln(os.Stderr, "legacydl:", err)
		os.Exit(1)
	}
}

func run(pinPath, goos, goarch, out, license string) error {
	if goos == "" || goarch == "" || out == "" {
		return fmt.Errorf("-os, -arch and -out are required")
	}

	data, err := os.ReadFile(pinPath)
	if err != nil {
		return err
	}
	pin, err := parse(data)
	if err != nil {
		return fmt.Errorf("%s: %w", pinPath, err)
	}

	f, err := pin.File(goos, goarch)
	if err != nil {
		return err
	}
	if err := fetch(f, out, 0o755); err != nil {
		return err
	}
	if license != "" {
		if err := fetch(pin.License, license, 0o644); err != nil {
			return err
		}
	}

	fmt.Printf("blizzard-legacy-dl %s %s/%s -> %s\n", pin.Version, goos, goarch, out)
	return nil
}

func parse(data []byte) (*Pin, error) {
	var p Pin
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, err
	}

	if p.Version == "" || !strings.HasPrefix(p.URL, "https://") || !strings.HasSuffix(p.URL, "/") {
		return nil, fmt.Errorf("needs a version and an https url ending in /")
	}
	if !strings.HasPrefix(p.License.URL, "https://") || !isHash(p.License.SHA256) {
		return nil, fmt.Errorf("license needs an https url and a sha256")
	}
	for key, a := range p.Assets {
		if a.Name == "" || strings.ContainsAny(a.Name, `/\`) || !isHash(a.SHA256) {
			return nil, fmt.Errorf("asset %s needs a plain name and a sha256", key)
		}
	}

	return &p, nil
}

// File is the download for a target.
func (p *Pin) File(goos, goarch string) (File, error) {
	a, ok := p.Assets[goos+"/"+goarch]
	if !ok {
		return File{}, fmt.Errorf("no blizzard-legacy-dl for %s/%s", goos, goarch)
	}

	return File{URL: p.URL + a.Name, SHA256: a.SHA256}, nil
}

func isHash(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size && s == strings.ToLower(s)
}

// fetch writes f to path, unless a file with its hash is already there.
func fetch(f File, path string, mode os.FileMode) error {
	if have, err := os.ReadFile(path); err == nil && sum(have) == f.SHA256 {
		return os.Chmod(path, mode)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(f.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", f.URL, resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return err
	}
	if len(data) > maxSize {
		return fmt.Errorf("%s: larger than %d bytes", f.URL, maxSize)
	}

	return write(data, f.SHA256, path, mode)
}

// write checks data against want and then writes it to path.
func write(data []byte, want, path string, mode os.FileMode) error {
	if got := sum(data); got != want {
		return fmt.Errorf("%s: sha256 %s, pinned %s", filepath.Base(path), got, want)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	// WriteFile's mode is masked by the umask.
	if err := os.Chmod(tmp, mode); err != nil {
		os.Remove(tmp)
		return err
	}

	return os.Rename(tmp, path)
}

func sum(data []byte) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:])
}
