package loaderkit

// cue_validate.go — the per-entity/per-document CUE-VALIDATE mechanism (K1 unit 2, relocated from
// charly/cue_schema.go + charly/cue_node.go + charly/cue_defaults.go): the closed-schema entity
// check (ValidateEntityClosedCUE), the load-time #NodeDoc structural gate (ValidateNodeDocCUE), the
// post-merge schema-defaults fill (ApplyCueDefaults), and the shared YAML→cue.Value ingest
// (CueDocFromYAML) all Unify against the HOST's process-wide compiled schema — cue.Value instances
// only interoperate within the cue.Context that built them, so (unlike DecodeEntityViaCUE's
// self-contained shorthand-decode, which owns an independent context) this mechanism never compiles
// its own copy: every call resolves the loader's OWN process-wide compiled schema (cue_schema.go —
// cueSchemaCtx/sharedCueSchema/cueKindDef, owned here since K-wave 2 cone R1 ruling 1, when the
// former host-threaded `cs spec.CueSchema` parameter was dropped from every entry point below).

import (
	"fmt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	cueyaml "cuelang.org/go/encoding/yaml"
	"gopkg.in/yaml.v3"
)

// docDirectiveSet (the reserved document-directive word set, #NodeDoc top-level keys) is already
// defined in parse.go — the SAME fixed vocabulary constant (spec.DocDirectives), reused here (R3).

// CueDocFromYAML ingests one YAML document into a cue.Value (the whole doc), built with the
// loader's own cue.Context so the result can Unify against the compiled schema's definitions.
func CueDocFromYAML(path string, data []byte) (cue.Value, error) {
	af, err := cueyaml.Extract(path, data)
	if err != nil {
		return cue.Value{}, fmt.Errorf("%s: yaml ingest: %w", path, err)
	}
	v := cueSchemaCtx().BuildFile(af)
	if v.Err() != nil {
		return cue.Value{}, fmt.Errorf("%s: build: %w", path, v.Err())
	}
	return v, nil
}

// ValidateEntityClosedCUE unifies a single entity with #<Kind> and validates it WITHOUT requiring
// concreteness — it catches closedness violations (unknown keys) and type/enum/regex conflicts, but
// not missing-required fields. This is the LOAD-time check (restores the deleted unmarshalers'
// typo-detection), AND (since c9befd83) the sole remaining `charly box validate` entity-schema
// gate: its former sibling validateEntityCUE (concrete-required) was a dead-code-radical-removal-
// batch deletion — every kind this project's schemas currently model has no meaningfully-required
// field concreteness would catch beyond what closedness already does (verified against
// #Box/#Builder: every field is optional or carries a default), and the modern load-time
// plugin-kind gate (RDD-verified live: `plugin kind:<X>: plugin_input fails #<X>Input`) is the
// actual production entity-schema enforcement path today, superseding the legacy per-kind Go-side
// validateVocabularyCollections/validateEntityCUE pair (also deleted) for every kind beyond box.
func ValidateEntityClosedCUE(kind, label string, entity cue.Value) error {
	def, ok := cueKindDef(kind)
	if !ok {
		return fmt.Errorf("%s: no CUE schema registered for kind %q", label, kind)
	}
	if err := entity.Unify(def).Validate(); err != nil {
		return fmt.Errorf("%s: %s", label, errors.Details(err, nil))
	}
	return nil
}

// ValidateEntityCUE is ValidateEntityClosedCUE's CONCRETE twin: it unifies a single entity with
// #<Kind> and validates it requiring concreteness, so it catches everything the closed check does
// PLUS missing-required fields and unresolved disjunctions (a PCI hostdev with no slot/function, a
// vm source: arm missing its discriminator-required field). charly core's own load-time
// validateKindValueCUE is the closedness-only gate for the #<Kind>Value-typed kinds; this is the
// stricter form the schema-tightening corpus asserts the schema still enforces, so that a future
// re-loosening of any modeled subtree fails loudly instead of silently accepting a broken entity.
func ValidateEntityCUE(kind, label string, entity cue.Value) error {
	def, ok := cueKindDef(kind)
	if !ok {
		return fmt.Errorf("%s: no CUE schema registered for kind %q", label, kind)
	}
	if err := entity.Unify(def).Validate(cue.Concrete(true)); err != nil {
		return fmt.Errorf("%s: %s", label, errors.Details(err, nil))
	}
	return nil
}

// ValidateNodeDocCUE validates a unified node-form document (raw YAML bytes) by unifying EACH
// top-level entity node against #Node (the reserved document directives are skipped — the loader
// decodes those). It runs the CLOSEDNESS check (no cue.Concrete): it catches a typo'd
// discriminator, an unknown field in a kind-value, a wrong-kind child, and a child under a
// childless kind — but does NOT require every entity's required fields (that stays the concrete
// `charly box validate` gate). label identifies the document in errors.
//
// Validation is PER ENTITY, not whole-document: unifying the whole document against a closed
// #NodeDoc forced CUE to resolve the per-child kind-disjunction across every entity at once — an
// O(entities × kinds × children) blow-up (a full-graph validate took ~30 CPU-minutes). One small
// entity at a time keeps each unification bounded by that entity's own size while preserving
// identical strictness.
func ValidateNodeDocCUE(label string, data []byte) error {
	doc, err := CueDocFromYAML(label, data)
	if err != nil {
		return err
	}
	docDef, err := schemaDef(label, "#NodeDoc")
	if err != nil {
		return err
	}
	iter, ierr := doc.Fields()
	if ierr != nil {
		return fmt.Errorf("%s: %w", label, ierr)
	}
	for iter.Next() {
		name := iter.Selector().Unquoted()
		if docDirectiveSet[name] {
			continue // version/repo/import/discover/defaults/provides — not entities
		}
		// Validate ONE entity through #NodeDoc's pattern constraint (`{[!~dir]: #Node}`) via
		// FillPath: this is the SAME lazy, closedness-only evaluation the whole-document Unify
		// used (so the #DataChild|#StepChild disjunction stays lazy — no spurious "incomplete
		// value" on an env/var child whose key also exists on a step), but bounded to this single
		// entity for speed.
		filled := docDef.FillPath(cue.MakePath(cue.Str(name)), iter.Value())
		if err := filled.Validate(); err != nil {
			return fmt.Errorf("%s: node %q: %s", label, name, errors.Details(err, nil))
		}
	}
	return nil
}

// ApplyCueDefaults fills schema-declared defaults into an already-RESOLVED entity by unifying its
// marshaled form with #<Kind> and decoding back. It is the unify-AFTER-merge counterpart to the
// loader's decode (which deliberately does NOT unify, so merge/inheritance see unset-as-zero): run
// this only at the point an entity is finalized for use, never at load.
//
// Only REQUIRED-with-default schema fields materialize — an optional-with-default field (`field?:
// *x`) stays absent on unify and does not reach the struct, so a value the caller never set for
// such a field is unaffected. A field already carrying a value is preserved (unify keeps the
// concrete value; the default only fills the gap). The canonical use is `firmware: *"bios"` in
// schema/vm.cue, which is required-with-default precisely so it materializes.
//
// Because it round-trips through the CLOSED #<Kind> schema, the entity must already validate
// against it (it does — the loader validated it). The round-trip is lossless for every modeled
// field; see cue_defaults_test.go (charly).
func ApplyCueDefaults(kind string, out any) error {
	def, ok := cueKindDef(kind)
	if !ok {
		return fmt.Errorf("applyCueDefaults: no CUE schema registered for kind %q", kind)
	}
	b, err := yaml.Marshal(out)
	if err != nil {
		return fmt.Errorf("applyCueDefaults %s: marshal: %w", kind, err)
	}
	af, err := cueyaml.Extract("defaults", b)
	if err != nil {
		return fmt.Errorf("applyCueDefaults %s: cue ingest: %w", kind, err)
	}
	cv := cueSchemaCtx().BuildFile(af)
	if cv.Err() != nil {
		return fmt.Errorf("applyCueDefaults %s: cue build: %w", kind, cv.Err())
	}
	merged := cv.Unify(def)
	if merged.Err() != nil {
		return fmt.Errorf("applyCueDefaults %s: unify with #%s: %w", kind, kind, merged.Err())
	}
	if err := merged.Decode(out); err != nil {
		return fmt.Errorf("applyCueDefaults %s: decode: %w", kind, err)
	}
	return nil
}
