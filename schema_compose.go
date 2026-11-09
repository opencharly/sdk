package sdk

// schema_compose.go — THE one compose for a plugin's served schema (sdk#490).
//
// A plugin's served schema used to be TEXT all the way down: `CompileString` on the
// concatenated `schema/*.cue` body, in a context with no CUE module in scope. That made
// a plugin unable to say what the whole cutover exists for — `import
// "github.com/opencharly/spec/schema"` — because every compile site refused the import
// before resolution was attempted ("package imported but not defined", or, for the host's
// N-units-joined path, "expected label or ':'" from N import clauses landing in one file).
//
// This file composes the schema the way the rest of the cutover already composes the base
// document schema (charly core's cue_schema.go): as ONE CUE MODULE INSTANCE, evaluated
// under a declared `language.version`, instead of as a package-less compilation unit. The
// module is the plugin's own and the dependency is `github.com/opencharly/spec`, served
// OFFLINE out of the spec Go module's embedded CUE module — no network, no module cache,
// no `cue.mod/pkg` vendoring, and no local `replace` (which the pinned modfile schema does
// not have).
//
// TWO entry points, ONE compose. `ComposeServedSchema` composes the plugin's schema FS (the
// serve side, `BuildCapabilitiesWithRequires`), and `ComposeServedText` composes the
// package-less body that reaches the host over the wire (`Capabilities.schema_cue`, the
// host gate). Both funnel into composeServedBody, so the two halves of the contract judge
// the same schema with the same code — which is the property sdk#490 asks for: a builtin
// and an out-of-process plugin, and the serve side and the host, agree byte for byte.
//
// THREE measured constraints decide the shape (sdk#490, measured live):
//   - the loader requires a dependency's source FS to implement `module.OSRootFS`, because
//     it maps an imported package to an absolute directory; an in-memory-only dep is
//     impossible, so spec's module is MATERIALIZED at a real path (a fixed, content-named
//     temp root — never the host tree) and served from there;
//   - `Registry.Fetch` is called with the MAJOR-SUFFIXED path (`github.com/opencharly/spec@v0`),
//     so the registry matches on the base path;
//   - the wire carries no module declaration (the proto has no field for one), so the host
//     composes under the DERIVED declaration — the same declaration a plugin that ships no
//     `cue.mod/module.cue` is composed under on the serve side. The derived declaration
//     depends only on the spec module's declared language version, which is why the two
//     entry points agree for every plugin shape.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"

	specmodule "github.com/opencharly/spec"
	"github.com/opencharly/spec/schema"
	"github.com/opencharly/spec/schemaconcat"
)

const (
	// specModulePath is the ONE module a plugin's schema may import offline: the contract
	// module. The registry serves exactly this path and refuses every other, so a plugin
	// that declares a dependency nobody can resolve fails loudly here instead of silently
	// resolving against a toolchain default.
	specModulePath = "github.com/opencharly/spec"

	// pluginModuleRoot is the VIRTUAL module root a plugin schema is composed at. Nothing is
	// read from the host filesystem: the plugin's `cue.mod/module.cue` and its single
	// synthesized schema file arrive through load.Config.Overlay (whose entries imply their
	// parent directories), and a FIXED path — never a temp dir — keeps every CUE diagnostic
	// deterministic and free of machine-specific paths.
	pluginModuleRoot = "/opencharly/plugin-schema"

	// pluginModuleFile is where CUE's loader requires a module declaration relative to the
	// module root, and the path a plugin's embedded FS carries it at when it ships one.
	pluginModuleFile = "cue.mod/module.cue"

	// pluginSchemaDir is the schema subdirectory inside the composed module — the same
	// "<dir>" the sdk already reads a plugin's schema from.
	pluginSchemaDir = "schema"

	// pluginPackage is the package name the synthesized schema file declares. A module
	// instance needs a package; the concatenated body is package-less by contract
	// (schemaconcat strips the clause), so the compose re-establishes it — which changes no
	// value, only the file's legality inside a package directory.
	pluginPackage = "plugin"

	// derivedPluginModulePath is the module path the DERIVED declaration names. It is internal
	// and never published or fetched; it exists so the instance HAS a module (and therefore a
	// declared language version) rather than evaluating under the toolchain's default.
	derivedPluginModulePath = "opencharly.local/plugin-schema"

	// derivedDepVersion is the version the derived declaration records for spec. It is not a
	// release selection: the registry serves the spec module the Go binary already links, so
	// the module graph is decided by the build, never by this string. The modfile schema
	// requires a version, and a pinned plugin may not name a digest here, so a zero version
	// is the honest placeholder — nothing resolves it against a registry index.
	derivedDepVersion = "v0.0.0"
)

// ComposeServedSchema composes a plugin's embedded schema as one CUE module instance: the
// concatenated `schema/*.cue` body of schemaFS under dir, evaluated inside the plugin's own
// module declaration when its FS ships `cue.mod/module.cue`, and under the derived
// declaration when it does not. ctx is the context the value is built in (a cue.Value is
// only valid inside the context that built it); nil gets a fresh one.
func ComposeServedSchema(ctx *cue.Context, schemaFS fs.FS, dir string) (cue.Value, error) {
	body, _, err := schemaconcat.ConcatSchema(schemaFS, dir, nil)
	if err != nil {
		return cue.Value{}, fmt.Errorf("plugin schema: %w", err)
	}
	decl, err := pluginModuleDecl(schemaFS)
	if err != nil {
		return cue.Value{}, err
	}
	return composeServedBody(ctx, body, decl)
}

// ComposeServedText composes the package-less body a plugin serves on
// `Capabilities.schema_cue` — the host's half of the contract, and the same source the
// serve side concatenates. It is the SAME compose ComposeServedSchema runs, under the
// DERIVED module declaration, because the wire carries no `cue.mod/module.cue`: the host
// cannot be told the plugin's declaration, so the declaration must be one both sides can
// derive. For a plugin that ships no declaration — every plugin today — the two entry
// points are byte-for-byte the same compose.
func ComposeServedText(ctx *cue.Context, body string) (cue.Value, error) {
	decl, err := derivedPluginModuleDecl()
	if err != nil {
		return cue.Value{}, err
	}
	return composeServedBody(ctx, body, decl)
}

// composeServedBody is the ONE compose: the package-less schema body plus the module
// declaration it is evaluated under, loaded as ./schema out of the virtual plugin module,
// with spec served by the offline registry.
func composeServedBody(ctx *cue.Context, body string, decl []byte) (cue.Value, error) {
	if ctx == nil {
		ctx = cuecontext.New()
	}
	loc, err := specModuleLoc()
	if err != nil {
		return cue.Value{}, err
	}
	overlay := map[string]load.Source{
		filepath.Join(pluginModuleRoot, filepath.FromSlash(pluginModuleFile)): load.FromBytes(decl),
		filepath.Join(pluginModuleRoot, pluginSchemaDir, "plugin.cue"):        load.FromBytes([]byte("package " + pluginPackage + "\n\n" + body)),
	}
	instances := load.Instances([]string{"./" + pluginSchemaDir}, &load.Config{
		Dir:      pluginModuleRoot,
		Overlay:  overlay,
		Registry: specRegistry{loc: loc},
	})
	if len(instances) != 1 {
		return cue.Value{}, fmt.Errorf("composing ./%s from a plugin module produced %d instances, want 1", pluginSchemaDir, len(instances))
	}
	inst := instances[0]
	if err := inst.Err; err != nil {
		return cue.Value{}, err
	}
	if inst.Module == "" || inst.ModuleFile == nil {
		return cue.Value{}, fmt.Errorf("the plugin's ./%s loaded OUTSIDE its CUE module (module %q): the module's language.version must govern evaluation", pluginSchemaDir, inst.Module)
	}
	v := ctx.BuildInstance(inst)
	return v, v.Err()
}

// pluginModuleDecl returns the declaration a plugin's schema is composed under. A plugin
// that ships `cue.mod/module.cue` in its embedded FS declares its own module — that is the
// file the per-consumer unit adds (opencharly/opencharly#950) — and it is used verbatim
// after ONE consistency check: its declared language version must be the spec module's.
// The check exists because the host composes the same source text under the derived
// declaration (the wire carries no declaration), so a plugin evaluated under a different
// language version would be judged by two different semantics on the two sides of the
// contract.
func pluginModuleDecl(schemaFS fs.FS) ([]byte, error) {
	data, err := fs.ReadFile(schemaFS, pluginModuleFile)
	if errors.Is(err, fs.ErrNotExist) {
		return derivedPluginModuleDecl()
	}
	if err != nil {
		return nil, fmt.Errorf("plugin schema FS: reading %s: %w", pluginModuleFile, err)
	}
	// ParseNonStrict is the parser the LOADER itself applies to a main module file, so a
	// declaration the loader accepts is never refused here on a formality it tolerates (a
	// dep written without its major suffix, for instance).
	mf, err := modfile.ParseNonStrict(data, pluginModuleFile)
	if err != nil {
		return nil, fmt.Errorf("plugin %s: %w", pluginModuleFile, err)
	}
	specMF, err := specModuleFile()
	if err != nil {
		return nil, err
	}
	if mf.Language == nil || mf.Language.Version != specMF.Language.Version {
		got := "<none>"
		if mf.Language != nil && mf.Language.Version != "" {
			got = mf.Language.Version
		}
		return nil, fmt.Errorf("plugin %s declares language.version %s, but the spec CUE module pins %s: the served schema is composed under the spec module's language version on BOTH sides of the contract (the wire carries no module declaration), so a plugin may not evaluate under a different one", pluginModuleFile, got, specMF.Language.Version)
	}
	return data, nil
}

// derivedPluginModuleDecl is the module declaration the compose supplies when a plugin
// ships none. It names one dependency — spec — because that is the only module the offline
// registry serves, and it pins the language version the spec module declares rather than
// the toolchain's default (a schema's `close()` + embedded-definition idiom depends on it).
//
// The dep key carries spec's major suffix (`@v0`), which is the canonical form CUE's own
// `cue mod` writes: a v0 module's key names its major, and the loader asks the registry for
// exactly that major-suffixed module.
func derivedPluginModuleDecl() ([]byte, error) {
	specMF, err := specModuleFile()
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("module: %q\n\nlanguage: {\n\tversion: %q\n}\n\ndeps: {\n\t%q: v: %q\n}\n",
		derivedPluginModulePath, specMF.Language.Version, specModulePath+"@v0", derivedDepVersion)), nil
}

// specModuleFile is the spec CUE module's own declaration, parsed from the copy its Go
// module embeds. It is the ONE source for the language version every composed plugin
// schema is evaluated under.
var specModuleFile = sync.OnceValues(func() (*modfile.File, error) {
	data, err := fs.ReadFile(specmodule.FS, pluginModuleFile)
	if err != nil {
		return nil, fmt.Errorf("reading the embedded spec CUE module declaration: %w", err)
	}
	// The spec module's declaration is authored in spec and parsed STRICTLY: a malformed
	// embedded declaration must fail loudly here, never be tolerated into a compose.
	mf, err := modfile.Parse(data, pluginModuleFile)
	if err != nil {
		return nil, fmt.Errorf("the embedded spec CUE module declaration: %w", err)
	}
	if mf.Language == nil || mf.Language.Version == "" {
		return nil, fmt.Errorf("the spec CUE module declares no language.version, so a composed plugin schema would silently evaluate under the toolchain's default semantics")
	}
	return mf, nil
})

// specRegistry is the OFFLINE module registry a plugin compose sees: it serves the spec
// module — the module the Go binary already links — and nothing else. There is no network
// call and no module-cache lookup; the whole dependency graph is the spec module, whose own
// declaration carries no deps.
type specRegistry struct {
	loc module.SourceLoc
}

// Fetch returns spec's materialized source. It matches on the BASE path because the loader
// asks with the major-suffixed path (`github.com/opencharly/spec@v0`).
func (r specRegistry) Fetch(_ context.Context, m module.Version) (module.SourceLoc, error) {
	if m.BasePath() != specModulePath {
		return module.SourceLoc{}, fmt.Errorf("no offline CUE module registry entry for %s: the compose serves %s only", m.Path(), specModulePath)
	}
	return r.loc, nil
}

// Requirements is the module graph of the served module: spec declares no deps, so there is
// nothing below it.
func (r specRegistry) Requirements(_ context.Context, m module.Version) ([]module.Version, error) {
	if m.BasePath() != specModulePath {
		return nil, fmt.Errorf("no offline CUE module registry entry for %s", m.Path())
	}
	return nil, nil
}

// ModuleVersions refuses every query: a compose inside a build resolves one version — the
// one the binary links — and there is no index to list.
func (r specRegistry) ModuleVersions(_ context.Context, mpath string) ([]string, error) {
	return nil, fmt.Errorf("no version index for CUE module %q: the compose serves exactly one version of %s", mpath, specModulePath)
}

// specModuleLoc is the materialized location of the spec CUE module, built at most once per
// process and shared by every compose.
var specModuleLoc = sync.OnceValues(materializeSpecModule)

// materializeSpecModule writes the spec CUE module — the `schema` package source and the
// `cue.mod/module.cue` declaration — to a real directory and returns it as a module source
// location.
//
// It MUST touch the filesystem: the loader maps an imported package to an absolute
// directory and therefore requires the dependency's FS to implement `module.OSRootFS` (an
// in-memory-only dep is refused), and the module is assembled from the two embedded trees
// the spec Go module ships rather than from any host checkout.
//
// The directory is CONTENT-NAMED (a digest of the embedded tree), so it is stable across
// processes and machines running the same build, every path in a diagnostic is
// deterministic, and a stale copy from an older spec can never be read: a changed tree has
// a different name. It is written once, through a private temp directory renamed into
// place, so two processes racing on it cannot observe a half-written module.
func materializeSpecModule() (module.SourceLoc, error) {
	files, digest, err := specModuleFiles()
	if err != nil {
		return module.SourceLoc{}, err
	}
	dir := filepath.Join(os.TempDir(), "opencharly-spec-cue-module-"+digest)
	if err := writeModuleTree(dir, files); err != nil {
		return module.SourceLoc{}, err
	}
	return module.SourceLoc{FS: module.OSDirFS(dir), Dir: "."}, nil
}

// specModuleFiles reads the embedded spec module into memory and hashes it. Both trees are
// WALKED rather than named file by file, so a file added to either side is carried without
// an edit here.
func specModuleFiles() (map[string][]byte, string, error) {
	files := map[string][]byte{}
	paths := make([]string, 0, 300)
	collect := func(fsys fs.FS, dir string) error {
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
			rel := filepath.Join(dir, filepath.FromSlash(p))
			files[rel] = data
			paths = append(paths, rel)
			return nil
		})
	}
	if err := collect(specmodule.FS, "."); err != nil {
		return nil, "", fmt.Errorf("reading the embedded spec CUE module declaration: %w", err)
	}
	if err := collect(schema.FS, pluginSchemaDir); err != nil {
		return nil, "", fmt.Errorf("reading the embedded spec schema: %w", err)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		// A length-prefixed path and a length-prefixed body: no two trees can hash alike by
		// moving a byte across the path/body boundary.
		fmt.Fprintf(h, "%d:%s\n", len(p), p)
		fmt.Fprintf(h, "%d:", len(files[p]))
		h.Write(files[p])
	}
	return files, hex.EncodeToString(h.Sum(nil))[:16], nil
}

// writeModuleTree ensures dir holds exactly files. A directory that already holds the same
// names is reused untouched; a fresh tree is written beside it and renamed into place, so a
// concurrent compose sees either no directory or a complete one.
func writeModuleTree(dir string, files map[string][]byte) error {
	if moduleTreeComplete(dir, files) {
		return nil
	}
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("materializing the spec CUE module: %w", err)
	}
	tmp, err := os.MkdirTemp(parent, ".spec-module-")
	if err != nil {
		return fmt.Errorf("materializing the spec CUE module: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	for rel, data := range files {
		dst := filepath.Join(tmp, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("materializing the spec CUE module: %w", err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return fmt.Errorf("materializing the spec CUE module: %w", err)
		}
	}
	if err := os.Rename(tmp, dir); err != nil {
		// Another process won the race and its tree is the same content-named tree; use it.
		if moduleTreeComplete(dir, files) {
			return nil
		}
		return fmt.Errorf("materializing the spec CUE module at %s: %w", dir, err)
	}
	return nil
}

// moduleTreeComplete reports whether dir already holds every file, with the same bytes.
func moduleTreeComplete(dir string, files map[string][]byte) bool {
	for rel, data := range files {
		got, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil || string(got) != string(data) {
			return false
		}
	}
	return true
}
