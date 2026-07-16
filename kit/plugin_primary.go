package kit

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/opencharly/sdk/spec"
)

// plugin_primary.go — the plugin-verb PRIMARY-input registry + the plan RESUGAR (K4: relocated
// from charly/node_desugar.go — pure state + yaml.Node transforms, no project-loader dependency).
// The registry itself is whole-program state (every compiled-in plugin registers its primary at
// init, and the byte-gated prescan registers an external plugin's declared primary before parse),
// but it needs no loader access — only spec.AuthoringVerbs (already sdk-native) for the collision
// guard. charly core's two registration call sites (provider_registry.go, plugin_prescan.go) call
// kit.RegisterPluginPrimary directly (K3 ZERO-ALIASES — no alias file); ResugarPlan is consumed by
// sdk/deploykit's marshalBundleNode AND candy/plugin-deploy-pod's deploy-state writer.

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

// ResugarPlan is the desugar's INVERSE, used by the deploy-state WRITER
// (sdk/deploykit's marshalBundleNode): each step's internal plugin/plugin_input pair rewrites
// back to the authored `<word>: <input>` sugar (collapsing a single-primary map
// to the scalar shorthand), so a written file round-trips through the
// parse-time desugar instead of tripping its authored-envelope ban.
func ResugarPlan(plan *yaml.Node) {
	if plan == nil || plan.Kind != yaml.SequenceNode {
		return
	}
	for _, st := range plan.Content {
		if st.Kind != yaml.MappingNode {
			continue
		}
		pluginIdx, inputIdx := -1, -1
		for i := 0; i+1 < len(st.Content); i += 2 {
			switch st.Content[i].Value {
			case "plugin":
				pluginIdx = i
			case "plugin_input":
				inputIdx = i
			}
		}
		if pluginIdx < 0 {
			continue
		}
		word := st.Content[pluginIdx+1].Value
		input := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		if inputIdx >= 0 {
			input = st.Content[inputIdx+1]
		}
		// scalar-collapse: input == {<primary>: <scalar>}
		if prim, ok := PluginPrimaryFor(word); ok && input.Kind == yaml.MappingNode &&
			len(input.Content) == 2 && input.Content[0].Value == prim &&
			input.Content[1].Kind == yaml.ScalarNode {
			input = input.Content[1]
		}
		nc := make([]*yaml.Node, 0, len(st.Content))
		for i := 0; i+1 < len(st.Content); i += 2 {
			switch i {
			case pluginIdx:
				nc = append(nc, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: word,
					HeadComment: st.Content[i].HeadComment}, input)
			case inputIdx:
				// dropped — folded into the sugar key's value
			default:
				nc = append(nc, st.Content[i], st.Content[i+1])
			}
		}
		st.Content = nc
	}
}
