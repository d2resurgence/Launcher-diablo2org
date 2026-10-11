package cdkey

import (
	"strings"
	"testing"
)

func TestNormalise(t *testing.T) {
	tests := map[string]string{
		"  abcd-efgh-2467-89bc ": "ABCDEFGH246789BC",
		"ABCD EFGH\t2467 89BC":   "ABCDEFGH246789BC",
		"":                       "",
		// Anything else is left for the player to see: no validation.
		"o0-I1": "O0I1",
	}
	for in, want := range tests {
		if got := Normalise(in); got != want {
			t.Errorf("Normalise(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRedactGivenKeys(t *testing.T) {
	// Not shaped like a key at all, so only the explicit list catches it.
	const key = "AAAA1111AAAA1111"
	text := "stored aaaa-1111-aaaa-1111 and AAAA 1111 AAAA 1111 and " + key
	got := string(Redact([]byte(text), key))
	if strings.Contains(strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(got)), key) {
		t.Fatalf("key survived: %q", got)
	}
	if strings.Count(got, Redaction) != 3 {
		t.Fatalf("want 3 redactions, got %q", got)
	}
}

func TestRedactKeyShaped(t *testing.T) {
	tests := map[string]bool{
		"key BCDE2467FGHJ89KM here":            true,  // 16
		"key BCDE-2467-FGHJ-89KM here":         true,  // 16, grouped
		"key bcde2467fghj89kmnprt246789 here":  true,  // 26, any case
		"key BCDEFG-2467-FGHJKM-89KM-NPRTVW":   true,  // 26, grouped
		"word DEFENDERBECKETTBERG":             false, // no digits
		"hash 2467246724672467":                false, // no letters
		"short BCDE2467FGHJ89K":                false, // 15
		"long BCDE2467FGHJ89KMN":               false, // 17
		"sha 0123456789abcdef0123456789abcdef": false,
		`path C:\Users\Player\Diablo II\Save`:  false,
	}
	for in, redacted := range tests {
		got := string(Redact([]byte(in)))
		if strings.Contains(got, Redaction) != redacted {
			t.Errorf("Redact(%q) = %q, want redacted=%v", in, got, redacted)
		}
	}
}
