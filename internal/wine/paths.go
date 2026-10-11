package wine

import (
	"fmt"
	"path"
	"strings"
)

// A fresh prefix maps drive Z: to the root of the host's filesystem, so any
// host path is reachable from Windows programs through it.

// WinPath is how Windows programs under Wine see a host path: Z: and the
// path with backslashes. The path must be absolute.
func WinPath(unix string) string {
	clean := path.Clean(unix)
	if clean == "/" {
		return `Z:\`
	}
	return `Z:` + strings.ReplaceAll(clean, "/", `\`)
}

// UnixPath is the host path for a Windows path on drive Z:.
func UnixPath(win string) (string, error) {
	if len(win) < 2 || win[1] != ':' || (win[0] != 'Z' && win[0] != 'z') {
		return "", fmt.Errorf("%q isn't on drive Z:", win)
	}
	rest := strings.ReplaceAll(win[2:], `\`, "/")
	if rest == "" {
		rest = "/"
	}
	if !strings.HasPrefix(rest, "/") {
		return "", fmt.Errorf("%q isn't a full path", win)
	}
	return path.Clean(rest), nil
}
