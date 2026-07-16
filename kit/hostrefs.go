package kit

import (
	"strings"

	"github.com/opencharly/sdk/spec"
)

// hostrefs.go — P12a follow-up: CloseHostCleanups, CollectHostRefs, and
// SplitHostKey relocated from charly/check_members.go. Pure over spec.Op
// (StringFields/PluginInput) + the already-kit-native TestVarRefs/
// CollectAnyStrings/HostVar — zero core state. charly core's callers
// (resolveHostVarsForChecks, resolveHostVars, check_cmd.go, check_runner_live.go)
// are themselves GATED (they drive live venue resolution), so they stay core
// and call kit.CloseHostCleanups / kit.CollectHostRefs / kit.SplitHostKey.

// CloseHostCleanups tears down any ssh -L forwards opened while resolving
// ${HOST:<member>} address variables. Safe to call on a nil/empty slice.
func CloseHostCleanups(cleanups []func()) {
	for _, c := range cleanups {
		if c != nil {
			c()
		}
	}
}

// CollectHostRefs returns the distinct ${HOST:<member>} variable keys referenced
// across every string field of every check (keys in the "NAME:arg" form used
// by ExpandTestVars).
func CollectHostRefs(checks []spec.Op) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		for _, key := range TestVarRefs(s) {
			name := key
			if before, _, ok := strings.Cut(key, ":"); ok {
				name = before
			}
			if name != HostVar {
				continue
			}
			if !seen[key] {
				seen[key] = true
				out = append(out, key)
			}
		}
	}
	for i := range checks {
		for _, p := range checks[i].StringFields() {
			if *p != "" {
				add(*p)
			}
		}
		// A plugin verb (http/addr/…) carries its authored fields in PluginInput, not
		// StringFields, so ${HOST:…} cross-member refs there (an http URL targeting a
		// sibling member) are collected here too — the map analogue of the StringFields scan.
		for _, s := range CollectAnyStrings(checks[i].PluginInput) {
			add(s)
		}
	}
	return out
}

// SplitHostKey splits a "HOST:web" / "HOST:web:8080" key into the variable name
// and the remaining argument(s) (everything after the FIRST colon).
func SplitHostKey(key string) (name, arg string, ok bool) {
	before, after, ok := strings.Cut(key, ":")
	if !ok {
		return key, "", false
	}
	return before, after, true
}
