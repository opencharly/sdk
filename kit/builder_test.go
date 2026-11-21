package kit

import (
	"reflect"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestBuilderCollectContext covers the per-builder deploy-time stage-context derivation that the
// four externalized detection-builder plugins (cargo/npm/pixi/aur) serve via OpCollectContext.
func TestBuilderCollectContext(t *testing.T) {
	cases := []struct {
		word string
		in   spec.BuilderCollectInput
		want map[string]any
	}{
		{"pixi", spec.BuilderCollectInput{Candy: "jupyter"}, map[string]any{"env_name": "default"}},
		// npm's teardown set is package.json's dependencies — the globals the apply install adds.
		{"npm", spec.BuilderCollectInput{Candy: "claude-code", Manifest: "package.json",
			ManifestContent: `{"dependencies": {"@anthropic-ai/claude-code": "*", "aaa": "1"}}`},
			map[string]any{"packages": []string{"@anthropic-ai/claude-code", "aaa"}}},
		{"npm", spec.BuilderCollectInput{Candy: "empty"}, map[string]any{}},
		// cargo records BOTH legs: the library paths (a function of the candy name) and the
		// package name `cargo install --path` registers.
		{"cargo", spec.BuilderCollectInput{Candy: "ripgrep", Manifest: "Cargo.toml", ManifestContent: "[package]\nname = \"ripgrep\"\n"}, map[string]any{
			"lib_dir":  "/usr/local/lib/charly/ripgrep",
			"ld_conf":  "/etc/ld.so.conf.d/charly-ripgrep.conf",
			"packages": []string{"ripgrep"},
		}},
		// mise's teardown set is the manifest's tools and the manifest copied into $HOME.
		{"mise", spec.BuilderCollectInput{Candy: "tools", Manifest: "mise.toml", ManifestContent: "[tools]\nnode = \"22\"\nusage = \"latest\"\n"},
			map[string]any{"manifest": "mise.toml", "tools": []string{"node", "usage"}}},
		{"mise", spec.BuilderCollectInput{Candy: "tv", Manifest: ".tool-versions", ManifestContent: "# pinned\nnode 22\npython 3.12\n"},
			map[string]any{"manifest": ".tool-versions", "tools": []string{"node", "python"}}},
		{"aur", spec.BuilderCollectInput{Candy: "chrome", Packages: []string{"google-chrome"}}, map[string]any{"packages": []string{"google-chrome"}}},
		{"aur", spec.BuilderCollectInput{Candy: "x"}, map[string]any{}},
	}
	for _, tc := range cases {
		got, err := BuilderCollectContext(tc.word, tc.in)
		if err != nil {
			t.Errorf("BuilderCollectContext(%q, %s): %v", tc.word, tc.in.Candy, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("BuilderCollectContext(%q, %s) = %+v, want %+v", tc.word, tc.in.Candy, got, tc.want)
		}
	}
	// An unknown word and an unparseable manifest are errors — never an empty teardown.
	if _, err := BuilderCollectContext("unknown", spec.BuilderCollectInput{Candy: "x"}); err == nil {
		t.Error("an unknown builder word returned no error")
	}
	if _, err := BuilderCollectContext("npm", spec.BuilderCollectInput{Candy: "x", Manifest: "package.json", ManifestContent: "{"}); err == nil {
		t.Error("an unparseable package.json returned no error")
	}
}

// TestBuilderReverse covers the per-builder teardown-op KIND derivation the four plugins serve via
// OpReverse — the logic this externalization moves out-of-process. Context arrives JSON-decoded
// (string slices as []any), so the []any decode path is exercised too.
func TestBuilderReverse(t *testing.T) {
	// pixi → pixi-env-remove (user scope, Extra[layer]=candy).
	ops := BuilderReverse("pixi", spec.BuilderReverseInput{Candy: "jupyter", Context: map[string]any{"env_name": "default"}})
	if len(ops) != 1 || ops[0].Kind != spec.ReverseOpPixiEnvRemove || ops[0].Scope != spec.ScopeUser {
		t.Fatalf("pixi reverse = %+v, want [pixi-env-remove user]", ops)
	}
	if ops[0].Extra["layer"] != "jupyter" || len(ops[0].Targets) != 1 || ops[0].Targets[0] != "default" {
		t.Fatalf("pixi reverse op fields = %+v", ops[0])
	}

	// aur → package-remove (system scope, pac format); []any decode path.
	ops = BuilderReverse("aur", spec.BuilderReverseInput{Candy: "chrome", Context: map[string]any{"packages": []any{"google-chrome"}}})
	if len(ops) != 1 || ops[0].Kind != spec.ReverseOpPackageRemove || ops[0].Scope != spec.ScopeSystem || ops[0].Format != "pac" {
		t.Fatalf("aur reverse = %+v, want [package-remove system pac]", ops)
	}

	// npm → npm-uninstall-g when packages present; nil when absent (the best-effort no-op).
	ops = BuilderReverse("npm", spec.BuilderReverseInput{Context: map[string]any{"packages": []string{"@anthropic-ai/claude-code"}}})
	if len(ops) != 1 || ops[0].Kind != spec.ReverseOpNpmUninstallG {
		t.Fatalf("npm reverse = %+v, want [npm-uninstall-g]", ops)
	}
	if got := BuilderReverse("npm", spec.BuilderReverseInput{}); got != nil {
		t.Fatalf("npm reverse with empty context = %+v, want nil", got)
	}

	// cargo → cargo-uninstall of the recorded package; nil otherwise.
	ops = BuilderReverse("cargo", spec.BuilderReverseInput{Context: map[string]any{"packages": []string{"ripgrep"}}})
	if len(ops) != 1 || ops[0].Kind != spec.ReverseOpCargoUninstall || ops[0].Targets[0] != "ripgrep" {
		t.Fatalf("cargo reverse = %+v, want [cargo-uninstall ripgrep]", ops)
	}

	// mise → one user-scope plugin script: uninstall the tools, remove the copied manifest.
	ops = BuilderReverse("mise", spec.BuilderReverseInput{Context: map[string]any{"tools": []any{"node"}, "manifest": "mise.toml"}})
	if len(ops) != 1 || ops[0].Kind != spec.ReverseOpPluginScript || ops[0].Scope != spec.ScopeUser {
		t.Fatalf("mise reverse = %+v, want [plugin-script user]", ops)
	}
	if script := ops[0].Extra[spec.ReverseOpPluginScriptKey]; script != "mise uninstall --all 'node'\nrm -f \"$HOME\"/'mise.toml'" {
		t.Fatalf("mise reverse script = %q", script)
	}

	// unknown / empty → no teardown.
	if got := BuilderReverse("unknown", spec.BuilderReverseInput{}); got != nil {
		t.Fatalf("unknown reverse = %+v, want nil", got)
	}
}
