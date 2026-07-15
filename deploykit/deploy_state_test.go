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
//   - MarshalBundleNodeLegacy round-trip (target/nested/peer re-injection + descent drop).
//   - SaveBundleConfig round-trip through a stub DeployStateHost + CHARLY_DEPLOY_CONFIG
//     tempdir redirect (exercises LoadBundleConfig's fail-safe, LatestSchemaVersion,
//     MigrateDeployEntity, the atomic tempfile+rename write).
//   - RegisterDeployStateHost seam (the charly init hook).
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

// --- MarshalBundleNodeLegacy — the structural-field round-trip ---

// TestMarshalBundleNodeLegacy_ReInjectsStructuralFields is the per-host overlay writer
// body test: MarshalBundleNodeLegacy re-injects the structural fields (target / nested /
// peer) that the node-form serializer expects, recurses into children/members, and drops
// the loader-derived descent venue descriptor (a stored descent trips #DeployValue's
// descent?: _|_ on reload). The returned *yaml.Node body is exactly what MigrateDeployEntity
// consumes (the charly-side legacy→node-form transform reconciles it into clean node-form),
// so the test inspects the NODE body directly — the contract the SDK-level function owns.
func TestMarshalBundleNodeLegacy_ReInjectsStructuralFields(t *testing.T) {
	child := &spec.Deploy{Image: "redis"}
	member := &spec.Deploy{Image: "sidecar"}
	node := spec.Deploy{
		Image:    "web",
		Target:   "vm",
		Env:      map[string]string{"LOG_LEVEL": "info"},
		Children: map[string]*spec.Deploy{"db": child},
		Members:  map[string]*spec.Deploy{"peer1": member},
		Descent:  &spec.DescentDescriptor{Venue: "parent"}, // must be DROPPED by DropMappingKey
	}

	body, err := MarshalBundleNodeLegacy(&node)
	if err != nil {
		t.Fatalf("MarshalBundleNodeLegacy: %v", err)
	}
	if body == nil || body.Kind != yaml.MappingNode {
		t.Fatalf("MarshalBundleNodeLegacy returned %v; want a MappingNode", body)
	}

	// Walk the mapping's key/value pairs and index them by key string.
	keys := mappingKeys(body)
	for _, want := range []string{"target", "nested", "peer", "env"} {
		if _, ok := keys[want]; !ok {
			t.Errorf("body missing re-injected key %q; present keys: %v", want, keys)
		}
	}
	if _, ok := keys["descent"]; ok {
		t.Errorf("body still carries `descent` (DropMappingKey failed); present keys: %v", keys)
	}

	// target value must be "vm".
	if tv := mappingValueString(body, "target"); tv != "vm" {
		t.Errorf("body[target] = %q; want vm", tv)
	}
	// nested must contain the "db" child; peer must contain "peer1".
	if !mappingHasChildKey(body, "nested", "db") {
		t.Errorf("body[nested] missing the db child; keys: %v", keys)
	}
	if !mappingHasChildKey(body, "peer", "peer1") {
		t.Errorf("body[peer] missing the peer1 member; keys: %v", keys)
	}
	// env must carry LOG_LEVEL=info (the non-structural field marshals normally).
	if ev := mappingValueString(body, "env"); ev != "" && !strings.Contains(ev, "LOG_LEVEL") {
		t.Errorf("body[env] = %q; want it to carry LOG_LEVEL", ev)
	}
}

// mappingKeys returns the set of key strings in a YAML mapping node.
func mappingKeys(m *yaml.Node) map[string]*yaml.Node {
	out := map[string]*yaml.Node{}
	if m == nil || m.Kind != yaml.MappingNode {
		return out
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		out[m.Content[i].Value] = m.Content[i+1]
	}
	return out
}

func mappingValueString(m *yaml.Node, key string) string {
	v := mappingKeys(m)[key]
	if v == nil {
		return ""
	}
	if v.Kind == yaml.ScalarNode {
		return v.Value
	}
	data, _ := yaml.Marshal(v)
	return string(data)
}

func mappingHasChildKey(m *yaml.Node, key, child string) bool {
	v := mappingKeys(m)[key]
	if v == nil || v.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(v.Content); i += 2 {
		if v.Content[i].Value == child {
			return true
		}
	}
	return false
}

// --- SaveBundleConfig — the atomic write round-trip through the DeployStateHost seam ---

// TestSaveBundleConfig_RoundTrip exercises the full SaveBundleConfig write path: the
// fail-safe LoadBundleConfig re-check, the LatestSchemaVersion version stamp, the
// MigrateDeployEntity per-entry transform, MarshalBundleNodeLegacy on each entry, and the
// atomic tempfile+os.Rename write — all reaching core through the DeployStateHost seam
// (the SDK cannot import charly's LoadUnified/LatestSchemaVersion/migrateDeployEntity).
// CHARLY_DEPLOY_CONFIG redirects the write to a tempdir so the test never touches the
// operator's real per-host overlay.
func TestSaveBundleConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "charly.yml")
	t.Setenv(kit.DeployConfigEnv, dest)

	// Stub the host Mechanisms SaveBundleConfig reaches through DeployStateHost.
	stub := &StateHostMechanisms{
		LoadUnifiedBundleConfig: func(configDir string) (*BundleConfig, error) {
			return nil, nil // absent file → LoadBundleConfig returns (empty, nil) → fail-safe passes
		},
		LatestSchemaVersion: func() string { return "2026.196.0000" },
		MigrateDeployEntity: func(body *yaml.Node) *yaml.Node { return body }, // identity: observe the node-form body verbatim
	}
	prev := DeployStateHost
	RegisterDeployStateHost(stub)
	t.Cleanup(func() { DeployStateHost = prev })

	dc := &BundleConfig{
		Bundle: map[string]BundleNode{
			"web": {
				Image:    "web",
				Target:   "vm",
				Env:      map[string]string{"LOG_LEVEL": "info"},
				Children: map[string]*spec.Deploy{"db": {Image: "redis"}},
			},
		},
	}

	if err := SaveBundleConfig(dc); err != nil {
		t.Fatalf("SaveBundleConfig: %v", err)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading written overlay %s: %v", dest, err)
	}
	got := string(data)
	for _, want := range []string{"version:", "2026.196.0000", "web:", "target: vm", "nested:", "db:", "LOG_LEVEL"} {
		if !strings.Contains(got, want) {
			t.Errorf("written overlay missing %q:\n%s", want, got)
		}
	}

	// The file must exist at the redirected path (atomic rename landed).
	if !kit.FileExists(dest) {
		t.Errorf("overlay file not present at %s after SaveBundleConfig", dest)
	}
}

// TestSaveBundleConfig_ErrorsWhenHostNotRegistered pins the nil-safe guard: with NO
// DeployStateHost registered (a read-only SDK consumer that never writes the per-host
// ledger), SaveBundleConfig errors clearly instead of silently no-op'ing or writing a
// versionless file.
func TestSaveBundleConfig_ErrorsWhenHostNotRegistered(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(kit.DeployConfigEnv, filepath.Join(dir, "charly.yml"))
	prev := DeployStateHost
	DeployStateHost = nil
	t.Cleanup(func() { DeployStateHost = prev })

	err := SaveBundleConfig(&BundleConfig{Bundle: map[string]BundleNode{"x": {Image: "x"}}})
	if err == nil {
		t.Fatal("SaveBundleConfig with nil DeployStateHost returned nil; want an error")
	}
}

// --- RegisterDeployStateHost — the charly init seam ---

// TestRegisterDeployStateHost pins the seam contract: a non-nil host is stored; a nil
// argument is ignored (the existing registration survives — charly registers once at
// init and a stray nil re-registration must not wipe it).
func TestRegisterDeployStateHost(t *testing.T) {
	prev := DeployStateHost
	t.Cleanup(func() { DeployStateHost = prev })

	DeployStateHost = nil
	h := &StateHostMechanisms{LatestSchemaVersion: func() string { return "x" }}
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
