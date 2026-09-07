package loaderkit

// declared_fields_floor.go — the declared-fields CONTRACT FLOOR (the executor-boundary
// declared-fields channel, sdk #221/#223/#225 class).
//
// The declared-fields channels (spec.Threaded.StructuralDeclaredFields — spec#104 — and
// spec.Threaded.DeployDeclaredFields — spec#107) are HOST-process registry state: charly's
// loaderThreaded() snapshots each recognized word's declared input-schema body FIELD names
// from pluginSchemas.inputDefs, whose population is lazy (the builtin schema gate runs inside
// loadProjectPlugins, mid-load — and an external plugin's served schema registers only at its
// connect). A parse that consumes a Threaded BEFORE that gate — the executor loader legs
// (LoadUnifiedViaExecutor's "loader-walk"/"loader-threaded" host legs, shared by plugin-fleet
// deploy add, plugin-build build:project, and the deploy-target surfaces), the plugin-side
// Threaded consumers (plugin-box validate, plugin-build resolve), or any process whose load
// interleaves differently — receives EMPTY declared-fields maps, and the documented
// no-declared-schema fallback re-arms the unsafe value-shape scan: a converted tree's declared
// #Deploy fields re-scan as in-substrate members (the live repro: distro-arch
// check-agent-live's pod body — `iterate:` carrying an `agent:` kind-word key — failing
// `charly deploy add` with `node "sandbox": expected a mapping value, got yaml kind 8`,
// the exact regression sdk #223/#225 fixed for the populated host-side parse).
//
// The design ruling: the declared-fields maps are CONTRACT DATA — what each kind schema
// DECLARES — not host-registry state. For the spec-vocabulary kinds that declaration lives in
// the embedded spec/schema contract (the SAME single source the cue:gen Go types and charly's
// base schema splice derive from — R3), which this package ALREADY compiles lazily
// (cue_schema.go's sharedCueSchema + the cueKindDefs table). So the parse consults a CONTRACT
// FLOOR: a threaded entry (the served-schema channel, authoritative) always wins; a word the
// threaded map lacks floors to its contract-declared field set; a word the contract does not
// declare (an external plugin kind) floors to nil — the documented no-declared-schema fallback,
// unchanged. Result: a parse of a converted tree behaves IDENTICALLY at every placement and
// every registry-timing state — host-side or plugin-side, gate-run or pre-gate — because the
// tier-1 declared fields never depend on process lifecycle.

import (
	"sync"

	"cuelang.org/go/cue"
)

// contractDeclaredFields is lazily computed ONCE per process from the embedded contract:
// every cueKindDefs word → its declared body FIELD name set. For the five substrate words the
// authored body validates against the #<Kind>Value disjunction (#Pod | #DeployValue —
// spec/schema node.cue), so the declared set is the UNION of the kind def's fields and the
// #Deploy base's fields (the #DeployValue arm IS #Deploy minus the derived keys). A kind def
// absent from the compiled schema contributes nothing — the once-guard panics on a missing
// cueKindDefs def already (cue_schema.go), so the failure mode is a build-time invariant,
// never a runtime input.
var contractDeclaredFieldsSet = sync.OnceValue(func() map[string]map[string]bool {
	out := make(map[string]map[string]bool, len(cueKindDefs))
	for word, defPath := range cueKindDefs {
		fields := cueDefFields(defPath)
		d := sharedCueSchema().LookupPath(cue.ParsePath("#Deploy"))
		if d.Err() == nil {
			for f := range cueValueFields(d) {
				fields[f] = true
			}
		}
		out[word] = fields
	}
	return out
})

// contractDeclaredFields returns the CONTRACT-declared body field names for one kind word:
// the floor the parse consults when the threaded declared-fields map lacks the word. An
// unknown word (an external plugin kind the contract does not declare) returns nil — the
// documented no-declared-schema fallback stays exactly as it was.
func contractDeclaredFields(word string) map[string]bool {
	return contractDeclaredFieldsSet()[word]
}

// threadedDeclaredFields is the ONE declared-fields lookup the parse makes (R3 — the single
// consult site for both the substrate branch and the structural branch of parseNode): the
// threaded (served-schema) entry when present, else the contract floor.
func threadedDeclaredFields(m map[string]map[string]bool, disc string) map[string]bool {
	if fields, ok := m[disc]; ok {
		return fields // the served-schema channel is authoritative — presence wins
	}
	return contractDeclaredFields(disc)
}

// cueDefFields extracts one compiled definition's top-level FIELD names (the same
// optional-including, definitions-excluding iteration charly's registeredInputDefFields uses
// against the unified value — one extraction shape, R3).
func cueDefFields(defPath string) map[string]bool {
	d := sharedCueSchema().LookupPath(cue.ParsePath(defPath))
	if d.Err() != nil {
		return nil
	}
	return cueValueFields(d)
}

func cueValueFields(d cue.Value) map[string]bool {
	it, err := d.Fields(cue.Optional(true), cue.Definitions(false))
	if err != nil {
		return nil
	}
	fields := map[string]bool{}
	for it.Next() {
		fields[it.Selector().Unquoted()] = true
	}
	return fields
}
