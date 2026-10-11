package wine

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ulikunitz/xz"
)

// maxUnpacked caps what an archive may unpack to. Wine is under 1 GB.
const maxUnpacked = 4 << 30

// Unpack extracts a .tar.xz into dest, which must not exist yet. It keeps
// symbolic links and executable bits, and refuses any entry that would land
// outside dest: absolute paths, .. components, links pointing out, and
// writes through a link.
func Unpack(r io.Reader, dest string) error {
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("%s already exists", dest)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}

	xr, err := xz.NewReader(r)
	if err != nil {
		return err
	}
	tr := tar.NewReader(xr)

	var total int64
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		name, err := entryPath(h.Name)
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		if err := noLinkOnPath(dest, name); err != nil {
			return err
		}

		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			total += h.Size
			if total > maxUnpacked {
				return fmt.Errorf("the archive unpacks to more than %d bytes", int64(maxUnpacked))
			}
			if err := writeFile(target, tr, h); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := linkInside(name, h.Linkname); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(h.Linkname, target); err != nil {
				return err
			}
		case tar.TypeLink:
			src, err := entryPath(h.Linkname)
			if err != nil || src == "" {
				return fmt.Errorf("%s: bad hard link to %q", h.Name, h.Linkname)
			}
			if err := noLinkOnPath(dest, src); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Link(filepath.Join(dest, filepath.FromSlash(src)), target); err != nil {
				return err
			}
		case tar.TypeXGlobalHeader:
			// pax metadata; nothing to write.
		default:
			return fmt.Errorf("%s: unsupported entry type %q", h.Name, h.Typeflag)
		}
	}
}

// entryPath cleans an entry's name, refusing ones that leave the archive.
// "" means the archive's root.
func entryPath(name string) (string, error) {
	if strings.HasPrefix(name, "/") || strings.Contains(name, `\`) {
		return "", fmt.Errorf("unsafe path in archive: %q", name)
	}
	clean := path.Clean(name)
	if clean == "." {
		return "", nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("unsafe path in archive: %q", name)
	}
	return clean, nil
}

// linkInside checks a symbolic link at name, pointing to target, stays
// inside the archive. A .. is only allowed before any other part, as in
// ../lib/x: after one, as in y/.., where it's taken depends on whether y is
// itself a link, which cleaning the path as text can't see.
func linkInside(name, target string) error {
	if target == "" || strings.HasPrefix(target, "/") || strings.Contains(target, `\`) {
		return fmt.Errorf("%s: unsafe link to %q", name, target)
	}
	leading := true
	for _, part := range strings.Split(target, "/") {
		switch part {
		case "..":
			if !leading {
				return fmt.Errorf("%s: link to %q has .. after a folder", name, target)
			}
		case "", ".":
		default:
			leading = false
		}
	}
	resolved := path.Clean(path.Join(path.Dir(name), target))
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return fmt.Errorf("%s: link to %q leaves the archive", name, target)
	}
	return nil
}

// noLinkOnPath refuses to write through a symbolic link: every folder on the
// way to name must be a real folder (or not exist yet), and so must name.
func noLinkOnPath(dest, name string) error {
	p := dest
	for _, part := range strings.Split(name, "/") {
		p = filepath.Join(p, part)
		info, err := os.Lstat(p)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s: would write through a link", name)
		}
	}
	return nil
}

func writeFile(target string, r io.Reader, h *tar.Header) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}

	// Only the permission bits, and always readable and writable by us.
	mode := os.FileMode(h.Mode)&0o755 | 0o600
	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, io.LimitReader(r, h.Size)); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Chmod(target, mode)
}
