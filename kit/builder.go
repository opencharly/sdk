package kit

// builder.go — the SINGLE shared implementation of the four detection-builders'
// DEPLOY-TIME IR shim (cargo / npm / pixi / aur), R3. Each builder is served by its OWN
// composable plugin candy (candy/plugin-builder-<word>), but all four dispatch the SAME two
// pure functions here, keyed by the builder word — so the per-builder Go logic lives in ONE
// place a leaf plugin module imports (this package depends only on the stdlib + charly/spec).
//
// A builder's BUILD-TIME multi-stage is resolved by kit.BuilderResolve (builder_resolve.go, C10);
// these functions are the deploy-time legs:
//
//   - BuilderCollectContext: the per-candy stage-context keys the host merges onto the base
//     ({layer,builder,home}) to form BuilderStep.RawStageContext. Behaviour-preserving copy of
//     the former in-proc CollectContext bodies (pixi → constant default env; aur → its section
//     packages + replaces; cargo/npm → none, refined host-side at install time).
//   - BuilderReverse: the teardown ops for a resolved stage context. The builder-specific
//     reverse-op KIND (pixi-env-remove / npm-uninstall-g / cargo-uninstall / package-remove) is
//     exactly the logic this externalization moves out-of-process. For aur the host fills the
//     package-remove UninstallCmd later (fillReverseUninstallCmds), so only Kind/Format/Targets/
//     Scope are named here.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/opencharly/spec/spec"
	toml "github.com/pelletier/go-toml/v2"
)

// BuilderCollectContext returns the builder-specific stage-context keys for `word` given the
// host-supplied candy descriptor. An unknown word returns nil (a custom candy builder with no
// plugin keeps base-only context — the host never invokes a plugin for it).
func BuilderCollectContext(word string, in spec.BuilderCollectInput) (map[string]any, error) {
	switch word {
	case "pixi":
		// Default pixi env name; a candy's pixi.toml [workspace]/[project] name overrides it,
		// which the host target reads at install time.
		return map[string]any{"env_name": "default"}, nil
	case "aur":
		ctx := map[string]any{}
		if len(in.Packages) > 0 {
			ctx["packages"] = in.Packages
		}
		return ctx, nil
	case "npm":
		// The globals the apply install puts on the target are package.json's dependencies
		// (`npm install -g name@ver`), so they are its teardown set.
		deps, err := npmDependencies(in.ManifestContent)
		if err != nil {
			return nil, fmt.Errorf("npm builder, candy %q: %w", in.Candy, err)
		}
		ctx := map[string]any{}
		if len(deps) > 0 {
			ctx["packages"] = deps
		}
		return ctx, nil
	case "cargo":
		// A LIBRARY crate's artifacts go to a directory that is a pure function of the candy
		// name; a BINARY crate is installed by `cargo install --path`, which registers the
		// crate's package name — the name `cargo uninstall` takes. Both legs are recorded: each
		// is idempotent on the other kind of crate.
		ctx := map[string]any{
			"lib_dir": CargoLibDir(in.Candy),
			"ld_conf": CargoLdConfPath(in.Candy),
		}
		name, err := cargoPackageName(in.ManifestContent)
		if err != nil {
			return nil, fmt.Errorf("cargo builder, candy %q: %w", in.Candy, err)
		}
		if name != "" {
			ctx["packages"] = []string{name}
		}
		return ctx, nil
	case "mise":
		// The apply install copies the manifest into $HOME and runs `mise install`, so its
		// teardown is the manifest's tools plus the copied manifest.
		tools, err := miseTools(in.Manifest, in.ManifestContent)
		if err != nil {
			return nil, fmt.Errorf("mise builder, candy %q: %w", in.Candy, err)
		}
		ctx := map[string]any{}
		if in.Manifest != "" {
			ctx["manifest"] = in.Manifest
		}
		if len(tools) > 0 {
			ctx["tools"] = tools
		}
		return ctx, nil
	}
	// An unknown word is LOUD: a builder the vocabulary names but this kit does not know would
	// otherwise record no teardown at all (opencharly/sdk#608).
	return nil, fmt.Errorf("kit.BuilderCollectContext: unknown builder word %q", word)
}

// BuilderReverse returns the teardown ops for `word` given its resolved stage context (the
// BuilderCollectContext output the host stored on the BuilderStep). An unknown word, or a
// context missing the keys a builder needs, returns nil (no teardown — the same best-effort the
// in-proc builders had).
func BuilderReverse(word string, in spec.BuilderReverseInput) []spec.ReverseOp {
	switch word {
	case "pixi":
		if env := builderCtxString(in.Context, "env_name"); env != "" {
			return []spec.ReverseOp{{
				Kind:    spec.ReverseOpPixiEnvRemove,
				Targets: []string{env},
				Scope:   spec.ScopeUser,
				Extra:   map[string]string{"layer": in.Candy},
			}}
		}
	case "npm":
		if pkgs := builderCtxStringSlice(in.Context, "packages"); len(pkgs) > 0 {
			return []spec.ReverseOp{{
				Kind:    spec.ReverseOpNpmUninstallG,
				Targets: pkgs,
				Scope:   spec.ScopeUser,
			}}
		}
	case "cargo":
		var ops []spec.ReverseOp
		if pkgs := builderCtxStringSlice(in.Context, "packages"); len(pkgs) > 0 {
			ops = append(ops, spec.ReverseOp{
				Kind:    spec.ReverseOpCargoUninstall,
				Targets: pkgs,
				Scope:   spec.ScopeUser,
			})
		}
		// The library leg: the artifact directory and the loader drop-in that makes it
		// visible are installed together, so they are removed together — leaving the
		// ld.so.conf.d entry behind would point the loader at a directory that no longer
		// exists.
		if dir := builderCtxString(in.Context, "lib_dir"); dir != "" {
			ops = append(ops, spec.ReverseOp{
				Kind:    spec.ReverseOpRmDirRecursive,
				Targets: []string{dir},
				Scope:   spec.ScopeSystem,
			})
		}
		if conf := builderCtxString(in.Context, "ld_conf"); conf != "" {
			ops = append(ops, spec.ReverseOp{
				Kind:    spec.ReverseOpRmFileSystem,
				Targets: []string{conf},
				Scope:   spec.ScopeSystem,
			})
		}
		return ops
	case "mise":
		var script []string
		if tools := builderCtxStringSlice(in.Context, "tools"); len(tools) > 0 {
			script = append(script, "mise uninstall --all "+strings.Join(shellQuoteAll(tools), " "))
		}
		if m := builderCtxString(in.Context, "manifest"); m != "" {
			script = append(script, `rm -f "$HOME"/`+shellQuoteAll([]string{m})[0])
		}
		if len(script) > 0 {
			return []spec.ReverseOp{{
				Kind:  spec.ReverseOpPluginScript,
				Scope: spec.ScopeUser,
				Extra: map[string]string{spec.ReverseOpPluginScriptKey: strings.Join(script, "\n")},
			}}
		}
	case "aur":
		// aur packages install into the host package DB; reverse is a package-remove (the host
		// renders UninstallCmd from the format's uninstall_template via fillReverseUninstallCmds).
		if pkgs := builderCtxStringSlice(in.Context, "packages"); len(pkgs) > 0 {
			return []spec.ReverseOp{{
				Kind:    spec.ReverseOpPackageRemove,
				Format:  "pac",
				Targets: pkgs,
				Scope:   spec.ScopeSystem,
			}}
		}
	}
	return nil
}

// builderCtxString reads a string value from a stage-context map (the JSON-decoded form, where
// values arrive as `any`). Empty when absent or not a string.
func builderCtxString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// builderCtxStringSlice reads a []string value from a stage-context map. Handles both a native
// []string and the JSON-decoded []any of strings (the form an OpReverse Context arrives in).
func builderCtxStringSlice(m map[string]any, key string) []string {
	if m == nil {
		return nil
	}
	switch v := m[key].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// npmDependencies returns package.json's dependency names, sorted — the globals the npm apply
// install puts on the target. Empty content is no manifest (nothing to tear down); unparseable
// content is an error, never an empty teardown.
func npmDependencies(content string) ([]string, error) {
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}
	var pj struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(content), &pj); err != nil {
		return nil, fmt.Errorf("parsing package.json: %w", err)
	}
	out := make([]string, 0, len(pj.Dependencies))
	for name := range pj.Dependencies {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// cargoPackageName returns Cargo.toml's [package].name (the name `cargo install --path`
// registers and `cargo uninstall` takes).
func cargoPackageName(content string) (string, error) {
	if strings.TrimSpace(content) == "" {
		return "", nil
	}
	var ct struct {
		Package struct {
			Name string `toml:"name"`
		} `toml:"package"`
	}
	if err := toml.Unmarshal([]byte(content), &ct); err != nil {
		return "", fmt.Errorf("parsing Cargo.toml: %w", err)
	}
	return ct.Package.Name, nil
}

// miseTools returns the tool names a mise manifest installs: mise.toml's [tools] keys, or the
// first field of each .tool-versions line. Sorted.
func miseTools(manifest, content string) ([]string, error) {
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}
	var out []string
	switch manifest {
	case ".tool-versions":
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			out = append(out, strings.Fields(line)[0])
		}
	default:
		var mt struct {
			Tools map[string]any `toml:"tools"`
		}
		if err := toml.Unmarshal([]byte(content), &mt); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", manifest, err)
		}
		for name := range mt.Tools {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// shellQuoteAll single-quotes each value for a POSIX shell.
func shellQuoteAll(in []string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
	}
	return out
}
