package wine

import (
	"slices"
	"strings"
)

// DefaultDebug turns off Wine's debug channels, which are noisy and slow.
const DefaultDebug = "-all"

// baseOverrides stops Wine offering to install Mono and Gecko: neither is
// needed by the game, and the prompts would appear on first run.
const baseOverrides = "mscoree,mshtml="

// Env is the environment every Wine call runs with: the parent's, minus any
// Wine variables of the player's own, plus the prefix and the launcher's
// settings. debug "" means DefaultDebug; overrides are added to the base
// DLL overrides, such as "ddraw=n,b".
func Env(parent []string, prefix, debug string, overrides ...string) []string {
	if debug == "" {
		debug = DefaultDebug
	}

	env := slices.DeleteFunc(slices.Clone(parent), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "WINEPREFIX", "WINEDEBUG", "WINEDLLOVERRIDES", "WINEARCH", "WINELOADER", "WINESERVER":
			return true
		}
		return false
	})

	dlls := append([]string{baseOverrides}, overrides...)
	return append(env,
		"WINEPREFIX="+prefix,
		"WINEDEBUG="+debug,
		"WINEDLLOVERRIDES="+strings.Join(dlls, ";"),
	)
}
