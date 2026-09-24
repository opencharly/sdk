package loaderkit

// beds_test.go — the ONE bed resolver's unit tests: Beds() enumerates LOCAL +
// namespace-qualified beds (recursively), ResolveBed resolves both forms, and
// BedScope returns the OWNING namespace so a namespaced bed's bare `from:`
// resolves against its own scope. Locks the fix for the namespaced-bed
// validate-but-not-run defect.

import (
	"sort"
	"testing"

	"github.com/opencharly/spec/spec"
)

func TestBeds_EnumeratesLocalAndNamespaced(t *testing.T) {
	uf := fold()
	beds := uf.Beds()
	for _, want := range []string{"local-bed", "omarchy.ns-bed"} {
		if _, ok := beds[want]; !ok {
			t.Fatalf("Beds() missing %q; got keys %v", want, sortedBedKeys(beds))
		}
	}
	if _, ok := beds["ns-bed"]; ok {
		t.Fatal("Beds() leaked an unqualified namespace bed key")
	}
	// A non-disposable deploy is not a bed.
	uf.Deploy["not-a-bed"] = spec.DeployNode{}
	if _, ok := uf.Beds()["not-a-bed"]; ok {
		t.Fatal("Beds() included a non-disposable deploy")
	}
}

func TestResolveBed_Forms(t *testing.T) {
	uf := fold()
	if _, ok := uf.ResolveBed("local-bed"); !ok {
		t.Fatal("ResolveBed(local-bed) failed")
	}
	if _, ok := uf.ResolveBed("omarchy.ns-bed"); !ok {
		t.Fatal("ResolveBed(omarchy.ns-bed) failed")
	}
	if _, ok := uf.ResolveBed("ns-bed"); ok {
		t.Fatal("ResolveBed(ns-bed) resolved an unqualified namespace bed")
	}
	if _, ok := uf.ResolveBed("missing"); ok {
		t.Fatal("ResolveBed(missing) resolved")
	}
}

func TestBedScope_ReturnsOwningNamespace(t *testing.T) {
	uf := fold()
	scope, leaf := uf.BedScope("omarchy.ns-bed")
	if scope == nil || leaf != "ns-bed" {
		t.Fatalf("BedScope(omarchy.ns-bed) = (%v, %q), want (the omarchy namespace, ns-bed)", scope, leaf)
	}
	if scope != uf.Namespaces["omarchy"] {
		t.Fatal("BedScope returned the wrong namespace subtree")
	}
	// A local bed scopes to the root.
	scope, leaf = uf.BedScope("local-bed")
	if scope != uf || leaf != "local-bed" {
		t.Fatalf("BedScope(local-bed) = (%v, %q), want (root, local-bed)", scope, leaf)
	}
	if scope, _ := uf.BedScope("missing"); scope != nil {
		t.Fatal("BedScope(missing) returned a non-nil scope")
	}
}

func sortedBedKeys(m map[string]spec.DeployNode) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
