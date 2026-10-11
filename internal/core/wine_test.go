package core

import (
	"reflect"
	"testing"

	"github.com/diablo2org/launcher/internal/spec"
	"github.com/diablo2org/launcher/internal/store"
)

func rendererProfile() *spec.Profile {
	return &spec.Profile{Components: []spec.Component{
		{ID: "maphack", Kind: "bh", Default: "1.9.9", Versions: []spec.ComponentVersion{{ID: "1.9.9"}}},
		{ID: "hd", Default: "3.0", Versions: []spec.ComponentVersion{{ID: "3.0"}}, Conflicts: []string{"d2gl"}},
		{ID: "d2gl", Kind: "d2gl", Versions: []spec.ComponentVersion{{ID: "1.3.3"}, {ID: "1.3.2"}}, Conflicts: []string{"hd"}},
	}}
}

func TestEnabledPrefersD2GL(t *testing.T) {
	p := rendererProfile()
	tests := []struct {
		name   string
		chosen map[string]string
		prefer bool
		want   map[string]string
	}{
		{"native: defaults as they are", nil, false,
			map[string]string{"maphack": "1.9.9", "hd": "3.0"}},
		{"Wine: d2gl's newest, a default-only conflict off", nil, true,
			map[string]string{"maphack": "1.9.9", "d2gl": "1.3.3"}},
		{"Wine: the player turned d2gl off", map[string]string{"d2gl": ""}, true,
			map[string]string{"maphack": "1.9.9", "hd": "3.0"}},
		{"Wine: the player chose hd", map[string]string{"hd": "3.0"}, true,
			map[string]string{"maphack": "1.9.9", "hd": "3.0"}},
		{"Wine: the player's own d2gl version", map[string]string{"d2gl": "1.3.2", "hd": ""}, true,
			map[string]string{"maphack": "1.9.9", "d2gl": "1.3.2"}},
	}
	for _, tt := range tests {
		srv := &store.Server{Components: map[string]string{}}
		for k, v := range tt.chosen {
			srv.Components[k] = v
		}
		if got := enabled(p, srv, tt.prefer); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestWineStatusNative(t *testing.T) {
	h := newHarness(t)
	if s := h.m.WineStatus(t.Context()); s.Needed || s.Ready {
		t.Errorf("no Wine host, but %+v", s)
	}
	if err := h.m.SetupWine(t.Context(), nil); err != nil {
		t.Errorf("SetupWine with no Wine host: %v", err)
	}
}
