package kit

import (
	"fmt"

	"github.com/opencharly/sdk/spec"
)

// plugin_primary.go — the plugin-verb PRIMARY-input registry (K4: relocated from
// charly/node_desugar.go — pure state, no project-loader dependency). The registry itself is
// whole-program state (every compiled-in plugin registers its primary at init, and the byte-gated
// prescan registers an external plugin's declared primary before parse), but it needs no loader
// access — only spec.AuthoringVerbs (already sdk-native) for the collision guard. charly core's two
// registration call sites (provider_registry.go, plugin_prescan.go) call kit.RegisterPluginPrimary
// directly (K3 ZERO-ALIASES — no alias file). The plan RESUGAR (the save-side desugar inverse) is
// data-driven and lives in sdk/deploykit's MarshalBundleNode (it threads the primaries D-fact from
// the resolved-project envelope, plugin-reachable — the former registry-driven kit.ResugarPlan was
// retired in the deploy_nodeform convergence).

// authoredOpFieldSet is the CUE-derived reserved #Op field set — a verb word colliding with one of
// these is rejected at registration (the sugar rule could never reach it). Recomputed here directly
// from spec.AuthoringVerbs rather than copied from charly's reserved_registry.go (R3 — one source).
var authoredOpFieldSet = func() map[string]bool {
	m := make(map[string]bool, len(spec.AuthoringVerbs))
	for _, w := range spec.AuthoringVerbs {
		m[w] = true
	}
	return m
}()

// PluginPrimaries maps a plugin verb word to its declared PRIMARY input field —
// the target of the scalar sugar shorthand (`file: /usr/bin/xterm` →
// plugin_input: {file: …}). Compiled-in plugins seed it at init via
// RegisterPluginPrimary (their capability manifest); the byte-gated prescan
// registers an external plugin's declared primary before parse.
var PluginPrimaries = map[string]string{
	// The 11 live-container verbs' scalar shorthand (`cdp: status`) must desugar
	// at PARSE time — before any out-of-process provider can connect and serve
	// its ProvidedCapability.Primary — so their shared `method` primary is a
	// FROZEN CONVENTION seeded here (the same determinism rationale as the
	// migrate hook's frozen table). A connected plugin's declared primary
	// re-registers the same value; a NEW external verb declares its primary in
	// its candy manifest's plugin.primary map (prescanned pre-parse) instead of
	// extending this table.
	"cdp": "method", "wl": "method", "dbus": "method", "vnc": "method",
	"mcp": "method", "record": "method", "spice": "method", "libvirt": "method",
	"kube": "method", "adb": "method", "appium": "method",
}

// RegisterPluginPrimary declares word's primary input field. A verb word that
// collides with an authored #Op field is rejected at registration — the sugar
// rule could never reach it (the field would classify as a builtin modifier).
func RegisterPluginPrimary(word, field string) error {
	if authoredOpFieldSet[word] {
		return fmt.Errorf("plugin verb word %q collides with an authored #Op field — pick a non-colliding word", word)
	}
	PluginPrimaries[word] = field
	return nil
}

// PluginPrimaryFor returns word's declared primary input field.
func PluginPrimaryFor(word string) (string, bool) {
	f, ok := PluginPrimaries[word]
	return f, ok
}

