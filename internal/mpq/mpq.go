// Package mpq reads just enough of Blizzard's MPQ archives to make a 1.14
// base archive usable by a pre-1.14 game: it finds an archive's
// (attributes) member, tells whether it's in the 1.14 format, and writes a
// copy of the archive with that member unlisted.
//
// Ported from jaenster's blizzard-legacy-dl and libd2 (MIT licence,
// copyright 2026 jaenster), whose install does the same to the archives it
// builds for older versions.
package mpq

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Hash types, as Storm numbers them.
const (
	hashTableOffset = 0
	hashNameA       = 1
	hashNameB       = 2
	hashFileKey     = 3
)

// Block flags.
const (
	flagImplode    = 0x0000_0100
	flagCompress   = 0x0000_0200
	flagEncrypted  = 0x0001_0000
	flagFixKey     = 0x0002_0000
	flagSingleUnit = 0x0100_0000
	flagSectorCRC  = 0x0400_0000
)

const (
	emptySlot   = 0xFFFF_FFFF
	deletedSlot = 0xFFFF_FFFE
	compZlib    = 0x02
)

// cryptTable drives every MPQ hash and cipher, generated as Storm does.
var cryptTable = func() [0x500]uint32 {
	var t [0x500]uint32
	seed := uint32(0x0010_0001)
	for i := 0; i < 0x100; i++ {
		idx := i
		for j := 0; j < 5; j++ {
			seed = (seed*125 + 3) % 0x2AAAAB
			hi := (seed & 0xFFFF) << 16
			seed = (seed*125 + 3) % 0x2AAAAB
			t[idx] = hi | seed&0xFFFF
			idx += 0x100
		}
	}
	return t
}()

// hashString hashes a member name: case and slash direction don't matter.
func hashString(name string, kind uint32) uint32 {
	s1, s2 := uint32(0x7FED_7FED), uint32(0xEEEE_EEEE)
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '/' {
			c = '\\'
		}
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		s1 = cryptTable[kind<<8+uint32(c)] ^ (s1 + s2)
		s2 = uint32(c) + s1 + s2 + s2<<5 + 3
	}
	return s1
}

func decrypt(data []byte, key uint32) {
	seed := uint32(0xEEEE_EEEE)
	for i := 0; i+4 <= len(data); i += 4 {
		seed += cryptTable[0x400+key&0xFF]
		v := binary.LittleEndian.Uint32(data[i:]) ^ (key + seed)
		binary.LittleEndian.PutUint32(data[i:], v)
		key = (^key<<0x15 + 0x1111_1111) | key>>0x0B
		seed = v + seed + seed<<5 + 3
	}
}

func encrypt(data []byte, key uint32) {
	seed := uint32(0xEEEE_EEEE)
	for i := 0; i+4 <= len(data); i += 4 {
		seed += cryptTable[0x400+key&0xFF]
		plain := binary.LittleEndian.Uint32(data[i:])
		binary.LittleEndian.PutUint32(data[i:], plain^(key+seed))
		key = (^key<<0x15 + 0x1111_1111) | key>>0x0B
		seed = plain + seed + seed<<5 + 3
	}
}

type hashEntry struct {
	nameA, nameB uint32
	locale       uint16
	platform     uint16
	block        uint32
}

type blockEntry struct {
	pos, packed, size, flags uint32
}

// Archive is an open MPQ archive.
type Archive struct {
	r       io.ReaderAt
	base    int64
	sector  uint32
	hashPos int64
	hashes  []hashEntry
	blocks  []blockEntry
}

// ErrNotFound means the archive has no such member.
var ErrNotFound = errors.New("no such member")

// maxTable bounds a table's entries, against a corrupt header.
const maxTable = 1 << 20

// Open reads an archive's header and tables.
func Open(r io.ReaderAt) (*Archive, error) {
	base, err := findHeader(r)
	if err != nil {
		return nil, err
	}
	h := make([]byte, 32)
	if _, err := r.ReadAt(h, base); err != nil {
		return nil, err
	}
	blockSize := binary.LittleEndian.Uint16(h[14:])
	hashPos := int64(binary.LittleEndian.Uint32(h[16:]))
	blockPos := int64(binary.LittleEndian.Uint32(h[20:]))
	hashLen := binary.LittleEndian.Uint32(h[24:])
	blockLen := binary.LittleEndian.Uint32(h[28:])
	// 512 << 23 overflows the sector size to 0.
	if blockSize > 22 || hashLen > maxTable || blockLen > maxTable || hashLen&(hashLen-1) != 0 {
		return nil, errors.New("bad MPQ header")
	}

	a := &Archive{r: r, base: base, sector: 512 << blockSize, hashPos: base + hashPos}

	raw, err := a.table(a.hashPos, hashLen, "(hash table)")
	if err != nil {
		return nil, err
	}
	a.hashes = make([]hashEntry, hashLen)
	for i := range a.hashes {
		e := raw[i*16:]
		a.hashes[i] = hashEntry{
			nameA:    binary.LittleEndian.Uint32(e),
			nameB:    binary.LittleEndian.Uint32(e[4:]),
			locale:   binary.LittleEndian.Uint16(e[8:]),
			platform: binary.LittleEndian.Uint16(e[10:]),
			block:    binary.LittleEndian.Uint32(e[12:]),
		}
	}

	raw, err = a.table(base+blockPos, blockLen, "(block table)")
	if err != nil {
		return nil, err
	}
	a.blocks = make([]blockEntry, blockLen)
	for i := range a.blocks {
		e := raw[i*16:]
		a.blocks[i] = blockEntry{
			pos:    binary.LittleEndian.Uint32(e),
			packed: binary.LittleEndian.Uint32(e[4:]),
			size:   binary.LittleEndian.Uint32(e[8:]),
			flags:  binary.LittleEndian.Uint32(e[12:]),
		}
	}
	return a, nil
}

// findHeader finds the MPQ header, which starts on a 512-byte boundary.
func findHeader(r io.ReaderAt) (int64, error) {
	magic := make([]byte, 4)
	for off := int64(0); off < 64<<20; off += 512 {
		if _, err := r.ReadAt(magic, off); err != nil {
			break
		}
		if string(magic) == "MPQ\x1a" {
			return off, nil
		}
	}
	return 0, errors.New("not an MPQ archive")
}

func (a *Archive) table(pos int64, n uint32, name string) ([]byte, error) {
	buf := make([]byte, int(n)*16)
	if _, err := a.r.ReadAt(buf, pos); err != nil {
		return nil, fmt.Errorf("reading %s: %w", name, err)
	}
	decrypt(buf, hashString(name, hashFileKey))
	return buf, nil
}

// slot is the hash-table slot naming a member, or -1.
func (a *Archive) slot(name string) int {
	if len(a.hashes) == 0 {
		return -1
	}
	mask := len(a.hashes) - 1
	na, nb := hashString(name, hashNameA), hashString(name, hashNameB)
	i := int(hashString(name, hashTableOffset)) & mask
	for range a.hashes {
		e := a.hashes[i]
		if e.block == emptySlot {
			return -1
		}
		if e.block != deletedSlot && e.nameA == na && e.nameB == nb {
			return i
		}
		i = (i + 1) & mask
	}
	return -1
}

// maxMember bounds a member Read will load.
const maxMember = 16 << 20

// Read reads a member. Only stored and zlib-compressed members are
// supported: enough for (attributes).
func (a *Archive) Read(name string) ([]byte, error) {
	s := a.slot(name)
	if s < 0 || int(a.hashes[s].block) >= len(a.blocks) {
		return nil, ErrNotFound
	}
	b := a.blocks[a.hashes[s].block]
	if b.size > maxMember || b.packed > maxMember {
		return nil, fmt.Errorf("%s is too large", name)
	}
	if b.flags&flagImplode != 0 {
		return nil, fmt.Errorf("%s: imploded members aren't supported", name)
	}

	raw := make([]byte, b.packed)
	if _, err := a.r.ReadAt(raw, a.base+int64(b.pos)); err != nil {
		return nil, err
	}

	var key uint32
	encrypted := b.flags&flagEncrypted != 0
	if encrypted {
		key = hashString(filepath.Base(strings.ReplaceAll(name, `\`, "/")), hashFileKey)
		if b.flags&flagFixKey != 0 {
			key = (key + b.pos) ^ b.size
		}
	}

	if b.flags&flagSingleUnit != 0 || b.flags&flagCompress == 0 && !encrypted {
		if encrypted {
			decrypt(raw, key)
		}
		return expand(raw, b.size, b.flags)
	}

	sectors := (b.size + a.sector - 1) / a.sector
	var offsets []uint32
	if b.flags&flagCompress != 0 {
		entries := sectors + 1
		if b.flags&flagSectorCRC != 0 {
			entries++
		}
		if int(entries)*4 > len(raw) {
			return nil, errors.New("bad sector table")
		}
		table := append([]byte{}, raw[:entries*4]...)
		if encrypted {
			decrypt(table, key-1)
		}
		for i := uint32(0); i <= sectors; i++ {
			offsets = append(offsets, binary.LittleEndian.Uint32(table[i*4:]))
		}
	} else {
		for i := uint32(0); i <= sectors; i++ {
			offsets = append(offsets, min(i*a.sector, b.packed))
		}
	}

	out := make([]byte, 0, b.size)
	for i := uint32(0); i < sectors; i++ {
		lo, hi := offsets[i], offsets[i+1]
		if lo > hi || int(hi) > len(raw) {
			return nil, errors.New("bad sector table")
		}
		chunk := append([]byte{}, raw[lo:hi]...)
		if encrypted {
			decrypt(chunk, key+i)
		}
		want := min(a.sector, b.size-i*a.sector)
		data, err := expand(chunk, want, b.flags)
		if err != nil {
			return nil, err
		}
		out = append(out, data...)
	}
	return out, nil
}

// expand undoes a sector's compression. A sector as large as its data is
// stored as it is.
func expand(chunk []byte, want, flags uint32) ([]byte, error) {
	if flags&flagCompress == 0 || uint32(len(chunk)) >= want {
		if uint32(len(chunk)) < want {
			return nil, errors.New("short sector")
		}
		return chunk[:want], nil
	}
	if len(chunk) == 0 {
		return nil, errors.New("empty sector")
	}
	if chunk[0] != compZlib {
		return nil, fmt.Errorf("unsupported compression 0x%02x", chunk[0])
	}
	zr, err := zlib.NewReader(bytes.NewReader(chunk[1:]))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	out, err := io.ReadAll(io.LimitReader(zr, int64(want)+1))
	if err != nil {
		return nil, err
	}
	if uint32(len(out)) != want {
		return nil, errors.New("sector decompressed to the wrong size")
	}
	return out, nil
}

// stormAttributes are the (attributes) columns a pre-1.14 Storm.dll reads:
// a CRC32 and a FILETIME per block.
const stormAttributes = 0x1 | 0x2

// ModernAttributes reports whether the archive's (attributes) is in the 1.14
// format, which a pre-1.14 Storm.dll can't read: it carries more than the
// CRC32 and FILETIME columns (an MD5 per block), and 1.14 compresses it with
// zlib, which that Storm can't decompress, so opening the archive fails
// with "The file data is corrupt". One the reader can't decode isn't 1.14's.
func (a *Archive) ModernAttributes() bool {
	data, err := a.Read("(attributes)")
	if err != nil || len(data) < 8 {
		return false
	}
	return binary.LittleEndian.Uint32(data[4:])&^stormAttributes != 0
}

// IsModern reports whether the archive at path has a 1.14 (attributes).
func IsModern(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	a, err := Open(f)
	if err != nil {
		return false, err
	}
	return a.ModernAttributes(), nil
}

// CopyWithoutAttributes copies the archive at src to dst with its
// (attributes) unlisted: its hash-table slot is marked deleted, so lookups
// still probe past it. Every other byte is unchanged, so the copy keeps its
// size and every member its offset. dst is written to a temporary file and
// renamed into place.
func CopyWithoutAttributes(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	a, err := Open(in)
	if err != nil {
		return err
	}
	s := a.slot("(attributes)")
	if s < 0 {
		return ErrNotFound
	}

	tmp := dst + ".launcher-copy"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			out.Close()
			os.Remove(tmp)
		}
	}()

	if _, err := in.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	a.hashes[s].block = deletedSlot
	table := make([]byte, len(a.hashes)*16)
	for i, e := range a.hashes {
		r := table[i*16:]
		binary.LittleEndian.PutUint32(r, e.nameA)
		binary.LittleEndian.PutUint32(r[4:], e.nameB)
		binary.LittleEndian.PutUint16(r[8:], e.locale)
		binary.LittleEndian.PutUint16(r[10:], e.platform)
		binary.LittleEndian.PutUint32(r[12:], e.block)
	}
	encrypt(table, hashString("(hash table)", hashFileKey))
	if _, err := out.WriteAt(table, a.hashPos); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	ok = true
	return os.Rename(tmp, dst)
}
