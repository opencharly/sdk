package deploykit

// deploy_state_test.go — sdk-level coverage for the deploy STATE-MODEL body relocated
// out of charly/deploy.go into sdk/deploykit by K5-Unit-1. The moved functions had ZERO
// sdk-level tests (the pr-validator F1/F2 finding on sdk PR #54); these tests exercise
// them LIVE in the sdk gate so (a) each would FAIL without the moved code (check-coverage
// gate) and (b) the green `go test ./...` output is a real live invocation of the moved
// code (earning `analysed on a live system` honestly).
//
// Coverage:
//   - ExportAllBox against a constructed *spec.ResolvedProject (the #67 keystone).
//   - SaveBundleConfig round-trip through a stub DeployStateHost (the 1-op
//     LoadUnifiedBundleConfig seam) + CHARLY_DEPLOY_CONFIG tempdir redirect + a stub
//     marshalNode callback (exercises LoadBundleConfig's fail-safe, the kit.LatestSchemaVersion
//     version stamp, the atomic tempfile+rename write). The deploy-kind-specific marshal
//     itself is tested charly-side (it lives in charly/deploy_nodeform.go).
//   - RegisterDeployStateHost seam (the charly init hook, 1-op).
//   - The pure helpers DescriptionInfo / IsSameBaseBox / RemoveBySource / RemoveByExactSource.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
	"gopkg.in/yaml.v3"
)

// --- ExportAllBox — the #67 keystone (#ResolvedProject envelope → BundleConfig) ---

// TestExportAllBox_ProjectsBoxAuthoredOverlayFromResolvedProject pins the #67 keystone:
// ExportAllBox reads the box-authored deploy-overlay surfaces (version / description /
// env / env_file / security / network) off the spec.ResolvedProject envelope — NOT the
// live *Config graph (the former shape). A box with at least one set field is emitted; a
// fully-zero box is skipped (the "only include if at least one field is set" guard).
func TestExportAllBox_ProjectsBoxAuthoredOverlayFromResolvedProject(t *testing.T) {
	sec := &spec.Security{Privileged: true, CapAdd: []string{"NET_ADMIN"}}
	rp := &spec.ResolvedProject{
		Boxes: map[string]spec.ResolvedBoxView{
			"web": {
				Version:     "2026.196.0000",
				Description: "web service",
				Env:         map[string]string{"LOG_LEVEL": "debug"},
				EnvFile:     "/etc/web.env",
				Security:    sec,
				Network:     "charly",
			},
			"empty": {}, // every overlay field zero → must be skipped
		},
	}

	dc := ExportAllBox(rp)
	if dc == nil {
		t.Fatal("ExportAllBox returned nil BundleConfig")
	}
	if len(dc.Bundle) != 1 {
		t.Fatalf("ExportAllBox produced %d entries; want 1 (the zero box must be skipped)", len(dc.Bundle))
	}
	entry, ok := dc.Bundle["web"]
	if !ok {
		t.Fatal("ExportAllBox missing the 'web' entry")
	}
	if entry.Version != "2026.196.0000" {
		t.Errorf("entry.Version = %q; want 2026.196.0000", entry.Version)
	}
	if entry.Description != "web service" {
		t.Errorf("entry.Description = %q; want %q", entry.Description, "web service")
	}
	if entry.EnvFile != "/etc/web.env" {
		t.Errorf("entry.EnvFile = %q; want /etc/web.env", entry.EnvFile)
	}
	if entry.Network != "charly" {
		t.Errorf("entry.Network = %q; want charly", entry.Network)
	}
	if !reflect.DeepEqual(entry.Env, map[string]string{"LOG_LEVEL": "debug"}) {
		t.Errorf("entry.Env = %v; want {LOG_LEVEL:debug}", entry.Env)
	}
	if entry.Security == nil || !entry.Security.Privileged || len(entry.Security.CapAdd) != 1 || entry.Security.CapAdd[0] != "NET_ADMIN" {
		t.Errorf("entry.Security = %+v; want Privileged+CapAdd[NET_ADMIN]", entry.Security)
	}

	// Determinism: re-running produces the same map (ExportAllBox sorts names internally).
	dc2 := ExportAllBox(rp)
	if !reflect.DeepEqual(dc.Bundle, dc2.Bundle) {
		t.Errorf("ExportAllBox is non-deterministic across calls")
	}
}

// TestExportAllBox_NilSafe proves the nil-receiver guard: a nil *spec.ResolvedProject
// yields an empty (non-nil) BundleConfig with a live Bundle map, so callers that range
// dc.Bundle never nil-deref.
func TestExportAllBox_NilSafe(t *testing.T) {
	dc := ExportAllBox(nil)
	if dc == nil {
		t.Fatal("ExportAllBox(nil) returned nil")
	}
	if dc.Bundle == nil {
		t.Fatal("ExportAllBox(nil) returned a BundleConfig with nil Bundle map")
	}
	if len(dc.Bundle) != 0 {
		t.Errorf("ExportAllBox(nil) produced %d entries; want 0", len(dc.Bundle))
	}
}

// --- SaveBundleConfig — the kind-blind file shell + callback round-trip ---

// TestSaveBundleConfig_RoundTrip exercises the full SaveBundleConfig write path: the
// fail-safe LoadBundleConfig re-check (through the 1-op LoadUnifiedBundleConfig seam), the
// kit.LatestSchemaVersion version stamp, the caller-supplied marshalNode callback per entry,
// and the atomic tempfile+os.Rename write. CHARLY_DEPLOY_CONFIG redirects the write to a
// tempdir so the test never touches the operator's real per-host overlay. The marshalNode
// stub emits a simple node-form body (the deploy-kind-specific marshal lives in
// charly/deploy_nodeform.go and is tested charly-side).
func TestSaveBundleConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "charly.yml")
	t.Setenv(kit.DeployConfigEnv, dest)

	// Stub the ONE host Mechanism SaveBundleConfig reaches through DeployStateHost (the
	// LoadUnified hop for the fail-safe re-check). The version stamp is kit.LatestSchemaVersion
	// (a direct kit call, not a seam op); the marshal is the caller's callback.
	stub := &StateHostMechanisms{
		LoadUnifiedBundleConfig: func(configDir string) (*BundleConfig, error) {
			return nil, nil // absent file → LoadBundleConfig returns (empty, nil) → fail-safe passes
		},
	}
	prev := DeployStateHost
	RegisterDeployStateHost(stub)
	t.Cleanup(func() { DeployStateHost = prev })

	dc := &BundleConfig{
		Bundle: map[string]BundleNode{
			"web": {
				Image: "web",
				Env:   map[string]string{"LOG_LEVEL": "info"},
			},
		},
	}

	// stubMarshalNode emits a node-form body: a mapping with the discriminator + the
	// struct-marshaled fields (a faithful miniature of charly's marshalBundleNode).
	stubMarshalNode := func(name string, node *BundleNode) (*yaml.Node, error) {
		nb, err := yaml.Marshal(node)
		if err != nil {
			return nil, err
		}
		var nd yaml.Node
		if err := yaml.Unmarshal(nb, &nd); err != nil {
			return nil, err
		}
		body := &yaml.Node{Kind: yaml.MappingNode}
		if len(nd.Content) == 1 && nd.Content[0].Kind == yaml.MappingNode {
			body = nd.Content[0]
		}
		content := &yaml.Node{Kind: yaml.MappingNode}
		value := &yaml.Node{Kind: yaml.MappingNode}
		content.Content = append(content.Content, kit.ScalarNode("pod"), value)
		for i := 0; i+1 < len(body.Content); i += 2 {
			value.Content = append(value.Content, body.Content[i], body.Content[i+1])
		}
		return content, nil
	}

	if err := SaveBundleConfig(dc, stubMarshalNode); err != nil {
		t.Fatalf("SaveBundleConfig: %v", err)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading written overlay %s: %v", dest, err)
	}
	got := string(data)
	// The version stamp is the real kit.LatestSchemaVersion() CalVer (non-empty).
	wantVersion := kit.LatestSchemaVersion().String()
	for _, want := range []string{"version:", wantVersion, "web:", "pod:", "image: web", "LOG_LEVEL"} {
		if !strings.Contains(got, want) {
			t.Errorf("written overlay missing %q:\n%s", want, got)
		}
	}

	// The file must exist at the redirected path (atomic rename landed).
	if !kit.FileExists(dest) {
		t.Errorf("overlay file not present at %s after SaveBundleConfig", dest)
	}
}

// TestSaveBundleConfig_ErrorsWhenCallbackNil pins the nil-callback guard: SaveBundleConfig
// errors clearly when marshalNode is nil (the deploy-kind-specific marshal is the caller's
// responsibility — a nil callback would nil-deref inside the per-entry loop).
func TestSaveBundleConfig_ErrorsWhenCallbackNil(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(kit.DeployConfigEnv, filepath.Join(dir, "charly.yml"))
	prev := DeployStateHost
	DeployStateHost = nil // no fail-safe re-check dep when the seam is nil
	t.Cleanup(func() { DeployStateHost = prev })

	err := SaveBundleConfig(&BundleConfig{Bundle: map[string]BundleNode{"x": {Image: "x"}}}, nil)
	if err == nil {
		t.Fatal("SaveBundleConfig with nil callback returned nil; want an error")
	}
}

// --- RegisterDeployStateHost — the charly init seam (1-op) ---

// TestRegisterDeployStateHost pins the seam contract: a non-nil host is stored; a nil
// argument is ignored (the existing registration survives — charly registers once at
// init and a stray nil re-registration must not wipe it).
func TestRegisterDeployStateHost(t *testing.T) {
	prev := DeployStateHost
	t.Cleanup(func() { DeployStateHost = prev })

	DeployStateHost = nil
	h := &StateHostMechanisms{LoadUnifiedBundleConfig: func(string) (*BundleConfig, error) { return nil, nil }}
	RegisterDeployStateHost(h)
	if DeployStateHost != h {
		t.Fatal("RegisterDeployStateHost did not store the non-nil host")
	}
	// A nil re-registration must NOT wipe the live host.
	RegisterDeployStateHost(nil)
	if DeployStateHost != h {
		t.Fatal("RegisterDeployStateHost(nil) wiped the live registration")
	}
}

// --- Pure helpers (no core dep) ---

func TestDescriptionInfo(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"   ", ""},
		{"one line", "one line"},
		{"  trimmed  ", "trimmed"},
		{"first line\nsecond line\nthird", "first line"},
		{"  multi\n  with indent  ", "multi"},
	}
	for _, c := range cases {
		if got := DescriptionInfo(c.in); got != c.want {
			t.Errorf("DescriptionInfo(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestIsSameBaseBox(t *testing.T) {
	cases := []struct {
		source, box string
		want        bool
	}{
		{"web", "web", true},
		{"web/instance", "web", true}, // instance suffix matches the base
		{"web", "other", false},
		{"other", "web", false},
		{"web/inst", "other", false},
		{"web-thing", "web", false}, // prefix must be box+"/" or exact, not a hyphen suffix
	}
	for _, c := range cases {
		if got := IsSameBaseBox(c.source, c.box); got != c.want {
			t.Errorf("IsSameBaseBox(%q, %q) = %v; want %v", c.source, c.box, got, c.want)
		}
	}
}

func TestRemoveBySource(t *testing.T) {
	entries := []EnvProvideEntry{
		{Name: "A", Source: "web"},
		{Name: "B", Source: "web/inst"},
		{Name: "C", Source: "db"},
		{Name: "D", Source: "other"},
	}
	got, removed := RemoveBySource(entries, "web")
	if !removed {
		t.Error("RemoveBySource reported nothing removed; want removed=true")
	}
	if len(got) != 2 {
		t.Fatalf("RemoveBySource left %d entries; want 2 (db + other)", len(got))
	}
	for _, e := range got {
		if IsSameBaseBox(e.Source, "web") {
			t.Errorf("RemoveBySource left a web-sourced entry: %+v", e)
		}
	}

	// No match → removed=false, slice unchanged.
	got2, removed2 := RemoveBySource(entries, "nope")
	if removed2 {
		t.Error("RemoveBySource(nope) reported removed; want false")
	}
	if len(got2) != len(entries) {
		t.Errorf("RemoveBySource(nope) mutated the slice: %d → %d", len(entries), len(got2))
	}
}

func TestRemoveByExactSource(t *testing.T) {
	entries := []EnvProvideEntry{
		{Name: "A", Source: "web"},
		{Name: "B", Source: "web/inst"},
		{Name: "C", Source: "db"},
	}
	// Exact match on "web" removes ONLY A — the cross-instance match is RemoveBySource's job.
	got, removed := RemoveByExactSource(entries, "web")
	if !removed {
		t.Error("RemoveByExactSource(web) reported removed=false")
	}
	if len(got) != 2 {
		t.Fatalf("RemoveByExactSource(web) left %d; want 2 (web/inst + db)", len(got))
	}
	for _, e := range got {
		if e.Source == "web" {
			t.Errorf("RemoveByExactSource left the exact 'web' source: %+v", e)
		}
	}

	_, removed2 := RemoveByExactSource(entries, "missing")
	if removed2 {
		t.Error("RemoveByExactSource(missing) reported removed; want false")
	}
}

// TestFindBundleNode covers the whole-tree name search (Cutover B unit 5, P13-KERNEL-B —
// moved from charly/k3s_post.go's findBundleNodeByName/findBundleNodePtrByName, which had
// no dedicated test coverage in charly; this closes that gap at the new home). A node may
// be found at the top level, nested under Children at any depth, or nested under Members
// at any depth; a name present nowhere in the tree returns nil.
func TestFindBundleNode(t *testing.T) {
	leaf := &BundleNode{Image: "leaf-image"}
	member := &BundleNode{Image: "member-image"}
	child := &BundleNode{
		Image:    "child-image",
		Children: map[string]*BundleNode{"leaf": leaf},
	}
	root := BundleNode{
		Image:    "root-image",
		Children: map[string]*BundleNode{"child": child},
		Members:  map[string]*BundleNode{"sidecar": member},
	}
	bundle := map[string]BundleNode{"stack": root}

	if got := FindBundleNode(bundle, "stack"); got == nil || got.Image != "root-image" {
		t.Errorf("FindBundleNode(stack) top-level = %v; want root-image", got)
	}
	if got := FindBundleNode(bundle, "child"); got != child {
		t.Errorf("FindBundleNode(child) nested-Children = %v; want %v", got, child)
	}
	if got := FindBundleNode(bundle, "leaf"); got != leaf {
		t.Errorf("FindBundleNode(leaf) nested-two-deep-Children = %v; want %v", got, leaf)
	}
	if got := FindBundleNode(bundle, "sidecar"); got != member {
		t.Errorf("FindBundleNode(sidecar) nested-Members = %v; want %v", got, member)
	}
	if got := FindBundleNode(bundle, "nonexistent"); got != nil {
		t.Errorf("FindBundleNode(nonexistent) = %v; want nil", got)
	}
	if got := FindBundleNode(nil, "anything"); got != nil {
		t.Errorf("FindBundleNode(nil bundle) = %v; want nil", got)
	}
}

// TestFindVmDeployNode_AmbiguousFallbackErrors covers RCA #14 (FINAL/K5 unit
// 6a): the step-3 fallback scan (matching by vm entity when the caller's name
// doesn't key a top-level entry directly) must ERROR on 2+ candidates, never
// first-win — proven live to silently return a DIFFERENT (unrelated) deploy's
// node across separate process runs, because Go randomizes map iteration
// order per process. Steps 1-2 (exact key match) stay unambiguous by
// construction and must never error.
func TestFindVmDeployNode_AmbiguousFallbackErrors(t *testing.T) {
	deploys := map[string]BundleNode{
		// Two independent top-level vm deploys sharing one base entity —
		// the check-substrate / check-builder-vm shape (both real top-level
		// vm deploys, both `from: eval-vm`).
		"check-substrate":  {Target: "vm", From: "eval-vm", Plan: []spec.Step{{Check: "substrate step"}}},
		"check-builder-vm": {Target: "vm", From: "eval-vm", Plan: []spec.Step{{Check: "builder step"}}},
		"unrelated-vm":     {Target: "vm", From: "other-base"},
		"unrelated-non-vm": {Target: "pod"},
	}

	t.Run("ambiguous fallback (2+ From matches) errors, never first-wins", func(t *testing.T) {
		_, ok, err := FindVmDeployNode(deploys, "some-caller-name", "eval-vm")
		if err == nil {
			t.Fatal("FindVmDeployNode with 2 same-base candidates: err = nil, want an ambiguity error")
		}
		if ok {
			t.Error("FindVmDeployNode with 2 same-base candidates: ok = true, want false alongside the error")
		}
	})

	t.Run("unique fallback match still succeeds", func(t *testing.T) {
		node, ok, err := FindVmDeployNode(deploys, "some-caller-name", "other-base")
		if err != nil {
			t.Fatalf("FindVmDeployNode with 1 candidate: unexpected error %v", err)
		}
		if !ok || node.From != "other-base" {
			t.Errorf("FindVmDeployNode with 1 candidate = (%+v, %v), want the unrelated-vm entry", node, ok)
		}
	})

	t.Run("exact key match (step 1) never triggers ambiguity even with same-base siblings present", func(t *testing.T) {
		node, ok, err := FindVmDeployNode(deploys, "check-substrate", "eval-vm")
		if err != nil {
			t.Fatalf("FindVmDeployNode exact-key match: unexpected error %v", err)
		}
		if !ok || node.From != "eval-vm" || len(node.Plan) != 1 || node.Plan[0].Check != "substrate step" {
			t.Errorf("FindVmDeployNode exact-key match = (%+v, %v), want check-substrate's own entry", node, ok)
		}
	})

	t.Run("no match at all is not an error", func(t *testing.T) {
		_, ok, err := FindVmDeployNode(deploys, "nonexistent", "nonexistent-base")
		if err != nil {
			t.Fatalf("FindVmDeployNode no-match: unexpected error %v", err)
		}
		if ok {
			t.Error("FindVmDeployNode no-match: ok = true, want false")
		}
	})

	t.Run("nil deploys map is not an error", func(t *testing.T) {
		_, ok, err := FindVmDeployNode(nil, "anything", "anything")
		if err != nil || ok {
			t.Errorf("FindVmDeployNode(nil) = (ok=%v, err=%v), want (false, nil)", ok, err)
		}
	})
}

// TestPathLeaf pins the leaf-extraction semantics — including the tolerant
// (non-error) handling of a malformed dotted path, which is what
// distinguishes it from SplitDottedPath (that one returns nil for the same
// input).
func TestPathLeaf(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"foo", "foo"},
		{"foo.bar.baz", "baz"},
		{"host", "host"},
		{"local", "local"},
		{"a..b", "b"}, // malformed (doubled dot) — still yields the raw trailing segment
	}
	for _, tc := range cases {
		if got := PathLeaf(tc.in); got != tc.want {
			t.Errorf("PathLeaf(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestClassifyNodeTarget covers the W4 pure-helpers relocation (moved from
// charly/bundle_add_cmd.go): node.Target wins when set; otherwise a
// ref-based deploy's path LEAF classifies "host"/"local" as the local
// target and everything else as pod.
func TestClassifyNodeTarget(t *testing.T) {
	cases := []struct {
		name string
		node *BundleNode
		path string
		want string
	}{
		{"node.Target wins", &BundleNode{Target: "vm"}, "anything", "vm"},
		{"nested node.Target wins over leaf", &BundleNode{Target: "k8s"}, "stack.web", "k8s"},
		{"nil node, literal host leaf -> local", nil, "host", "local"},
		{"nil node, literal local leaf -> local", nil, "local", "local"},
		{"nil node, nested host leaf -> local", nil, "stack.host", "local"},
		{"nil node, other leaf -> pod", nil, "my-app", "pod"},
		{"empty node, no Target, other leaf -> pod", &BundleNode{}, "my-app", "pod"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyNodeTarget(tc.node, tc.path); got != tc.want {
				t.Errorf("ClassifyNodeTarget(%+v, %q) = %q, want %q", tc.node, tc.path, got, tc.want)
			}
		})
	}
}
