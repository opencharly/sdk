package loaderkit

import (
	"encoding/json"

	"github.com/opencharly/spec/spec"
)

// materialize_wire.go — the serialization contract for a materialized spec.UnifiedFile crossing the
// loader-materialize / loader-*-validate reverse legs (K1-LOADER RELOCATION, Unit B/D). It exists
// because spec.UnifiedFile.PluginKinds is tagged `json:"-"` ("Host-internal — never serialized"): a plain
// json.Marshal of a spec.UnifiedFile SILENTLY DROPS every standalone-template + plugin-kind entity
// (uf.PluginKinds[disc][name] — the map uf.VM()/Local()/Android()/Pod()/K8s() and the check-bed /
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
	if uf == nil {
		return
	}
	if len(uf.PluginKinds) > 0 {
		out[prefix] = uf.PluginKinds
	}
	for name, ns := range uf.Namespaces {
		child := name
		if prefix != "" {
			child = prefix + "." + name
		}
		capturePluginKinds(ns, child, out)
	}
}

// restorePluginKinds re-attaches the captured PluginKinds maps at each namespace level by the SAME
// path capturePluginKinds used, so the reconstructed uf matches the source at every level.
func restorePluginKinds(uf *spec.UnifiedFile, prefix string, in pluginKindsByPath) {
	if uf == nil {
		return
	}
	if pk, ok := in[prefix]; ok {
		uf.PluginKinds = pk
	}
	for name, ns := range uf.Namespaces {
		child := name
		if prefix != "" {
			child = prefix + "." + name
		}
		restorePluginKinds(ns, child, in)
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
