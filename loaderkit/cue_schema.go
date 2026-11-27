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

	"io/fs"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/load"
	specmodule "github.com/opencharly/spec"
	sdkschema "github.com/opencharly/spec/schema"
	"github.com/opencharly/spec/schemaconcat"

	"github.com/opencharly/spec/spec"
)

// cueKindDefs maps a kind name to its entity definition path within the compiled schema.
// Clause-D DATA (see the file comment): the nine entries are the former per-file
// charly/cue_kind_<name>.go registrations, moved here unchanged; kubevirt (the 6th deploy
// substrate) was added with its kind.
//
// `box` is an INTERNAL validation key, not a YAML kind keyword — `box:` merged into `candy:` in the
// EDGE-INHERIT cutover, but #Box remains the IMAGE def (the image arm of #CandyValue) and a candy:
// node carrying base:/from: is typed "box" for per-entity validation. cueKindDefs was never
// bijective with the kind keywords, so an internal key is fine.
var cueKindDefs = map[string]string{
	"box":        "#Box",
	"candy":      "#Candy",
	"check":      "#Check",
	"deploy":     "#Deploy",
	"kubernetes": "#ResolvedKubernetes",
	"kubevirt":   "#ResolvedKubeVirt",
	"local":      "#ResolvedLocal",
	"pod":        "#ResolvedPod",
	"vm":         "#ResolvedVm",
}

// `kubernetes` names #ResolvedKubernetes for the SAME reason `pod` names #ResolvedPod and
// `local` names #ResolvedLocal: spec#304 unit 5 moved the authored #Kubernetes body into the
// serving plugin's CUE module (opencharly/plugin-substrate#35), so the contract no longer
// declares it. The envelope is the contract's remaining declaration for the kind; its `raw`
// carries the full cluster model opaquely, and the consumer that decodes it is
// candy/plugin-k8sgen (the envelope's own note says so).
//
// `local` names #ResolvedLocal for the SAME reason `pod` names #ResolvedPod: spec#304 unit 4
// moved the authored #Local body into plugin-substrate's own CUE module
// (opencharly/plugin-substrate#34), so the contract no longer declares it. The envelope is
// the contract's remaining declaration for the kind, and its field set is the authored
// body's plus the resolved-only `raw` — the same bounded, deliberate choice described for
// `pod` below (an authored `raw:` is rejected by the value gate as `field not allowed`).
//
// `pod` names #ResolvedPod, NOT the authored #Pod: spec#304 unit 3 moved the authored body
// into plugin-substrate's own CUE module (opencharly/plugin-substrate#32), so the contract no
// longer declares #Pod. What the contract still declares for the kind — and what this table
// needs — is the resolve-to-envelope form, whose field set is exactly the authored body's plus
// the resolved-only `raw` (the pod value gate rejects an authored `raw:` as `field not
// allowed`, so the floor's one extra name cannot admit a document). Two consequences are
// deliberate and bounded: the floor for `pod` now carries `raw`, and `env_default`'s type
// differs between the authored `#StrVal` and the envelope's `string` — which would matter only
// to a CONCRETE unify, i.e. `ValidateEntityClosedCUE("pod", …)` / `ApplyCueDefaults("pod")`,
// and no caller in the tree passes "pod" to either. The alternative the sdk's own gate forbids
// is dropping the entry: `pod` is the live repro's kind (declared_fields_floor.go's header), so
// a nil floor would re-arm the unsafe value-shape scan sdk#221/#223/#225 fixed.

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

// kindDefFor resolves a kind's authored definition through the THREADED gates first, then the
// base schema. A gate carries the SERVED CUE source (the base module plus the owning plugin's
// files, concatenated exactly as they are served) and the definition path the plugin declared;
// compiling that source here is what lets a loader-side helper see a body that moved into a
// plugin, without a registry query and without a second private copy of the schema
// (PROGRAM/schema-ownership-rca-rdd.md §3 I2/I3/I4).
// compileGateSource loads a gate's served CUE source as a FILE OF THE SPEC SCHEMA PACKAGE in the
// same assembled module the base is composed from: the embedded schema, the module's own cue.mod
// declaration, and the gate file. Everything the source imports then resolves the way it does at
// build time, and a source that is genuinely invalid still fails loudly.
func compileGateSource(kind, source string) (cue.Value, error) {
	overlay := map[string]load.Source{}
	if err := overlayFSTree(overlay, sdkschema.FS, "schema"); err != nil {
		return cue.Value{}, fmt.Errorf("kind %q: read the embedded schema: %w", kind, err)
	}
	if err := overlayFSTree(overlay, specmodule.FS, "."); err != nil {
		return cue.Value{}, fmt.Errorf("kind %q: read the module declaration: %w", kind, err)
	}
	// The gate's file lives in its OWN package: a served schema that imports the spec schema
	// cannot sit INSIDE that package (CUE refuses the import cycle), so the gate is loaded from
	// `./gate` — which imports `schema` — and the def is looked up there.
	overlay["/gate/gate_"+gateFileSlug(kind)+".cue"] = load.FromString(gatePackage(source))
	insts := load.Instances([]string{"./gate"}, &load.Config{Dir: "/", Overlay: overlay})
	if len(insts) != 1 {
		return cue.Value{}, fmt.Errorf("kind %q: gate source loaded as %d instances, want 1", kind, len(insts))
	}
	if insts[0].Err != nil {
		return cue.Value{}, fmt.Errorf("kind %q: gate source does not compile: %w", kind, insts[0].Err)
	}
	v := cueSchemaCtx().BuildInstance(insts[0])
	if v.Err() != nil {
		return cue.Value{}, fmt.Errorf("kind %q: gate source does not compile: %w", kind, v.Err())
	}
	return v, nil
}

// gatePackage rewrites a served source's package clause to the gate's own package, so a schema
// file authored as `package schema` can be compiled beside (and importing) that package.
func gatePackage(source string) string {
	lines := strings.Split(source, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "package ") {
			lines[i] = "package gate"
			return strings.Join(lines, "\n")
		}
	}
	return "package gate\n" + source
}

// gateFileSlug renders a kind as a file-name component (no separators, no dots).
func gateFileSlug(kind string) string {
	var b strings.Builder
	for _, r := range kind {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// overlayFSTree copies every regular file of fsys into the overlay under /<dir>/<path>. CUE's
// overlay implies parent directories, so this creates no directory entry and touches no disk.
func overlayFSTree(overlay map[string]load.Source, fsys fs.FS, dir string) error {
	return fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		overlay["/"+dir+"/"+p] = load.FromBytes(data)
		return nil
	})
}

func kindDefFor(kind string, gates map[string]spec.KindGate) (cue.Value, error) {
	if g, ok := gates[kind]; ok && g.Def != "" {
		// A DECLARED gate is authoritative: if its served source cannot compile or its def is
		// missing, that is the error the caller must see. Falling back to the base here would
		// report "no CUE schema registered" for a body that HAS a declaration.
		if g.Source == "" {
			return cue.Value{}, fmt.Errorf("kind %q: gate declares no served source", kind)
		}
		// The source is compiled IN THE MODULE IT DECLARES — the embedded spec schema, its
		// cue.mod declaration, and the gate's own file in the same package. Prepending the
		// concatenated base to a bare CompileString was NOT enough: a served plugin schema may
		// import `github.com/opencharly/spec/schema`, and a string compile has no module context
		// to resolve that import (measured: "package ... imported but not defined").
		v, verr := compileGateSource(kind, g.Source)
		if verr != nil {
			return cue.Value{}, verr
		}
		d := v.LookupPath(cue.ParsePath(g.Def))
		if d.Err() != nil {
			return cue.Value{}, fmt.Errorf("kind %q: gate def %s not found in its served source: %w", kind, g.Def, d.Err())
		}
		return d, nil
	}
	// No declaration: the base module is the authority for a word the contract still owns.
	if d, ok := cueKindDef(kind); ok {
		return d, nil
	}
	return cue.Value{}, fmt.Errorf("no CUE schema registered for kind %q", kind)
}

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
