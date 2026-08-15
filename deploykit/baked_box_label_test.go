package deploykit

import (
	"testing"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/spec/spec"
)

// TestBuildBakedMetadata_BoxLabelIsLeafName is the regression gate for the stale-artifact
// certification incident (charly#check-box-target-image).
//
// The Generator's Boxes map is keyed by the identifier the caller named, which for a box reached
// through an `import:` namespace is QUALIFIED (`fedora.fedora-nonfree`). The image REF is not:
// buildkit.ResolveBox descends into the namespace, so the box resolves under its leaf name and
// FullTag reads `<registry>/fedora-nonfree:<tag>`. Emitting the map key into `ai.opencharly.box`
// therefore labelled the image with a name no ref ever carries — and the local-image resolver
// keys its label family on exactly that equality, so a freshly built namespaced image dropped out
// of its own box's candidate family and an older bare-labelled image was elected and certified.
//
// The label MUST equal the ref's repo name. Fails without the fix (emits "fedora.fedora-nonfree").
func TestBuildBakedMetadata_BoxLabelIsLeafName(t *testing.T) {
	const qualified = "fedora.fedora-nonfree"
	g := NewRenderGenerator()
	g.Config = &spec.Config{}
	g.Candies = map[string]CandyModel{}
	g.Boxes = map[string]*buildkit.ResolvedBox{
		qualified: {ResolvedBox: spec.ResolvedBox{
			Name:             "fedora-nonfree",
			EffectiveVersion: "2026.227.0830",
			Registry:         "ghcr.io/opencharly",
			Tag:              "2026.227.0836",
			FullTag:          "ghcr.io/opencharly/fedora-nonfree:2026.227.0836",
			User:             "charly",
			Home:             "/home/charly",
		}},
	}

	meta := g.buildBakedMetadata(qualified, nil)
	if meta.Box != "fedora-nonfree" {
		t.Fatalf("ai.opencharly.box = %q, want %q — the label must equal the ref's repo name, "+
			"never the namespace-qualified map key (a namespaced build is otherwise invisible to "+
			"a resolve of its own image name)", meta.Box, "fedora-nonfree")
	}
	// The invariant stated as the resolver consumes it: label == the trailing repo segment of the
	// ref this very build produces.
	if want := refRepoNameForTest(g.Boxes[qualified].FullTag); meta.Box != want {
		t.Fatalf("ai.opencharly.box = %q, but the built ref is named %q", meta.Box, want)
	}
	// An unqualified key (every non-imported box, and every synthesized intermediate) is unchanged.
	g.Boxes["fedora-nonfree"] = g.Boxes[qualified]
	if meta := g.buildBakedMetadata("fedora-nonfree", nil); meta.Box != "fedora-nonfree" {
		t.Fatalf("unqualified key: ai.opencharly.box = %q, want %q", meta.Box, "fedora-nonfree")
	}
}

// refRepoNameForTest mirrors the resolver's trailing-repo-segment rule for the assertion above
// (the resolver's own predicate is unexported in spec/container; this is the one-line shape it
// applies to a plain `<registry>/<name>:<calver>` ref).
func refRepoNameForTest(ref string) string {
	repo := ref
	if i := lastIndexByte(repo, ':'); i > lastIndexByte(repo, '/') {
		repo = repo[:i]
	}
	return repo[lastIndexByte(repo, '/')+1:]
}

func lastIndexByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}
