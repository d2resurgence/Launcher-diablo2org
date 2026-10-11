// Package cdkey tidies the CD keys a player types in and keeps them out of
// anything the launcher writes down. Keys aren't checked: nobody can check
// ownership any more, and the game starts without one.
package cdkey

import (
	"regexp"
	"strings"
	"unicode"
)

// alphabet is the characters Diablo II keys are written in.
const alphabet = "246789BCDEFGHJKMNPRTVWXZ"

// Normalise trims a key, upper-cases it and drops spaces and hyphens, the
// way keys are printed on the box and in the Blizzard account.
func Normalise(key string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToUpper(r)
	}, key)
}

// keyLike is a run of key characters, perhaps in hyphenated groups. Redact
// only replaces runs of 16 or 26 characters, the two lengths keys come in.
var keyLike = regexp.MustCompile(`(?i)\b[` + alphabet + `](?:-?[` + alphabet + `]){15,25}\b`)

// Redaction replaces a key wherever it is found.
const Redaction = "[CD key]"

// Redact replaces the given keys, however they're spaced or cased, and
// anything else shaped like a key: 16 or 26 key characters with both
// letters and digits, optionally in hyphenated groups.
func Redact(data []byte, keys ...string) []byte {
	for _, k := range keys {
		k = Normalise(k)
		if len(k) < 4 {
			// Too short to stand for anything but itself.
			continue
		}
		var b strings.Builder
		b.WriteString("(?i)")
		for i, r := range k {
			if i > 0 {
				b.WriteString(`[-\s]*`)
			}
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
		data = regexp.MustCompile(b.String()).ReplaceAllLiteral(data, []byte(Redaction))
	}

	return keyLike.ReplaceAllFunc(data, func(m []byte) []byte {
		if !looksLikeKey(m) {
			return m
		}
		return []byte(Redaction)
	})
}

func looksLikeKey(m []byte) bool {
	var n int
	var letter, digit bool
	for _, c := range m {
		switch {
		case c == '-':
			continue
		case c >= '0' && c <= '9':
			digit = true
		default:
			letter = true
		}
		n++
	}

	return (n == 16 || n == 26) && letter && digit
}
