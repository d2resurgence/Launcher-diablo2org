package wine

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

// The keys the launcher reads and writes, as docs/SPEC.md section 8 lists
// them, under HKEY_CURRENT_USER in the prefix.
const (
	battleNetKey = `HKEY_CURRENT_USER\Software\Battle.net\Configuration`
	gatewayValue = "Diablo II Battle.net Gateways"
	diabloKey    = `HKEY_CURRENT_USER\Software\Blizzard Entertainment\Diablo II`
)

// appDefaultsKey holds Wine's settings for one program, by exe file name.
const appDefaultsKey = `HKEY_CURRENT_USER\Software\Wine\AppDefaults\`

// regTimeout bounds each regedit call.
var regTimeout = time.Minute

// Registry is the game's registry values in the launcher's Wine prefix, as
// launch.Registry. Values are read once per launch, by exporting the two
// keys, and writes are held until Commit imports them all in one go: each
// regedit is a Windows process start. It never edits the prefix's .reg
// files directly: the wineserver holds the registry and rewrites them.
//
// regedit /E puts up an error and waits when a key doesn't exist, so the
// keys are created first; it writes UTF-16, which keeps any path intact,
// where reg query prints in the console's code page.
type Registry struct {
	// Prefix is the prefix to work in, once there is one.
	Prefix func() (*Prefix, error)
	// TempDir holds the .reg files passed to regedit.
	TempDir string

	mu     sync.Mutex
	loaded map[string]map[string]regValue
	// pending holds writes by key, then by lower-cased name.
	pending map[string]map[string]namedValue
}

// regValue is a REG_SZ (multi nil) or REG_MULTI_SZ value, or with del, the
// value's removal.
type regValue struct {
	sz    string
	multi []string
	isMul bool
	del   bool
}

// GatewayList reads the gateway list; none yet is an empty list.
func (r *Registry) GatewayList() ([]string, error) {
	v, err := r.get(battleNetKey, gatewayValue)
	if err != nil {
		return nil, err
	}
	return v.multi, nil
}

// SetGatewayList writes the gateway list at the next Commit.
func (r *Registry) SetGatewayList(values []string) error {
	r.set(battleNetKey, gatewayValue, regValue{multi: append([]string{}, values...), isMul: true})
	return nil
}

// String reads a value under the game's own key; one not set is "".
func (r *Registry) String(name string) (string, error) {
	v, err := r.get(diabloKey, name)
	if err != nil {
		return "", err
	}
	return v.sz, nil
}

// SetString writes a value under the game's own key at the next Commit.
func (r *Registry) SetString(name, value string) error {
	r.set(diabloKey, name, regValue{sz: value})
	return nil
}

// SetGame has Wine report Windows XP to a game older than 1.14, at the next
// Commit. Under Wine on Apple Silicon, 1.13c's Fog.dll overflows its stack
// at start-up when told Windows 10, Wine's default; told XP, it starts. A
// 1.14 game gets Wine's default back.
func (r *Registry) SetGame(exe, version string) error {
	if exe == "" || strings.ContainsAny(exe, `\/[]`) {
		return fmt.Errorf("unexpected game exe %q", exe)
	}
	v := regValue{del: true}
	if !strings.HasPrefix(version, "1.14") {
		v = regValue{sz: "winxp"}
	}
	r.set(appDefaultsKey+exe, "Version", v)
	return nil
}

func (r *Registry) get(key, name string) (regValue, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if v, ok := r.pending[key][strings.ToLower(name)]; ok {
		return v.value, nil
	}
	if r.loaded == nil {
		if err := r.load(); err != nil {
			return regValue{}, err
		}
	}
	return r.loaded[key][strings.ToLower(name)], nil
}

func (r *Registry) set(key, name string, v regValue) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.pending == nil {
		r.pending = map[string]map[string]namedValue{}
	}
	if r.pending[key] == nil {
		r.pending[key] = map[string]namedValue{}
	}
	r.pending[key][strings.ToLower(name)] = namedValue{name: name, value: v}
}

// Commit imports the held writes, then forgets what was read, so the next
// launch reads afresh: the game may change the values while it runs. Writes
// that fail to import are kept for the next Commit.
func (r *Registry) Commit() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.loaded = nil
	if len(r.pending) == 0 {
		return nil
	}

	keys := map[string][]namedValue{}
	for key, values := range r.pending {
		for _, v := range values {
			keys[key] = append(keys[key], v)
		}
	}
	if err := r.importReg(encodeReg(keys)); err != nil {
		return err
	}
	r.pending = nil
	return nil
}

// load creates the keys if need be, then exports both.
func (r *Registry) load() error {
	p, err := r.Prefix()
	if err != nil {
		return err
	}
	if err := p.Persist(context.Background()); err != nil {
		return err
	}
	if err := r.importReg(encodeReg(map[string][]namedValue{battleNetKey: nil, diabloKey: nil})); err != nil {
		return err
	}

	loaded := map[string]map[string]regValue{}
	for _, key := range []string{battleNetKey, diabloKey} {
		data, err := r.export(p, key)
		if err != nil {
			return err
		}
		values, err := parseExport(data, key)
		if err != nil {
			return fmt.Errorf("reading %s: %w", key, err)
		}
		loaded[key] = values
	}
	r.loaded = loaded
	return nil
}

func (r *Registry) importReg(data []byte) error {
	p, err := r.Prefix()
	if err != nil {
		return err
	}
	path, err := r.temp("import", data)
	if err != nil {
		return err
	}
	defer os.Remove(path)

	if _, err := p.output(context.Background(), regTimeout, "regedit", "/S", WinPath(path)); err != nil {
		return fmt.Errorf("writing the registry: %w", err)
	}
	return nil
}

func (r *Registry) export(p *Prefix, key string) ([]byte, error) {
	path, err := r.temp("export", nil)
	if err != nil {
		return nil, err
	}
	defer os.Remove(path)
	// regedit writes the file itself.
	os.Remove(path)

	if _, err := p.output(context.Background(), regTimeout, "regedit", "/E", WinPath(path), key); err != nil {
		return nil, fmt.Errorf("reading the registry: %w", err)
	}
	return os.ReadFile(path)
}

func (r *Registry) temp(kind string, data []byte) (string, error) {
	if err := os.MkdirAll(r.TempDir, 0o755); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(r.TempDir, kind+"-*.reg")
	if err != nil {
		return "", err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return filepath.Abs(f.Name())
}

type namedValue struct {
	name  string
	value regValue
}

// encodeReg writes a .reg file for regedit /S: UTF-16LE with a BOM, as
// regedit only reads hex(1) and hex(7) data as UTF-16 from a UTF-16 file.
// Values are written as hex so no escaping is needed. A key with no values
// is still listed, which creates it.
func encodeReg(keys map[string][]namedValue) []byte {
	var b strings.Builder
	b.WriteString("Windows Registry Editor Version 5.00\r\n")

	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)

	for _, key := range names {
		b.WriteString("\r\n[" + key + "]\r\n")
		values := keys[key]
		sort.Slice(values, func(i, j int) bool { return values[i].name < values[j].name })
		for _, v := range values {
			if v.value.del {
				b.WriteString(quote(v.name) + "=-\r\n")
				continue
			}
			kind, data := "hex(1)", utf16z(v.value.sz)
			if v.value.isMul {
				kind = "hex(7)"
				data = nil
				for _, s := range v.value.multi {
					data = append(data, utf16z(s)...)
				}
				data = append(data, 0, 0)
			}
			b.WriteString(quote(v.name) + "=" + kind + ":" + hexList(data) + "\r\n")
		}
	}

	return append([]byte{0xff, 0xfe}, utf16le(b.String())...)
}

// utf16z is s in UTF-16LE with a terminating NUL.
func utf16z(s string) []byte {
	return append(utf16le(s), 0, 0)
}

func utf16le(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 0, len(units)*2)
	for _, u := range units {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

func hexList(data []byte) string {
	parts := make([]string, len(data))
	for i, c := range data {
		parts[i] = hex.EncodeToString([]byte{c})
	}
	return strings.Join(parts, ",")
}

func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// parseExport reads the values of one key from a regedit /E file, by
// lower-cased name. Subkeys and value types other than strings are skipped.
func parseExport(data []byte, key string) (map[string]regValue, error) {
	text, err := decodeUTF16(data)
	if err != nil {
		return nil, err
	}

	values := map[string]regValue{}
	inKey := false
	for _, line := range joinContinued(text) {
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inKey = strings.EqualFold(line[1:len(line)-1], key)
			continue
		}
		if !inKey || !strings.HasPrefix(line, `"`) {
			continue
		}
		name, rest, ok := unquote(line)
		if !ok || !strings.HasPrefix(rest, "=") {
			continue
		}
		rest = rest[1:]

		switch {
		case strings.HasPrefix(rest, `"`):
			s, tail, ok := unquote(rest)
			if ok && tail == "" {
				values[strings.ToLower(name)] = regValue{sz: s}
			}
		case strings.HasPrefix(rest, "hex(7):"):
			b, err := parseHex(rest[len("hex(7):"):])
			if err != nil {
				return nil, err
			}
			values[strings.ToLower(name)] = regValue{multi: splitMulti(b), isMul: true}
		case strings.HasPrefix(rest, "hex(1):"), strings.HasPrefix(rest, "hex(2):"):
			b, err := parseHex(rest[len("hex(1):"):])
			if err != nil {
				return nil, err
			}
			values[strings.ToLower(name)] = regValue{sz: strings.TrimRight(fromUTF16(b), "\x00")}
		}
	}
	return values, nil
}

func decodeUTF16(data []byte) (string, error) {
	if !bytes.HasPrefix(data, []byte{0xff, 0xfe}) {
		return "", errors.New("not a UTF-16 .reg file")
	}
	return fromUTF16(data[2:]), nil
}

func fromUTF16(b []byte) string {
	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(units))
}

// joinContinued splits text into lines, joining a hex value regedit wrapped
// over several with trailing backslashes. A quoted string never ends in a
// bare backslash: its own are written \\.
func joinContinued(text string) []string {
	var out []string
	cur, continued := "", false
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if continued {
			cur += line
		} else {
			cur = line
		}
		if strings.HasSuffix(cur, `\`) && strings.Contains(cur, "=hex") {
			cur, continued = strings.TrimSuffix(cur, `\`), true
			continue
		}
		out = append(out, cur)
		continued = false
	}
	return out
}

// unquote reads a "…" string with regedit's \\ and \" escapes from the start
// of s, returning it and what follows.
func unquote(s string) (string, string, bool) {
	if !strings.HasPrefix(s, `"`) {
		return "", "", false
	}
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\':
			if i+1 < len(s) {
				i++
				b.WriteByte(s[i])
			}
		case '"':
			return b.String(), s[i+1:], true
		default:
			b.WriteByte(c)
		}
	}
	return "", "", false
}

func parseHex(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var out []byte
	for _, part := range strings.Split(s, ",") {
		b, err := hex.DecodeString(strings.TrimSpace(part))
		if err != nil || len(b) != 1 {
			return nil, fmt.Errorf("bad hex %q", part)
		}
		out = append(out, b[0])
	}
	return out, nil
}

// splitMulti reads REG_MULTI_SZ data: NUL-terminated UTF-16 strings, ended
// by an empty one.
func splitMulti(b []byte) []string {
	var out []string
	for _, s := range strings.Split(fromUTF16(b), "\x00") {
		if s == "" {
			break
		}
		out = append(out, s)
	}
	return out
}
