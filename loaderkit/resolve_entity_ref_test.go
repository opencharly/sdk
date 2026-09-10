package loaderkit

// resolve_entity_ref_test.go — the reference-resolution surface's unit tests:
// every ref shape (local template, namespace-qualified template, local bed
// hop, namespace-qualified bed hop, missing-red) resolves identically for the
// load-time validator and the runtime resolver (the ONE surface, R3).

import (
	"encoding/json"
	"testing"

	"github.com/opencharly/spec/spec"
)

// fold builds a UnifiedFile with a local vm template, a local bed, and a
// namespace carrying its own vm template + bed (the git-linked import shape).
func boolPtr(b bool) *bool { return &b }

func fold() *spec.UnifiedFile {
	vmBody := json.RawMessage(`{"vm":{"source":{"kind":"iso"}}}`)
	ns := &spec.UnifiedFile{
		PluginKinds: map[string]map[string]json.RawMessage{
			"vm": {"ns-vm": vmBody},
		},
		Deploy: map[string]spec.DeployNode{
			"ns-bed": {From: "ns-vm", Disposable: boolPtr(true)},
		},
	}
	return &spec.UnifiedFile{
		PluginKinds: map[string]map[string]json.RawMessage{
			"vm": {"local-vm": vmBody},
		},
		Deploy: map[string]spec.DeployNode{
			"local-bed": {From: "local-vm", Disposable: boolPtr(true)},
		},
		Namespaces: map[string]*spec.UnifiedFile{"omarchy": ns},
	}
}

func TestResolveEntityRef_Shapes(t *testing.T) {
	uf := fold()
	cases := []struct {
		name string
		kind string
		ref  string
		want bool
	}{
		{"local template", "vm", "local-vm", true},
		{"namespace-qualified template", "vm", "omarchy.ns-vm", true},
		{"local bed hop", "vm", "local-bed", true},
		{"namespace-qualified bed hop", "vm", "omarchy.ns-bed", true},
		{"missing template", "vm", "nope", false},
		{"missing bed", "vm", "omarchy.nope", false},
		{"empty ref", "vm", "", false},
		{"nil fold", "vm", "local-vm", false},
	}
	for _, c := range cases {
		var got bool
		if c.name == "nil fold" {
			got = ResolveEntityRef(nil, c.kind, c.ref)
		} else {
			got = ResolveEntityRef(uf, c.kind, c.ref)
		}
		if got != c.want {
			t.Fatalf("%s: ResolveEntityRef(%s, %q) = %v, want %v", c.name, c.kind, c.ref, got, c.want)
		}
	}
}

func TestResolveEntityRef_QualifiedBedHop(t *testing.T) {
	// The qualified bed hop must resolve through the namespace's OWN bed set
	// (the unqualified name in the sub-fold), not the local set.
	uf := fold()
	if !ResolveEntityRef(uf, "vm", "omarchy.ns-bed") {
		t.Fatal("qualified bed hop did not resolve")
	}
	// The unqualified form must NOT leak into the namespace (a local-only ref).
	if ResolveEntityRef(uf, "vm", "ns-bed") {
		t.Fatal("unqualified namespace bed leaked into the local scope")
	}
}

func TestDeployTargetEntity_Qualified(t *testing.T) {
	uf := fold()
	// The qualified bed-hop resolves to the qualified template name.
	target, ok := DeployTargetEntity(uf, "omarchy.ns-bed")
	if !ok || target != "omarchy.ns-vm" {
		t.Fatalf("DeployTargetEntity(omarchy.ns-bed) = %q, %v; want omarchy.ns-vm, true", target, ok)
	}
	// The qualified template resolves directly.
	if target, ok := DeployTargetEntity(uf, "omarchy.ns-vm"); !ok || target != "omarchy.ns-vm" {
		t.Fatalf("DeployTargetEntity(omarchy.ns-vm) = %q, %v; want omarchy.ns-vm, true", target, ok)
	}
	// The unqualified form stays local-only.
	if _, ok := DeployTargetEntity(uf, "ns-bed"); ok {
		t.Fatal("unqualified namespace bed leaked into the local scope")
	}
}

func TestResolveKindEntityBody_Qualified(t *testing.T) {
	uf := fold()
	if body, ok := ResolveKindEntityBody(uf, "vm", "omarchy.ns-vm"); !ok || len(body) == 0 {
		t.Fatal("qualified vm body did not resolve")
	}
	if body, ok := ResolveKindEntityBody(uf, "vm", "local-vm"); !ok || len(body) == 0 {
		t.Fatal("local vm body did not resolve")
	}
	if _, ok := ResolveKindEntityBody(uf, "vm", "ns-vm"); ok {
		t.Fatal("unqualified namespace body leaked into the local scope")
	}
}
