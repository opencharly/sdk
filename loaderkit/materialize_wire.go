package loaderkit

import (
	"encoding/json"
	"strings"

	"github.com/opencharly/spec/spec"
)

// materialize_wire.go — the serialization contract for a materialized spec.UnifiedFile crossing the
// loader-materialize / loader-*-validate reverse legs (K1-LOADER RELOCATION, Unit B/D).
//
// TWO fields a plain json.Marshal would mishandle are carried explicitly:
//
//   - PluginKinds is tagged `json:"-"` ("Host-internal — never serialized"), so a plain marshal
//     SILENTLY DROPS every standalone-template + plugin-kind entity (uf.PluginKinds[disc][name] —
//     the map uf.VM()/Local()/Android()/Pod()/Kubernetes() and the check-bed / android / preempt
//     validators all read). Host-side that never matters (charly.LoadUnified mutates ONE
//     spec.UnifiedFile in place, no serialization); the PLUGIN-side witness (execLoaderExecutor)
//     round-trips the spec.UnifiedFile through HostBuild's []byte payload, so the maps are captured
//     by namespace PATH and re-attached after the round-trip.
//
//   - Namespaces is JSON-serialized (it carries only `yaml:"-"`), and the graph it holds is
//     legitimately CYCLIC: a mutual import (`main` imports `sub`, `sub` imports `main` — the case
//     UnifiedFile.collectBeds guards) and a local-path `import:` inside the same git working tree
//     both mount a back-edge. A plain marshal of the nested *UnifiedFile tree is then rejected by
//     encoding/json:
//
//     json: unsupported value: encountered a cycle via map[string]*spec.UnifiedFile
//
//     which made `charly box validate` / `charly task <name>` fail on ANY such project
//     (opencharly/opencharly#330). The namespace tree is therefore carried FLAT — one
//     materializedNamespace per PATH, each File a shallow copy with its OWN Namespaces cleared —
//     which terminates every cycle while preserving every path a finite walk reaches (the diamond
//     and multi-alias mounts included). UnmarshalMaterialized rebuilds the tree from the flat
//     levels; a genuine back-edge truncates, which is exactly the set collectBeds enumerates (it
//     returns the instant it re-enters a namespace already on the current path).
//
// This is a loaderkit-internal helper for the TRANSITIONAL materialize leg (dissolves when the
// materialize orchestration moves into loaderkit, #48); spec.UnifiedFile is itself a loaderkit
// hand-type, so its serialization envelope lives here too.

// pluginKindsByPath maps a namespace path ("" = root, "a", "a.b", …) → that level's PluginKinds
// (kind word → entity name → opaque canonical body).
type pluginKindsByPath = map[string]map[string]map[string]json.RawMessage

// materializedNamespace is ONE mounted namespace level, keyed by its PATH ("a", "a.b", …). File is a
// shallow copy WITH its own Namespaces cleared, so marshaling the envelope never recurses into the
// (possibly cyclic) graph.
type materializedNamespace struct {
	Path string            `json:"path"`
	File *spec.UnifiedFile `json:"file"`
}

// materializedEnvelope carries a spec.UnifiedFile plus the two things a plain json.Marshal cannot:
// the dropped PluginKinds maps (keyed by namespace path) and the FLATTENED namespace tree
// (cycle-safe). UF itself carries no Namespaces — they are the flat levels.
type materializedEnvelope struct {
	UF          *spec.UnifiedFile       `json:"uf"`
	Namespaces  []materializedNamespace `json:"namespaces,omitempty"`
	PluginKinds pluginKindsByPath       `json:"plugin_kinds,omitempty"`
}

// withoutNamespaces returns a shallow copy of uf with the nested Namespaces graph cleared, so the
// value is safe to marshal (the flat levels carry the tree instead). nil in → nil out.
func withoutNamespaces(uf *spec.UnifiedFile) *spec.UnifiedFile {
	if uf == nil {
		return nil
	}
	cp := *uf
	cp.Namespaces = nil
	return &cp
}

// captureNamespaceLevels walks the namespace graph with a PATH-SCOPED ancestor guard and records
// every reached level as {path, file-without-Namespaces}.
//
// Every node reachable by a path is recorded at that path (so a diamond / multi-alias mount keeps
// every alias), while a genuine BACK-EDGE — a namespace already on the current path — is skipped
// entirely: it contributes nothing to any finite walk, since collectBeds returns the instant it
// re-enters an ancestor. Recording a fabricated copy for the back-edge would instead make the
// reconstructed tree enumerate beds the source never exposes.
func captureNamespaceLevels(uf *spec.UnifiedFile, prefix string, out *[]materializedNamespace, ancestors map[*spec.UnifiedFile]bool) {
	if uf == nil || ancestors[uf] {
		return
	}
	ancestors[uf] = true
	defer delete(ancestors, uf)
	for name, ns := range uf.Namespaces {
		if ns == nil || ancestors[ns] {
			continue
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		*out = append(*out, materializedNamespace{Path: path, File: withoutNamespaces(ns)})
		captureNamespaceLevels(ns, path, out, ancestors)
	}
}

// attachNamespaceLevel installs file at its dot-separated path under root, creating intermediate
// nodes when needed. The capture walk is parents-first, so a level's parent normally already exists;
// the created-node branch keeps the rebuild order-independent, and an already-attached level's
// children are preserved.
func attachNamespaceLevel(root *spec.UnifiedFile, path string, file *spec.UnifiedFile) {
	if root == nil || file == nil || path == "" {
		return
	}
	segs := strings.Split(path, ".")
	cur := root
	for i, seg := range segs {
		if cur.Namespaces == nil {
			cur.Namespaces = map[string]*spec.UnifiedFile{}
		}
		if i == len(segs)-1 {
			cp := *file
			cp.Namespaces = nil
			if existing := cur.Namespaces[seg]; existing != nil {
				cp.Namespaces = existing.Namespaces
			}
			cur.Namespaces[seg] = &cp
			return
		}
		next := cur.Namespaces[seg]
		if next == nil {
			next = &spec.UnifiedFile{}
			cur.Namespaces[seg] = next
		}
		cur = next
	}
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
// PRESERVING PluginKinds (which a plain json.Marshal drops) and TERMINATING a cyclic namespace graph
// (which a plain marshal rejects). Use it on EVERY loader leg that sends a materialized
// spec.UnifiedFile across the wire (the loader-materialize reply + the loader-*-validate requests).
func MarshalMaterialized(uf *spec.UnifiedFile) ([]byte, error) {
	env := materializedEnvelope{UF: withoutNamespaces(uf), PluginKinds: pluginKindsByPath{}}
	capturePluginKinds(uf, "", env.PluginKinds)
	captureNamespaceLevels(uf, "", &env.Namespaces, map[*spec.UnifiedFile]bool{})
	return json.Marshal(env)
}

// UnmarshalMaterialized reconstructs a materialized spec.UnifiedFile from a MarshalMaterialized
// payload INTO uf, rebuilding the namespace tree from the flat levels and re-attaching PluginKinds
// at every level so the result matches the source at every reachable path. uf must be non-nil (the
// leg's own `merged`/scratch spec.UnifiedFile).
func UnmarshalMaterialized(data []byte, uf *spec.UnifiedFile) error {
	var env materializedEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	if env.UF != nil {
		*uf = *env.UF
		uf.Namespaces = nil
	}
	for _, lvl := range env.Namespaces {
		attachNamespaceLevel(uf, lvl.Path, lvl.File)
	}
	restorePluginKinds(uf, "", env.PluginKinds)
	return nil
}
