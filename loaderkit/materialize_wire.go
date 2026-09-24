package loaderkit

import (
	"encoding/json"

	"github.com/opencharly/spec/spec"
)

// materialize_wire.go — the serialization contract for a materialized spec.UnifiedFile crossing the
// loader-materialize / loader-*-validate reverse legs (K1-LOADER RELOCATION, Unit B/D). It exists
// because spec.UnifiedFile.PluginKinds is tagged `json:"-"` ("Host-internal — never serialized"): a plain
// json.Marshal of a spec.UnifiedFile SILENTLY DROPS every standalone-template + plugin-kind entity
// (uf.PluginKinds[disc][name] — the map uf.VM()/Local()/Android()/Pod()/Kubernetes() and the check-bed /
// android / preempt validators all read). Host-side that never matters (charly.LoadUnified mutates
// ONE spec.UnifiedFile in place, no serialization). But the PLUGIN-side witness (execLoaderExecutor) round-
// trips the spec.UnifiedFile through HostBuild's []byte payload, so without this helper the plugin-side uf
// loses PluginKinds and a kind:check bed like `from: <local-template>` false-fails "not defined"
// (the R10 bed regression this fixes). Namespaces + RootDir already round-trip (they carry only
// `yaml:"-"`, not `json:"-"`), so ONLY PluginKinds must be carried alongside — captured recursively
// (root + every mounted namespace) so a namespaced entity survives too, keeping the plugin-side
// reconstruction BYTE-IDENTICAL to host-side. This is a loaderkit-internal helper for the
// TRANSITIONAL materialize leg (dissolves when the materialize orchestration moves into loaderkit,
// #48); spec.UnifiedFile is itself a loaderkit hand-type, so its serialization envelope lives here too.

// pluginKindsByPath maps a namespace path ("" = root, "a", "a.b", …) → that level's PluginKinds
// (kind word → entity name → opaque canonical body).
type pluginKindsByPath = map[string]map[string]map[string]json.RawMessage

// materializedEnvelope carries a spec.UnifiedFile plus the PluginKinds maps a plain json.Marshal drops.
// UF round-trips everything except PluginKinds (Namespaces + RootDir survive via their default
// json field names); PluginKinds carries the dropped maps keyed by namespace path.
type materializedEnvelope struct {
	UF          *spec.UnifiedFile `json:"uf"`
	PluginKinds pluginKindsByPath `json:"plugin_kinds,omitempty"`
}

// capturePluginKinds walks uf + its mounted namespaces, recording each level's PluginKinds under its
// namespace path so the drop-on-marshal maps can be re-attached after the round-trip.
func capturePluginKinds(uf *spec.UnifiedFile, prefix string, out pluginKindsByPath) {
	capturePluginKindsSeen(uf, prefix, out, map[*spec.UnifiedFile]bool{})
}

// capturePluginKindsSeen walks the namespace tree with a PATH-SCOPED ancestor guard.
//
// The guard MUST be path-scoped, not a global pointer-identity set: the SAME *UnifiedFile is mounted
// at MULTIPLE namespace paths by design — a diamond import (`c` imported by both `a` and `b`, so
// mounted at `a.c` AND `b.c` as ONE pointer via the REFERENCE-mount pointer identity) and a
// multi-alias mount (one repo at `arch` + `cachyos.arch`). A global `seen[uf]` guard recorded the
// maps at only the FIRST alias reachable in map-iteration order (nondeterministic), so every later
// alias restored WITH EMPTY PluginKinds — a namespaced bed (`b.c.check-*`, `from: vm`) then
// false-failed "not defined". The ancestor stack records the maps at EVERY path while still
// terminating a genuine cycle (a namespace that contains itself up the stack, e.g. main<->sub).
func capturePluginKindsSeen(uf *spec.UnifiedFile, prefix string, out pluginKindsByPath, ancestors map[*spec.UnifiedFile]bool) {
	if uf == nil || ancestors[uf] {
		return
	}
	ancestors[uf] = true
	defer delete(ancestors, uf)
	if len(uf.PluginKinds) > 0 {
		out[prefix] = uf.PluginKinds
	}
	for name, ns := range uf.Namespaces {
		child := name
		if prefix != "" {
			child = prefix + "." + name
		}
		capturePluginKindsSeen(ns, child, out, ancestors)
	}
}

// restorePluginKinds re-attaches the captured PluginKinds maps at each namespace level by the SAME
// path capturePluginKinds used, so the reconstructed uf matches the source at every level.
func restorePluginKinds(uf *spec.UnifiedFile, prefix string, in pluginKindsByPath) {
	restorePluginKindsSeen(uf, prefix, in, map[*spec.UnifiedFile]bool{})
}

func restorePluginKindsSeen(uf *spec.UnifiedFile, prefix string, in pluginKindsByPath, ancestors map[*spec.UnifiedFile]bool) {
	if uf == nil || ancestors[uf] {
		return
	}
	ancestors[uf] = true
	defer delete(ancestors, uf)
	if pk, ok := in[prefix]; ok {
		uf.PluginKinds = pk
	}
	for name, ns := range uf.Namespaces {
		child := name
		if prefix != "" {
			child = prefix + "." + name
		}
		restorePluginKindsSeen(ns, child, in, ancestors)
	}
}

// MarshalMaterialized serializes a materialized spec.UnifiedFile for a reverse-leg []byte payload,
// PRESERVING PluginKinds (which a plain json.Marshal drops). Use it on EVERY loader leg that sends a
// materialized spec.UnifiedFile across the wire (the loader-materialize reply + the loader-*-validate
// requests).
func MarshalMaterialized(uf *spec.UnifiedFile) ([]byte, error) {
	env := materializedEnvelope{UF: uf, PluginKinds: pluginKindsByPath{}}
	capturePluginKinds(uf, "", env.PluginKinds)
	return json.Marshal(env)
}

// UnmarshalMaterialized reconstructs a materialized spec.UnifiedFile from a MarshalMaterialized payload
// INTO uf, re-attaching PluginKinds at every namespace level so the result is byte-identical to the
// source. uf must be non-nil (the leg's own `merged`/scratch spec.UnifiedFile).
func UnmarshalMaterialized(data []byte, uf *spec.UnifiedFile) error {
	var env materializedEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	if env.UF != nil {
		*uf = *env.UF
	}
	restorePluginKinds(uf, "", env.PluginKinds)
	return nil
}
