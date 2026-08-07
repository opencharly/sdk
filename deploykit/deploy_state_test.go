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
//   - SaveFleetConfig round-trip through a stub DeployStateHost (the 1-op
//     LoadUnifiedFleetConfig seam) + CHARLY_DEPLOY_CONFIG tempdir redirect + a stub
//     marshalNode callback (exercises LoadFleetConfig's fail-safe, the kit.LatestSchemaVersion
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
	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// --- ExportAllBox — the #67 keystone (#ResolvedProject envelope → FleetConfig) ---

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
		t.Fatal("ExportAllBox returned nil FleetConfig")
	}
	if len(dc.Fleet) != 1 {
		t.Fatalf("ExportAllBox produced %d entries; want 1 (the zero box must be skipped)", len(dc.Fleet))
	}
	entry, ok := dc.Fleet["web"]
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
	if !reflect.DeepEqual(dc.Fleet, dc2.Fleet) {
		t.Errorf("ExportAllBox is non-deterministic across calls")
	}
}

// TestExportAllBox_NilSafe proves the nil-receiver guard: a nil *spec.ResolvedProject
// yields an empty (non-nil) FleetConfig with a live Fleet map, so callers that range
// dc.Fleet never nil-deref.
func TestExportAllBox_NilSafe(t *testing.T) {
	dc := ExportAllBox(nil)
	if dc == nil {
		t.Fatal("ExportAllBox(nil) returned nil")
	}
	if dc.Fleet == nil {
		t.Fatal("ExportAllBox(nil) returned a FleetConfig with nil Fleet map")
	}
	if len(dc.Fleet) != 0 {
		t.Errorf("ExportAllBox(nil) produced %d entries; want 0", len(dc.Fleet))
	}
}

// --- SaveFleetConfig — the kind-blind file shell + callback round-trip ---

// TestSaveFleetConfig_RoundTrip exercises the full SaveFleetConfig write path: the
// fail-safe LoadFleetConfig re-check (through the 1-op LoadUnifiedFleetConfig seam), the
// kit.LatestSchemaVersion version stamp, the caller-supplied marshalNode callback per entry,
// and the atomic tempfile+os.Rename write. CHARLY_DEPLOY_CONFIG redirects the write to a
// tempdir so the test never touches the operator's real per-host overlay. The marshalNode
// stub emits a simple node-form body (the deploy-kind-specific marshal lives in
// charly/deploy_nodeform.go and is tested charly-side).
func TestSaveFleetConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "charly.yml")
	t.Setenv(kit.DeployConfigEnv, dest)

	// Stub the ONE host Mechanism SaveFleetConfig reaches through DeployStateHost (the
	// LoadUnified hop for the fail-safe re-check). The version stamp is kit.LatestSchemaVersion
	// (a direct kit call, not a seam op); the marshal is the caller's callback.
	stub := &StateHostMechanisms{
		LoadUnifiedFleetConfig: func(configDir string) (*FleetConfig, error) {
			return nil, nil // absent file → LoadFleetConfig returns (empty, nil) → fail-safe passes
		},
	}
	prev := DeployStateHost
	RegisterDeployStateHost(stub)
	t.Cleanup(func() { DeployStateHost = prev })

	dc := &FleetConfig{
		Fleet: map[string]FleetNode{
			"web": {
				Image: "web",
				Env:   map[string]string{"LOG_LEVEL": "info"},
			},
		},
	}

	// stubMarshalNode emits a node-form body: a mapping with the discriminator + the
	// struct-marshaled fields (a faithful miniature of charly's marshalFleetNode).
	stubMarshalNode := func(name string, node *FleetNode) (*yaml.Node, error) {
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

	if err := SaveFleetConfig(dc, stubMarshalNode, nil); err != nil {
		t.Fatalf("SaveFleetConfig: %v", err)
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
		t.Errorf("overlay file not present at %s after SaveFleetConfig", dest)
	}
}

// TestSaveFleetConfig_ErrorsWhenCallbackNil pins the nil-callback guard: SaveFleetConfig
// errors clearly when marshalNode is nil (the deploy-kind-specific marshal is the caller's
// responsibility — a nil callback would nil-deref inside the per-entry loop).
func TestSaveFleetConfig_ErrorsWhenCallbackNil(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(kit.DeployConfigEnv, filepath.Join(dir, "charly.yml"))
	prev := DeployStateHost
	DeployStateHost = nil // no fail-safe re-check dep when the seam is nil
	t.Cleanup(func() { DeployStateHost = prev })

	err := SaveFleetConfig(&FleetConfig{Fleet: map[string]FleetNode{"x": {Image: "x"}}}, nil, nil)
	if err == nil {
		t.Fatal("SaveFleetConfig with nil callback returned nil; want an error")
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
	h := &StateHostMechanisms{LoadUnifiedFleetConfig: func(string) (*FleetConfig, error) { return nil, nil }}
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
	entries := []spec.EnvProvideEntry{
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
	entries := []spec.EnvProvideEntry{
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

// TestFindFleetNode covers the whole-tree name search (Cutover B unit 5, P13-KERNEL-B —
// moved from charly/k3s_post.go's findFleetNodeByName/findFleetNodePtrByName, which had
// no dedicated test coverage in charly; this closes that gap at the new home). A node may
// be found at the top level, nested under Children at any depth, or nested under Members
// at any depth; a name present nowhere in the tree returns nil.
func TestFindFleetNode(t *testing.T) {
	leaf := &FleetNode{Image: "leaf-image"}
	member := &FleetNode{Image: "member-image"}
	child := &FleetNode{
		Image:    "child-image",
		Children: map[string]*FleetNode{"leaf": leaf},
	}
	root := FleetNode{
		Image:    "root-image",
		Children: map[string]*FleetNode{"child": child},
		Members:  map[string]*FleetNode{"sidecar": member},
	}
	fleet := map[string]FleetNode{"stack": root}

	if got := FindFleetNode(fleet, "stack"); got == nil || got.Image != "root-image" {
		t.Errorf("FindFleetNode(stack) top-level = %v; want root-image", got)
	}
	if got := FindFleetNode(fleet, "child"); got != child {
		t.Errorf("FindFleetNode(child) nested-Children = %v; want %v", got, child)
	}
	if got := FindFleetNode(fleet, "leaf"); got != leaf {
		t.Errorf("FindFleetNode(leaf) nested-two-deep-Children = %v; want %v", got, leaf)
	}
	if got := FindFleetNode(fleet, "sidecar"); got != member {
		t.Errorf("FindFleetNode(sidecar) nested-Members = %v; want %v", got, member)
	}
	if got := FindFleetNode(fleet, "nonexistent"); got != nil {
		t.Errorf("FindFleetNode(nonexistent) = %v; want nil", got)
	}
	if got := FindFleetNode(nil, "anything"); got != nil {
		t.Errorf("FindFleetNode(nil fleet) = %v; want nil", got)
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
	deploys := map[string]FleetNode{
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
// charly/fleet_add_cmd.go): node.Target wins when set; otherwise a
// ref-based deploy's path LEAF classifies "host"/"local" as the local
// target and everything else as pod.
func TestClassifyNodeTarget(t *testing.T) {
	cases := []struct {
		name string
		node *FleetNode
		path string
		want string
	}{
		{"node.Target wins", &FleetNode{Target: "vm"}, "anything", "vm"},
		{"nested node.Target wins over leaf", &FleetNode{Target: "k8s"}, "stack.web", "k8s"},
		{"nil node, literal host leaf -> local", nil, "host", "local"},
		{"nil node, literal local leaf -> local", nil, "local", "local"},
		{"nil node, nested host leaf -> local", nil, "stack.host", "local"},
		{"nil node, other leaf -> pod", nil, "my-app", "pod"},
		{"empty node, no Target, other leaf -> pod", &FleetNode{}, "my-app", "pod"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyNodeTarget(tc.node, tc.path); got != tc.want {
				t.Errorf("ClassifyNodeTarget(%+v, %q) = %q, want %q", tc.node, tc.path, got, tc.want)
			}
		})
	}
}

// TestSaveDeployState_PluginSideReader pins the #55 K4 config-write seam-collapse: a NON-NIL
// injected reader makes SaveDeployState persist deploy-state even when DeployStateHost is nil —
// the OUT-OF-PROCESS command:fleet case, where the write now runs plugin-side (deploykit.SaveDeployState
// with the plugin's own loader-backed reader + loader-threaded Primaries) instead of over the deleted
// "deploy-config-save-state" host seam. Without the reader param, SaveDeployState returns early at
// `if DeployStateHost == nil` and writes NOTHING, so this test FAILS on the pre-refactor code
// (check-coverage gate).
func TestSaveDeployState_PluginSideReader(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "charly.yml")
	t.Setenv(kit.DeployConfigEnv, dest)
	prev := DeployStateHost
	DeployStateHost = nil // simulate out-of-process: no charly-init host registration
	t.Cleanup(func() { DeployStateHost = prev })

	// The plugin's own loader-backed reader — a fresh, non-nil overlay (nothing on disk yet).
	reader := func() (*FleetConfig, error) {
		return &FleetConfig{Fleet: map[string]FleetNode{}}, nil
	}
	marshalNode := func(_ string, _ *FleetNode) (*yaml.Node, error) {
		content := &yaml.Node{Kind: yaml.MappingNode}
		content.Content = append(content.Content, kit.ScalarNode("pod"), &yaml.Node{Kind: yaml.MappingNode})
		return content, nil
	}

	SaveDeployState("web", "", SaveDeployStateInput{Box: "web", Target: "pod"}, marshalNode, reader)

	if !kit.FileExists(dest) {
		t.Fatalf("SaveDeployState with a non-nil reader wrote nothing at %s (DeployStateHost==nil); the injected reader must bypass the host-only guard", dest)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading written overlay: %v", err)
	}
	if !strings.Contains(string(data), "web:") {
		t.Errorf("written overlay missing the deploy entry:\n%s", string(data))
	}
}
