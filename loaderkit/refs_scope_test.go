package loaderkit

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/opencharly/spec/spec"
)

// refs_scope_test.go — the SCOPE gate on the COLLECTOR (CollectRemoteRefsOpts), the half that
// decides WHICH composition named a ref. The version rule is per BOX: two independent boxes
// pinning the same candy at different tags is NOT a conflict (silent), while two references
// INSIDE one box ARE (a same-scope skew). The collector therefore labels every collected ref with
// the box scope(s) whose candy closure reaches it, and — for a layer's require:/candy: — with the
// box that pulled that layer in, not with the layer's own name.

func scopeOf(t *testing.T, downloads []spec.RemoteDownload, repo, ver, bare string) []string {
	t.Helper()
	for _, d := range downloads {
		if d.RepoPath != repo || d.Version != ver {
			continue
		}
		if refs := d.RefReferrers[bare]; len(refs) > 0 {
			out := append([]string(nil), refs...)
			sort.Strings(out)
			return out
		}
	}
	return nil
}

// A box's own candy list labels its refs with THAT box's scope.
func TestCollectScopesBoxCandyListToOwningBox(t *testing.T) {
	const ref = "@github.com/opencharly/pod-dbus:v2026.243.1831"
	cfg := &spec.Config{Box: spec.BoxMap{
		"versa": json.RawMessage(`{"candy": ["` + ref + `"]}`),
	}}
	downloads, err := CollectRemoteRefsOpts(cfg, nil, spec.ResolveOpts{}, spec.RefsCollectSeams{})
	if err != nil {
		t.Fatal(err)
	}
	got := scopeOf(t, downloads, "github.com/opencharly/pod-dbus", "v2026.243.1831", "github.com/opencharly/pod-dbus")
	if len(got) != 1 || got[0] != "box=versa" {
		t.Fatalf("box candy ref scope = %v, want [box=versa]", got)
	}
}

// TWO INDEPENDENT BOXES pinning the same ref at different tags yields TWO downloads, each carrying
// ONLY its own box's scope. That is what makes a cross-box version difference SILENT at the arbiter
// (no shared scope) while still fetching both.
func TestCollectScopesTwoBoxesAtDifferentTagsStayIndependent(t *testing.T) {
	const oldRef = "@github.com/opencharly/pod-dbus:v2026.239.1555"
	const newRef = "@github.com/opencharly/pod-dbus:v2026.243.1831"
	cfg := &spec.Config{Box: spec.BoxMap{
		"fedora-coder": json.RawMessage(`{"candy": ["` + oldRef + `"]}`),
		"versa":        json.RawMessage(`{"candy": ["` + newRef + `"]}`),
	}}
	downloads, err := CollectRemoteRefsOpts(cfg, nil, spec.ResolveOpts{}, spec.RefsCollectSeams{})
	if err != nil {
		t.Fatal(err)
	}
	old := scopeOf(t, downloads, "github.com/opencharly/pod-dbus", "v2026.239.1555", "github.com/opencharly/pod-dbus")
	if len(old) != 1 || old[0] != "box=fedora-coder" {
		t.Fatalf("old-tag scope = %v, want [box=fedora-coder]", old)
	}
	fresh := scopeOf(t, downloads, "github.com/opencharly/pod-dbus", "v2026.243.1831", "github.com/opencharly/pod-dbus")
	if len(fresh) != 1 || fresh[0] != "box=versa" {
		t.Fatalf("new-tag scope = %v, want [box=versa]", fresh)
	}
}

// TWO LAYERS INSIDE ONE BOX referencing different versions of the same candy: BOTH references are
// attributed to that ONE box (the union of the box's layer closure), so the arbiter sees a genuine
// SAME-scope skew. This is the authoritative case #2, and it is exactly what the layer->owning-box
// attribution exists for.
func TestCollectScopesTwoLayersInOneBoxShareTheBoxScope(t *testing.T) {
	// The box composes layer-a and layer-b directly; each layer pins pod-dbus at a different tag.
	layers := map[string]spec.CandyReader{
		"layer-a": newLoaderTestCandy("layer-a", spec.CandyModel{}, spec.CandyView{
			Require: []spec.CandyRef{"@github.com/opencharly/pod-dbus:v2026.239.1555"},
		}),
		"layer-b": newLoaderTestCandy("layer-b", spec.CandyModel{}, spec.CandyView{
			Require: []spec.CandyRef{"@github.com/opencharly/pod-dbus:v2026.243.1831"},
		}),
	}
	cfg := &spec.Config{Box: spec.BoxMap{
		"one-box": json.RawMessage(`{"candy": ["layer-a", "layer-b"]}`),
	}}
	downloads, err := CollectRemoteRefsOpts(cfg, layers, spec.ResolveOpts{}, spec.RefsCollectSeams{})
	if err != nil {
		t.Fatal(err)
	}
	old := scopeOf(t, downloads, "github.com/opencharly/pod-dbus", "v2026.239.1555", "github.com/opencharly/pod-dbus")
	if len(old) != 1 || old[0] != "box=one-box" {
		t.Fatalf("layer-a ref scope = %v, want [box=one-box] (the box that pulled the layer in)", old)
	}
	fresh := scopeOf(t, downloads, "github.com/opencharly/pod-dbus", "v2026.243.1831", "github.com/opencharly/pod-dbus")
	if len(fresh) != 1 || fresh[0] != "box=one-box" {
		t.Fatalf("layer-b ref scope = %v, want [box=one-box]", fresh)
	}
}

// A layer pulled in by TWO boxes carries BOTH scopes (the union) — so an agreement in one box and a
// disagreement in another is reported for the latter only.
func TestCollectScopesLayerSharedByTwoBoxesCarriesBoth(t *testing.T) {
	layers := map[string]spec.CandyReader{
		"layer-a": newLoaderTestCandy("layer-a", spec.CandyModel{}, spec.CandyView{
			Require: []spec.CandyRef{"@github.com/opencharly/pod-dbus:v2026.243.1831"},
		}),
	}
	cfg := &spec.Config{Box: spec.BoxMap{
		"alpha": json.RawMessage(`{"candy": ["layer-a"]}`),
		"beta":  json.RawMessage(`{"candy": ["layer-a"]}`),
	}}
	downloads, err := CollectRemoteRefsOpts(cfg, layers, spec.ResolveOpts{}, spec.RefsCollectSeams{})
	if err != nil {
		t.Fatal(err)
	}
	got := scopeOf(t, downloads, "github.com/opencharly/pod-dbus", "v2026.243.1831", "github.com/opencharly/pod-dbus")
	if len(got) != 2 || got[0] != "box=alpha" || got[1] != "box=beta" {
		t.Fatalf("shared-layer ref scope = %v, want [box=alpha box=beta]", got)
	}
}

// A namespaced box is scoped by its QUALIFIED name, so two same-leaf boxes in different namespaces
// are distinct scopes (they are independent compositions). Proven via a base edge: the root box's
// base is the namespace's box, so the namespace box IS walked and its candy ref is scoped
// "box=cachyos.cachyos" — NOT the bare leaf "cachyos" (which would collide with another
// namespace's same-leaf box).
func TestCollectScopesNamespaceBoxIsQualified(t *testing.T) {
	const ref = "@github.com/opencharly/pod-dbus:v2026.243.1831"
	nsBox := &spec.Config{Box: spec.BoxMap{
		"cachyos": json.RawMessage(`{"candy": ["` + ref + `"]}`),
	}}
	cfg := &spec.Config{
		Box:        spec.BoxMap{"main": json.RawMessage(`{"base": "cachyos.cachyos"}`)},
		Namespaces: map[string]*spec.Config{"cachyos": nsBox},
	}
	downloads, err := CollectRemoteRefsOpts(cfg, nil, spec.ResolveOpts{}, spec.RefsCollectSeams{})
	if err != nil {
		t.Fatal(err)
	}
	got := scopeOf(t, downloads, "github.com/opencharly/pod-dbus", "v2026.243.1831", "github.com/opencharly/pod-dbus")
	if len(got) != 1 || got[0] != "box=cachyos.cachyos" {
		t.Fatalf("namespaced box scope = %v, want [box=cachyos.cachyos] (qualified, not the bare leaf)", got)
	}
}

// A layer whose owning box cannot be determined (not reachable from any collected box) forms its
// OWN scope, so its difference stays SILENT (the version rule only reports within a box).
func TestCollectScopesUnownedLayerIsItsOwnSilentScope(t *testing.T) {
	layers := map[string]spec.CandyReader{
		"orphan": newLoaderTestCandy("orphan", spec.CandyModel{}, spec.CandyView{
			Require: []spec.CandyRef{"@github.com/opencharly/pod-dbus:v2026.243.1831"},
		}),
	}
	// No box composes "orphan".
	cfg := &spec.Config{Box: spec.BoxMap{
		"box1": json.RawMessage(`{"candy": ["@github.com/opencharly/layer-x:v1"]}`),
	}}
	downloads, err := CollectRemoteRefsOpts(cfg, layers, spec.ResolveOpts{}, spec.RefsCollectSeams{})
	if err != nil {
		t.Fatal(err)
	}
	got := scopeOf(t, downloads, "github.com/opencharly/pod-dbus", "v2026.243.1831", "github.com/opencharly/pod-dbus")
	if len(got) != 1 || got[0] != "layer=orphan" {
		t.Fatalf("unowned layer scope = %v, want [layer=orphan]", got)
	}
}
