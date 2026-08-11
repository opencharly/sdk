package loaderkit

// cue_schema.go — the loader's OWN compiled CUE schema handle (K-wave 2, cone R1, ruling 1).
//
// Before this cutover the CUE-validate mechanism below took a `cs spec.CueSchema` parameter on
// every call: charly core owned the compiled schema (charly/cue_schema.go's cueSchemaCtx /
// sharedCueSchema / cueKindDefs) and threaded a handle through the ProjectLoader seam. That made
// the CUE schema — a per-kind DATA table plus a compiled schema value the loader is the only
// consumer of — a kernel possession, which the boundary law places with the capability that reads
// it (clause R): the loader validates config, so the loader owns the schema it validates against.
//
// So the schema is compiled HERE now, from the SAME single source every other consumer uses
// (spec/schema's embedded *.cue via the shared schemaconcat.ConcatSchema contract — R3, so the
// compiled schema can never drift from the generated Go types), and the six exported CUE-validate
// entry points take no schema parameter at all.
//
// The kind→def table below absorbs the nine former charly/cue_kind_<name>.go init() registrations
// verbatim. It is clause-D kind-recognition DATA — a name→def-path mapping consulted by word,
// never a per-kind code branch — and it belongs beside the schema it indexes into.
//
// LAZY by construction (sync.OnceValue): a full compile measures ~11ms over a ~398KB concatenated
// schema, so a command that never validates config (`charly version`) pays nothing. charly core
// keeps its own independently-compiled copy for the two mechanisms that genuinely need one — the
// plugin-schema splice (charly/plugin_loader.go) and the structural-kind value gate
// (charly/provider_kind_invoke.go's validateKindValueCUE) — and that copy is lazy too. The two
// never interoperate: a cue.Value is only valid within the cue.Context that built it, and no call
// path mixes a value from one context with a definition from the other.

import (
	"fmt"
	"sync"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	sdkschema "github.com/opencharly/spec/schema"
	"github.com/opencharly/spec/schemaconcat"
)

// cueKindDefs maps a kind name to its entity definition path within the compiled schema.
// Clause-D DATA (see the file comment): the nine entries are the former per-file
// charly/cue_kind_<name>.go registrations, moved here unchanged.
//
// `box` is an INTERNAL validation key, not a YAML kind keyword — `box:` merged into `candy:` in the
// EDGE-INHERIT cutover, but #Box remains the IMAGE def (the image arm of #CandyValue) and a candy:
// node carrying base:/from: is typed "box" for per-entity validation. cueKindDefs was never
// bijective with the kind keywords, so an internal key is fine.
var cueKindDefs = map[string]string{
	"android":    "#Android",
	"box":        "#Box",
	"candy":      "#Candy",
	"check":      "#Check",
	"deploy":     "#Deploy",
	"kubernetes": "#Kubernetes",
	"local":      "#Local",
	"pod":        "#Pod",
	"vm":         "#Vm",
}

// cueSchemaCtx is the loader's process-wide CUE context. Every ingest/build/Unify call in this
// package MUST use it, since cue.Value instances only interoperate within the context that built
// them.
var cueSchemaCtx = sync.OnceValue(func() *cue.Context { return cuecontext.New() })

// sharedCueSchema is every schema/*.cue file unified into one compiled value (the files carry no
// package clause, so they share one scope and the per-kind defs reference the shared #Step /
// #Context). Panics on a malformed or non-compiling schema: the schema is embedded in the binary,
// so a failure here is a build-time invariant violated, never a runtime input.
var sharedCueSchema = sync.OnceValue(func() cue.Value {
	body, _, err := schemaconcat.ConcatSchema(sdkschema.FS, ".", nil)
	if err != nil {
		panic(fmt.Sprintf("loaderkit: read embedded schema: %v", err))
	}
	v := cueSchemaCtx().CompileString(body)
	if v.Err() != nil {
		panic(fmt.Sprintf("loaderkit: CUE schema failed to compile: %v", errors.Details(v.Err(), nil)))
	}
	// Fail fast on a kind whose declared def is absent from the compiled schema — the same
	// invariant the former registerCueKind panicked on, checked once here for the whole table
	// instead of once per init().
	for kind, defPath := range cueKindDefs {
		if d := v.LookupPath(cue.ParsePath(defPath)); d.Err() != nil {
			panic(fmt.Sprintf("loaderkit: CUE kind %q: definition %s not found: %v", kind, defPath, d.Err()))
		}
	}
	return v
})

// cueKindDef returns the compiled entity definition for a kind, and whether the kind is known.
func cueKindDef(kind string) (cue.Value, bool) {
	dp, ok := cueKindDefs[kind]
	if !ok {
		return cue.Value{}, false
	}
	return sharedCueSchema().LookupPath(cue.ParsePath(dp)), true
}

// schemaDef looks a definition path up in the compiled schema, wrapping an absent def with label
// context (the shared shape of the former per-call `cs.Root.LookupPath` + Err() checks).
func schemaDef(label, defPath string) (cue.Value, error) {
	d := sharedCueSchema().LookupPath(cue.ParsePath(defPath))
	if d.Err() != nil {
		return cue.Value{}, fmt.Errorf("%s: %s schema not found: %w", label, defPath, d.Err())
	}
	return d, nil
}
