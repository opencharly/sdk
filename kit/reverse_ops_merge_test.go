package kit

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestAddCandyDeployment_MergesReverseOps is the charly#687 regression. A candy
// record is SHARED by every deploy that uses it (refcounted by DeployedBy); its
// reverse_ops must therefore be the UNION across those deploys. Before the fix the
// second deploy's ops replaced the first's, so teardown replayed only one and left
// the other's resource running (live: two `deploy:kindcluster` deploys sharing the
// `plugin-kube` provider candy left one kind cluster up).
func TestAddCandyDeployment_MergesReverseOps(t *testing.T) {
	dir := t.TempDir()
	paths := &LedgerPaths{
		ConfigFile: filepath.Join(dir, "charly.yml"),
		LockFile:   filepath.Join(dir, "charly.yml.lock"),
	}
	clusterOp := func(name string) spec.ReverseOp {
		return spec.ReverseOp{
			Kind:  spec.ReverseOpPluginScript,
			Extra: map[string]string{"script": "KIND_EXPERIMENTAL_PROVIDER='podman' kind delete cluster --name '" + name + "' >/dev/null 2>&1 || true;"},
		}
	}

	// deploy A contributes op(A)
	if err := AddCandyDeployment(paths, "plugin-kube", "deploy-a", func(rec *CandyRecord) {
		rec.ReverseOps = []spec.ReverseOp{clusterOp("a")}
	}); err != nil {
		t.Fatalf("AddCandyDeployment A: %v", err)
	}
	// deploy B contributes op(B) — must NOT clobber op(A)
	if err := AddCandyDeployment(paths, "plugin-kube", "deploy-b", func(rec *CandyRecord) {
		rec.ReverseOps = []spec.ReverseOp{clusterOp("b")}
	}); err != nil {
		t.Fatalf("AddCandyDeployment B: %v", err)
	}

	rec, err := ReadCandyRecord(paths, "plugin-kube")
	if err != nil {
		t.Fatalf("ReadCandyRecord: %v", err)
	}
	if rec == nil {
		t.Fatal("candy record missing")
	}
	if len(rec.ReverseOps) != 2 {
		t.Fatalf("both deploys' reverse ops must be retained, got %d: %+v", len(rec.ReverseOps), rec.ReverseOps)
	}
	if !containsReverseOp(rec.ReverseOps, clusterOp("a")) || !containsReverseOp(rec.ReverseOps, clusterOp("b")) {
		t.Fatalf("both op(A) and op(B) must be present: %+v", rec.ReverseOps)
	}

	// A re-add of the SAME deploy must not duplicate its op (content-dedup).
	if err := AddCandyDeployment(paths, "plugin-kube", "deploy-a", func(rec *CandyRecord) {
		rec.ReverseOps = []spec.ReverseOp{clusterOp("a")}
	}); err != nil {
		t.Fatalf("AddCandyDeployment A re-add: %v", err)
	}
	rec, err = ReadCandyRecord(paths, "plugin-kube")
	if err != nil {
		t.Fatalf("ReadCandyRecord: %v", err)
	}
	if len(rec.ReverseOps) != 2 {
		t.Fatalf("re-adding the same deploy must not duplicate ops, got %d: %+v", len(rec.ReverseOps), rec.ReverseOps)
	}
	if len(rec.DeployedBy) != 2 {
		t.Fatalf("DeployedBy must stay {a,b}, got %v", rec.DeployedBy)
	}
}

// TestAddCandyDeploymentVia_MergesReverseOps drives the EXECUTOR-ROUTED writer
// (the nested-deploy path) and fails if ITS merge is reverted — the twin of the
// operator-side test above, so both changed writers are covered (B12: a change
// whose new behavior has no test that would fail without it blocks).
func TestAddCandyDeploymentVia_MergesReverseOps(t *testing.T) {
	exec := &fakeRemoteExec{}
	paths := &LedgerPaths{ConfigFile: "/tmp/fake/charly.yml", LockFile: "/tmp/fake/charly.yml.lock"}
	op := func(name string) spec.ReverseOp {
		return spec.ReverseOp{Kind: spec.ReverseOpPluginScript, Extra: map[string]string{"script": "kind delete cluster --name " + name}}
	}
	if err := AddCandyDeploymentVia(exec, paths, "plugin-kube", "deploy-a", func(rec *CandyRecord) {
		rec.ReverseOps = []spec.ReverseOp{op("a")}
	}); err != nil {
		t.Fatalf("AddCandyDeploymentVia A: %v", err)
	}
	// The substrate file now holds A's record; feed it back so B reads it.
	exec.existing = exec.written
	if err := AddCandyDeploymentVia(exec, paths, "plugin-kube", "deploy-b", func(rec *CandyRecord) {
		rec.ReverseOps = []spec.ReverseOp{op("b")}
	}); err != nil {
		t.Fatalf("AddCandyDeploymentVia B: %v", err)
	}
	for _, want := range []string{"cluster --name a", "cluster --name b"} {
		if !strings.Contains(exec.written, want) {
			t.Fatalf("executor-routed writer must retain BOTH deploys' ops; missing %q:\n%s", want, exec.written)
		}
	}
}

// TestMergeReverseOps pins the helper's contract directly (dedup by content; a
// no-op for an empty incoming set; nil-safe).
func TestMergeReverseOps(t *testing.T) {
	op := func(s string) spec.ReverseOp {
		return spec.ReverseOp{Kind: spec.ReverseOpPluginScript, Extra: map[string]string{"script": s}}
	}

	if got := MergeReverseOps(nil, nil); got != nil {
		t.Fatalf("nil+nil must stay nil, got %+v", got)
	}
	base := []spec.ReverseOp{op("x")}
	got := MergeReverseOps(base, []spec.ReverseOp{op("y")})
	if len(got) != 2 || !containsReverseOp(got, op("x")) || !containsReverseOp(got, op("y")) {
		t.Fatalf("merge must union: %+v", got)
	}
	// duplicate incoming is deduped
	got = MergeReverseOps(base, []spec.ReverseOp{op("x")})
	if len(got) != 1 {
		t.Fatalf("content-dedup must drop the duplicate: %+v", got)
	}
	// existing slice is not mutated in place
	if len(base) != 1 {
		t.Fatalf("existing slice must not be mutated: %+v", base)
	}
}
