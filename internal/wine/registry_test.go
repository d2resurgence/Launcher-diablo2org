package wine

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/diablo2org/launcher/internal/launch"
)

var gateways = []string{"1001", "02", "evnt.slashdiablo.net", "-6", "SlashDiablo", "play.slashdiablo.net", "-6", "Slash Diablo"}

func TestRegRoundTrip(t *testing.T) {
	save := `Z:\Users\Élodie\Diablo II\slashdiablo\Save\`
	data := encodeReg(map[string][]namedValue{
		battleNetKey: {{name: gatewayValue, value: regValue{multi: gateways, isMul: true}}},
		diabloKey: {
			{name: "Save Path", value: regValue{sz: save}},
			{name: "BNETIP", value: regValue{sz: "play.slashdiablo.net"}},
		},
	})

	if !bytes.HasPrefix(data, []byte{0xff, 0xfe}) {
		t.Fatal("no UTF-16 byte order mark")
	}
	text, _ := decodeUTF16(data)
	if !strings.HasPrefix(text, "Windows Registry Editor Version 5.00\r\n") {
		t.Errorf("header: %q", text[:40])
	}

	bnet, err := parseExport(data, battleNetKey)
	if err != nil {
		t.Fatal(err)
	}
	if got := bnet[strings.ToLower(gatewayValue)]; !got.isMul || !slices.Equal(got.multi, gateways) {
		t.Errorf("gateways = %q", got.multi)
	}

	d2, err := parseExport(data, diabloKey)
	if err != nil {
		t.Fatal(err)
	}
	if d2["save path"].sz != save || d2["bnetip"].sz != "play.slashdiablo.net" {
		t.Errorf("values = %+v", d2)
	}
	// Another key's values aren't mixed in.
	if _, ok := bnet["save path"]; ok {
		t.Error("read a value from the wrong key")
	}
}

func TestRegEncodingMultiSZ(t *testing.T) {
	// hex(7) is UTF-16LE strings, each NUL-terminated, then an empty one.
	data := encodeReg(map[string][]namedValue{battleNetKey: {{name: "x", value: regValue{multi: []string{"a", "bc"}, isMul: true}}}})
	text, _ := decodeUTF16(data)
	if !strings.Contains(text, `"x"=hex(7):61,00,00,00,62,00,63,00,00,00,00,00`) {
		t.Errorf("encoded:\n%s", text)
	}

	// A key with no values is listed, which is how load creates it.
	empty := encodeReg(map[string][]namedValue{diabloKey: nil})
	text, _ = decodeUTF16(empty)
	if !strings.Contains(text, "["+diabloKey+"]") {
		t.Errorf("empty key missing:\n%s", text)
	}
}

// An export as Wine 11.0's regedit /E writes it.
const wineExport = "Windows Registry Editor Version 5.00\r\n\r\n" +
	"[HKEY_CURRENT_USER\\Software\\Blizzard Entertainment\\Diablo II]\r\n" +
	"\"Save Path\"=\"Z:\\\\Users\\\\Élodie\\\\Diablo II\\\\slashdiablo\\\\Save\\\\\"\r\n" +
	"\"Resolution\"=dword:00000001\r\n" +
	"\"Quote\"=\"say \\\"hi\\\"\"\r\n\r\n" +
	"[HKEY_CURRENT_USER\\Software\\Blizzard Entertainment\\Diablo II\\VideoConfig]\r\n" +
	"\"Save Path\"=\"elsewhere\"\r\n\r\n" +
	"[HKEY_CURRENT_USER\\Software\\Battle.net\\Configuration]\r\n" +
	"\"Diablo II Battle.net Gateways\"=hex(7):31,00,30,00,30,00,31,00,00,00,30,00,32,\\\r\n" +
	"  00,00,00,65,00,76,00,6e,00,74,00,2e,00,73,00,6c,00,61,00,73,00,68,00,64,00,\\\r\n" +
	"  69,00,61,00,62,00,6c,00,6f,00,2e,00,6e,00,65,00,74,00,00,00,2d,00,36,00,00,\\\r\n" +
	"  00,53,00,6c,00,61,00,73,00,68,00,44,00,69,00,61,00,62,00,6c,00,6f,00,00,00,\\\r\n" +
	"  00,00\r\n"

func TestParseWineExport(t *testing.T) {
	data := append([]byte{0xff, 0xfe}, utf16le(wineExport)...)

	d2, err := parseExport(data, diabloKey)
	if err != nil {
		t.Fatal(err)
	}
	if got := d2["save path"].sz; got != `Z:\Users\Élodie\Diablo II\slashdiablo\Save\` {
		t.Errorf("Save Path = %q", got)
	}
	if got := d2["quote"].sz; got != `say "hi"` {
		t.Errorf("Quote = %q", got)
	}
	if _, ok := d2["resolution"]; ok {
		t.Error("a dword was read as a string")
	}

	bnet, err := parseExport(data, battleNetKey)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1001", "02", "evnt.slashdiablo.net", "-6", "SlashDiablo"}
	if got := bnet[strings.ToLower(gatewayValue)].multi; !slices.Equal(got, want) {
		t.Errorf("gateways = %q", got)
	}

	if _, err := parseExport([]byte(wineExport), diabloKey); err == nil {
		t.Error("parsed a file that isn't UTF-16")
	}
}

func TestStartArgs(t *testing.T) {
	tests := []struct {
		box       launch.Box
		args      []string
		overrides []string
	}{
		{launch.Box{Args: []string{"-skiptobnet"}}, []string{"-skiptobnet", "-w"}, nil},
		{launch.Box{Args: []string{"-w"}}, []string{"-w"}, nil},
		{launch.Box{Args: []string{"-3dfx"}, D2GL: true}, []string{"-3dfx"}, nil},
		{launch.Box{Args: []string{"-skiptobnet"}, D2GL: true}, []string{"-skiptobnet"}, []string{"ddraw=n,b"}},
	}
	for _, tt := range tests {
		args, overrides := startArgs(tt.box)
		if !slices.Equal(args, tt.args) || !slices.Equal(overrides, tt.overrides) {
			t.Errorf("%+v: args %q overrides %q, want %q %q", tt.box, args, overrides, tt.args, tt.overrides)
		}
	}
}
