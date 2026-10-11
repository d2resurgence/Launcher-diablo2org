package mpq

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestCrypto(t *testing.T) {
	// Storm's values, as libd2's tests have them.
	if cryptTable[0] != 0x55C6_36E2 || cryptTable[1] != 0x02BE_0170 {
		t.Errorf("crypt table starts %08x %08x", cryptTable[0], cryptTable[1])
	}
	if h := hashString("(hash table)", hashFileKey); h != 0xC3AF_3770 {
		t.Errorf("(hash table) key %08x", h)
	}
	if h := hashString("(block table)", hashFileKey); h != 0xEC83_B3A3 {
		t.Errorf("(block table) key %08x", h)
	}
	if hashString(`data\global\excel\armor.txt`, hashNameA) != hashString("DATA/Global/Excel/Armor.txt", hashNameA) {
		t.Error("case or slashes change a name's hash")
	}

	buf := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 0xAA}
	orig := append([]byte{}, buf...)
	encrypt(buf, 0x1234_5678)
	if bytes.Equal(buf, orig) || buf[12] != 0xAA {
		t.Error("encrypt didn't, or touched the trailing partial dword")
	}
	decrypt(buf, 0x1234_5678)
	if !bytes.Equal(buf, orig) {
		t.Error("decrypt isn't encrypt's inverse")
	}
}

type member struct {
	name string
	data []byte
	// zlib compresses the member in sectors with a sector table.
	zlib bool
}

// build assembles an archive: header, members, hash table, block table.
func build(t *testing.T, sectorShift uint16, members ...member) []byte {
	t.Helper()
	sector := uint32(512) << sectorShift
	const hashLen = 16

	body := []byte{}
	pos := uint32(32)
	var blocks []blockEntry
	for _, m := range members {
		var packed []byte
		flags := uint32(0x8000_0000)
		if m.zlib {
			flags |= flagCompress
			n := (uint32(len(m.data)) + sector - 1) / sector
			table := make([]byte, (n+1)*4)
			var data []byte
			for i := uint32(0); i < n; i++ {
				binary.LittleEndian.PutUint32(table[i*4:], uint32(len(table)+len(data)))
				chunk := m.data[i*sector : min((i+1)*sector, uint32(len(m.data)))]
				var z bytes.Buffer
				zw := zlib.NewWriter(&z)
				zw.Write(chunk)
				zw.Close()
				if z.Len()+1 < len(chunk) {
					data = append(data, compZlib)
					data = append(data, z.Bytes()...)
				} else {
					data = append(data, chunk...)
				}
			}
			binary.LittleEndian.PutUint32(table[n*4:], uint32(len(table)+len(data)))
			packed = append(table, data...)
		} else {
			packed = m.data
		}
		blocks = append(blocks, blockEntry{pos: pos, packed: uint32(len(packed)), size: uint32(len(m.data)), flags: flags})
		body = append(body, packed...)
		pos += uint32(len(packed))
	}

	hashes := make([]hashEntry, hashLen)
	for i := range hashes {
		hashes[i] = hashEntry{nameA: emptySlot, nameB: emptySlot, locale: 0xFFFF, platform: 0xFFFF, block: emptySlot}
	}
	for bi, m := range members {
		i := hashString(m.name, hashTableOffset) & (hashLen - 1)
		for hashes[i].block != emptySlot {
			i = (i + 1) & (hashLen - 1)
		}
		hashes[i] = hashEntry{nameA: hashString(m.name, hashNameA), nameB: hashString(m.name, hashNameB), block: uint32(bi)}
	}

	ht := make([]byte, hashLen*16)
	for i, e := range hashes {
		r := ht[i*16:]
		binary.LittleEndian.PutUint32(r, e.nameA)
		binary.LittleEndian.PutUint32(r[4:], e.nameB)
		binary.LittleEndian.PutUint16(r[8:], e.locale)
		binary.LittleEndian.PutUint16(r[10:], e.platform)
		binary.LittleEndian.PutUint32(r[12:], e.block)
	}
	encrypt(ht, hashString("(hash table)", hashFileKey))

	bt := make([]byte, len(blocks)*16)
	for i, b := range blocks {
		r := bt[i*16:]
		binary.LittleEndian.PutUint32(r, b.pos)
		binary.LittleEndian.PutUint32(r[4:], b.packed)
		binary.LittleEndian.PutUint32(r[8:], b.size)
		binary.LittleEndian.PutUint32(r[12:], b.flags)
	}
	encrypt(bt, hashString("(block table)", hashFileKey))

	hashPos := pos
	blockPos := hashPos + uint32(len(ht))
	h := make([]byte, 32)
	copy(h, "MPQ\x1a")
	binary.LittleEndian.PutUint32(h[4:], 32)
	binary.LittleEndian.PutUint32(h[8:], blockPos+uint32(len(bt)))
	binary.LittleEndian.PutUint16(h[14:], sectorShift)
	binary.LittleEndian.PutUint32(h[16:], hashPos)
	binary.LittleEndian.PutUint32(h[20:], blockPos)
	binary.LittleEndian.PutUint32(h[24:], hashLen)
	binary.LittleEndian.PutUint32(h[28:], uint32(len(blocks)))

	out := append(h, body...)
	out = append(out, ht...)
	return append(out, bt...)
}

// attributes builds an (attributes) member: version 100, the given columns,
// and blocks entries of each.
func attributes(columns uint32, blocks int) []byte {
	out := make([]byte, 8)
	binary.LittleEndian.PutUint32(out, 100)
	binary.LittleEndian.PutUint32(out[4:], columns)
	for _, size := range []struct {
		bit uint32
		n   int
	}{{0x1, 4}, {0x2, 8}, {0x4, 16}} {
		if columns&size.bit != 0 {
			out = append(out, make([]byte, size.n*blocks)...)
		}
	}
	return out
}

func write(t *testing.T, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "d2sfx.mpq")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestModernAttributes(t *testing.T) {
	sound := bytes.Repeat([]byte("riff "), 2000)
	modern := build(t, 0,
		member{name: `data\global\sfx\cursor\button.wav`, data: sound},
		// 1.14: CRC32, FILETIME and MD5, zlib-compressed over several sectors.
		member{name: "(attributes)", data: attributes(0x7, 100), zlib: true},
	)
	old := build(t, 3,
		member{name: `data\global\sfx\cursor\button.wav`, data: sound},
		member{name: "(attributes)", data: attributes(0x3, 2)},
	)
	none := build(t, 3, member{name: "a.txt", data: []byte("hello")})

	for name, tt := range map[string]struct {
		data []byte
		want bool
	}{"1.14": {modern, true}, "pre-1.14": {old, false}, "none": {none, false}} {
		modern, err := IsModern(write(t, tt.data))
		if err != nil || modern != tt.want {
			t.Errorf("%s: IsModern = %v, %v; want %v", name, modern, err, tt.want)
		}
	}

	// Members read back, compressed or stored.
	f, _ := os.Open(write(t, modern))
	defer f.Close()
	a, err := Open(f)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := a.Read(`DATA/GLOBAL/SFX/CURSOR/BUTTON.WAV`); err != nil || !bytes.Equal(got, sound) {
		t.Errorf("read the sound: %d bytes, %v", len(got), err)
	}
	if got, err := a.Read("(attributes)"); err != nil || !bytes.Equal(got, attributes(0x7, 100)) {
		t.Errorf("read (attributes): %d bytes, %v", len(got), err)
	}
	if _, err := a.Read("missing"); err != ErrNotFound {
		t.Errorf("missing member: %v", err)
	}
}

func TestCopyWithoutAttributes(t *testing.T) {
	sound := bytes.Repeat([]byte("riff "), 2000)
	src := write(t, build(t, 0,
		member{name: `data\global\sfx\cursor\button.wav`, data: sound},
		member{name: "(attributes)", data: attributes(0x7, 100), zlib: true},
		member{name: `data\global\sfx\misc\gold.wav`, data: []byte("gold")},
	))
	before, _ := os.ReadFile(src)
	dst := filepath.Join(t.TempDir(), "d2sfx.mpq")

	if err := CopyWithoutAttributes(src, dst); err != nil {
		t.Fatal(err)
	}

	after, _ := os.ReadFile(src)
	if !bytes.Equal(before, after) {
		t.Fatal("the source archive changed")
	}
	fixed, _ := os.ReadFile(dst)
	if len(fixed) != len(before) {
		t.Errorf("copy is %d bytes, source %d", len(fixed), len(before))
	}
	if modern, err := IsModern(dst); err != nil || modern {
		t.Errorf("copy still modern: %v, %v", modern, err)
	}

	f, _ := os.Open(dst)
	defer f.Close()
	a, err := Open(f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Read("(attributes)"); err != ErrNotFound {
		t.Errorf("(attributes) still listed: %v", err)
	}
	// Every other member is still found, including any that probed past it.
	if got, err := a.Read(`data\global\sfx\cursor\button.wav`); err != nil || !bytes.Equal(got, sound) {
		t.Errorf("button.wav: %v", err)
	}
	if got, err := a.Read(`data\global\sfx\misc\gold.wav`); err != nil || string(got) != "gold" {
		t.Errorf("gold.wav: %q, %v", got, err)
	}

	// Only the hash table differs.
	h := make([]byte, 32)
	copy(h, fixed)
	hashPos := binary.LittleEndian.Uint32(h[16:])
	for i := range fixed {
		if fixed[i] != before[i] && (uint32(i) < hashPos || uint32(i) >= hashPos+16*16) {
			t.Fatalf("byte %d outside the hash table changed", i)
		}
	}

	if err := CopyWithoutAttributes(write(t, build(t, 3, member{name: "a", data: []byte("x")})), dst); err != ErrNotFound {
		t.Errorf("no (attributes): %v", err)
	}
}

func TestOpenRejects(t *testing.T) {
	if _, err := Open(bytes.NewReader([]byte("not an archive at all"))); err == nil {
		t.Error("opened junk")
	}
	bad := build(t, 3, member{name: "a", data: []byte("x")})
	binary.LittleEndian.PutUint32(bad[24:], 3) // not a power of two
	if _, err := Open(bytes.NewReader(bad)); err == nil {
		t.Error("opened an archive with a bad hash table size")
	}
}
