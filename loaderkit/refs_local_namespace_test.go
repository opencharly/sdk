package loaderkit

import (
	"encoding/json"
	"testing"

	"github.com/opencharly/spec/spec"
)

// refs_local_namespace_test.go — the IMPORTED-NAMESPACE half of local-template ref collection.
//
// A `kind:local` template declared in an imported namespace composes remote @-ref candies exactly
// like a root project's own template, and the resolved-project envelope exposes it under a
// namespace-qualified key (`cachyos.charly-cachyos-app`, via UnifiedFile.ProjectTemplates). The
// validator walks that folded map and checks each ref against the candy-model set, so the
// collector must put the SAME namespace's refs into the fetch set — otherwise the refs are never
// fetched, never scanned, and validate reports a candy that really exists as missing
// (opencharly/sdk#488).

// resolveLocalTestSeam decodes an authored template body into the parts the collector reads.
func resolveLocalTestSeam(body json.RawMessage) (*spec.ResolvedLocal, error) {
	var l spec.Local
	if err := json.Unmarshal(body, &l); err != nil {
		return nil, err
	}
	return &spec.ResolvedLocal{Candy: l.Candy}, nil
}

func downloadFor(downloads []spec.RemoteDownload, repo, ver string) (spec.RemoteDownload, bool) {
	for _, d := range downloads {
		if d.RepoPath == repo && d.Version == ver {
			return d, true
		}
	}
	return spec.RemoteDownload{}, false
}

// TestCollectRemoteRefsImportedNamespaceLocalTemplate is the #488 regression guard: a remote candy
// referenced ONLY from a kind:local template inside an imported namespace must enter the fetch set.
// Before the fix the collector iterated only the ROOT Config's Local map, so this download was
// absent and every consumer that validates the folded template map reported the candy missing.
func TestCollectRemoteRefsImportedNamespaceLocalTemplate(t *testing.T) {
	const ref = "@github.com/opencharly/pod-dsh/candy/dsh:v2026.279.1210"
	const repo = "github.com/opencharly/pod-dsh"
	const bare = "github.com/opencharly/pod-dsh/candy/dsh"

	ns := &spec.Config{Local: map[string]json.RawMessage{
		"charly-cachyos-app": json.RawMessage(`{"candy": ["wheel-nopasswd", "` + ref + `"]}`),
	}}
	cfg := &spec.Config{Namespaces: map[string]*spec.Config{"cachyos": ns}}

	downloads, err := CollectRemoteRefsOpts(cfg, nil, spec.ResolveOpts{}, spec.RefsCollectSeams{
		ResolveLocal: resolveLocalTestSeam,
	})
	if err != nil {
		t.Fatal(err)
	}
	d, ok := downloadFor(downloads, repo, "v2026.279.1210")
	if !ok {
		t.Fatalf("imported namespace's kind:local remote ref %q is absent from the fetch set: %+v", ref, downloads)
	}
	if len(d.Refs) != 1 || d.Refs[0] != bare {
		t.Fatalf("fetch set refs = %v, want [%s]", d.Refs, bare)
	}
	if got := d.RefReferrers[bare]; len(got) != 1 || got[0] != "kind:local=cachyos.charly-cachyos-app" {
		t.Fatalf("fetch set scopes = %v, want [kind:local=cachyos.charly-cachyos-app]", got)
	}
}

// TestCollectRemoteRefsNamespaceLocalTemplateTerminatesOnCycle proves the recursion that reaches an
// imported namespace's templates uses the SAME path-scoped ancestor guard the rest of the loader
// uses: a mutual import (main <-> cachyos) must terminate, and a namespace mounted at two alias
// paths must still contribute under both.
func TestCollectRemoteRefsNamespaceLocalTemplateTerminatesOnCycle(t *testing.T) {
	const ref = "@github.com/opencharly/pod-dsh/candy/dsh:v2026.279.1210"
	ns := &spec.Config{Local: map[string]json.RawMessage{
		"app": json.RawMessage(`{"candy": ["` + ref + `"]}`),
	}}
	root := &spec.Config{
		Namespaces: map[string]*spec.Config{
			"cachyos": ns,
			"alias":   ns, // the same namespace mounted at a second alias path
		},
	}
	ns.Namespaces = map[string]*spec.Config{"main": root} // mutual import back to the root

	downloads, err := CollectRemoteRefsOpts(root, nil, spec.ResolveOpts{}, spec.RefsCollectSeams{
		ResolveLocal: resolveLocalTestSeam,
	})
	if err != nil {
		t.Fatalf("cyclic namespace graph must terminate, got error: %v", err)
	}
	d, ok := downloadFor(downloads, "github.com/opencharly/pod-dsh", "v2026.279.1210")
	if !ok {
		t.Fatalf("cyclic-graph ref absent from the fetch set: %+v", downloads)
	}
	const bare = "github.com/opencharly/pod-dsh/candy/dsh"
	got := d.RefReferrers[bare]
	if len(got) != 2 {
		t.Fatalf("ref scopes = %v, want both alias paths [kind:local=cachyos.app kind:local=alias.app]", got)
	}
}

// TestCollectRemoteRefsNamespaceLocalTemplateIgnoresBareCandy is the NEGATIVE control: the
// namespace recursion must collect exactly the authored REMOTE refs and nothing else. A bare-name
// candy (a local namespace candy) is not a fetch coordinate, so it must not fabricate a download;
// and a template that names no remote candy adds nothing.
func TestCollectRemoteRefsNamespaceLocalTemplateIgnoresBareCandy(t *testing.T) {
	ns := &spec.Config{Local: map[string]json.RawMessage{
		"app": json.RawMessage(`{"candy": ["wheel-nopasswd", "cachyos-base"]}`),
	}}
	cfg := &spec.Config{Namespaces: map[string]*spec.Config{"cachyos": ns}}

	downloads, err := CollectRemoteRefsOpts(cfg, nil, spec.ResolveOpts{}, spec.RefsCollectSeams{
		ResolveLocal: resolveLocalTestSeam,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(downloads) != 0 {
		t.Fatalf("bare local candies must not fabricate remote downloads, got %+v", downloads)
	}
}
