package loaderkit

import (
	"testing"

	"github.com/opencharly/sdk/deploykit"
	"github.com/opencharly/spec/spec"
)

// bakeplugin_effective_version_test.go (relocated from charly/effective_version_test.go's
// TestBakePluginImpliesRequire_FeedsEffectiveVersion, #55 K3 Cone 4): proves the S0 hash-gap
// fix — a candy that declares ONLY `bake_plugin: <ref>` (no explicit require:) still pulls
// the baked plugin candy into its require chain (loaderkit's own populateFromYAML
// implication), so the baked plugin's version: contributes to the composing image's
// EffectiveVersion (deploykit.ComputeEffectiveVersions) and bumping the baked plugin's
// version bumps the image identity. This test FAILS without the bake_plugin→require
// implication. loaderkit already imports deploykit in production (scan_candy.go,
// finalize_candy.go, …), so this cross-kit integration test lives here rather than
// creating a reverse dependency.

// candyFromYAML runs a CandyYAML body through the same scan-pipeline pass production uses
// (ScanInlineCandy → spec.FinalizeCandyRefs) and wraps it into a spec.CandyReader fixture —
// the bake_plugin→require implication (populateFromYAML) and every other derived field this
// test exercises live in that pass, not in a hand-built candy literal.
func candyFromYAML(t *testing.T, name string, ly *spec.CandyYAML) spec.CandyReader {
	t.Helper()
	m, v, refs := ScanInlineCandy(name, "", ly)
	spec.FinalizeCandyRefs(&m, &v, refs)
	return newLoaderTestCandy(name, m, v)
}

// newLoaderTestCandy wraps a CandyModel + CandyView into a spec.CandyReader fixture,
// stamping name onto both views (mirrors charly's candy_test_helpers_test.go:testCandy).
func newLoaderTestCandy(name string, m spec.CandyModel, v spec.CandyView) spec.CandyReader {
	m.Name = name
	v.Name = name
	return deploykit.NewSpecCandyModel(m, v)
}

func candyRequires(l spec.CandyReader, bare string) bool { return countCandyRequire(l, bare) > 0 }

func countCandyRequire(l spec.CandyReader, bare string) int {
	n := 0
	for _, r := range l.GetRequire() {
		if r.Bare() == bare {
			n++
		}
	}
	return n
}

func TestBakePluginImpliesRequire_FeedsEffectiveVersion(t *testing.T) {
	// The consumer candy declares ONLY bake_plugin (no explicit require:).
	consumer := candyFromYAML(t, "consumer-candy", &spec.CandyYAML{
		Version:    "2026.100.0000", // lower than the baked plugin below
		BakePlugin: []string{"plugin-baked"},
	})

	// The implication: the baked plugin ref is now in the require set.
	if !candyRequires(consumer, "plugin-baked") {
		t.Fatalf("bake_plugin did not imply require: consumer.GetRequire() = %v", consumer.GetRequire())
	}
	// And it was not double-added (it's a set).
	if n := countCandyRequire(consumer, "plugin-baked"); n != 1 {
		t.Fatalf("plugin-baked appears %d times in require, want exactly 1", n)
	}

	plugin := newLoaderTestCandy("plugin-baked", spec.CandyModel{Version: "2026.200.0000"}, spec.CandyView{}) // the newest version
	layers := map[string]deploykit.CandyModel{
		"consumer-candy": consumer,
		"plugin-baked":   plugin,
	}
	images := map[string]*deploykit.ResolvedBox{ // An image composing ONLY the consumer candy. Its EffectiveVersion must
		// reflect the baked plugin's (higher) version, reached via the implied require.
		"img": {ResolvedBox: spec.ResolvedBox{Name: "img", Candy: []string{"consumer-candy"}, IsExternalBase: true, Base: "quay.io/x:1"}}}
	if err := deploykit.ComputeEffectiveVersions(images, layers); err != nil {
		t.Fatalf("ComputeEffectiveVersions: %v", err)
	}
	if got := images["img"].EffectiveVersion; got != "2026.200.0000" {
		t.Fatalf("EffectiveVersion = %q, want 2026.200.0000 (the baked plugin's version reached via the implied require)", got)
	}

	// Bumping the baked plugin's version bumps the composing image's identity.
	layers["plugin-baked"] = newLoaderTestCandy("plugin-baked", spec.CandyModel{Version: "2026.300.0000"}, spec.CandyView{})
	images2 := map[string]*deploykit.ResolvedBox{"img": {ResolvedBox: spec.ResolvedBox{Name: "img", Candy: []string{"consumer-candy"}, IsExternalBase: true, Base: "quay.io/x:1"}}}
	if err := deploykit.ComputeEffectiveVersions(images2, layers); err != nil {
		t.Fatal(err)
	}
	if got := images2["img"].EffectiveVersion; got != "2026.300.0000" {
		t.Fatalf("after baked-plugin bump: EffectiveVersion = %q, want 2026.300.0000", got)
	}

	// Declaring BOTH bake_plugin and an explicit require of the same ref does not
	// double-add (the redundant case the cutover removes from candy/charly-mcp).
	both := candyFromYAML(t, "both", &spec.CandyYAML{
		Version:    "2026.100.0000",
		Require:    []string{"plugin-baked"},
		BakePlugin: []string{"plugin-baked"},
	})
	if n := countCandyRequire(both, "plugin-baked"); n != 1 {
		t.Fatalf("explicit require + bake_plugin double-added: plugin-baked appears %d times, want 1", n)
	}
}
