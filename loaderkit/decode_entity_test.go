package loaderkit

// decode_entity_test.go — relocated from charly/listen_opaque_gate_test.go +
// charly/localpkg_test.go's TestLocalPkgMapRejectsScalar (K1 unit 1): both tests exercise the
// per-entity CUE decode mechanism directly (decode_entity.go), with zero charly-core dependency.
// Package loaderkit (white-box) — TestLibvirtListeners_OpaqueNotExpander asserts against the
// unexported cueShorthandExpanders map + implementsJSONUnmarshaler.

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// TestLibvirtListeners_OpaqueNotExpander locks the P1.4 fix: LibvirtGraphicsListeners
// is a json.Unmarshaler (so the CUE normalizer short-circuits it and cue.Value.Decode
// runs its tri-shape UnmarshalJSON on BOTH the YAML and the opaque substrate-decode
// path), and it is therefore NOT registered in cueShorthandExpanders — a re-added
// expander entry (or a lost UnmarshalJSON) would reintroduce the check-cross-vm-http
// regression.
func TestLibvirtListeners_OpaqueNotExpander(t *testing.T) {
	if !implementsJSONUnmarshaler(reflect.TypeOf(spec.LibvirtGraphicsListeners{})) {
		t.Fatal("LibvirtGraphicsListeners must implement json.Unmarshaler (the opaque-decode path)")
	}
	if _, ok := cueShorthandExpanders[reflect.TypeOf(spec.LibvirtGraphicsListeners{})]; ok {
		t.Fatal("LibvirtGraphicsListeners must NOT be in cueShorthandExpanders — its UnmarshalJSON serves both read paths")
	}
	// The scalar shorthand the vm web-vm bed uses decodes on the JSON (opaque) path.
	var ll spec.LibvirtGraphicsListeners
	if err := json.Unmarshal([]byte(`"127.0.0.1"`), &ll); err != nil {
		t.Fatalf("scalar listen shorthand must decode on the opaque path: %v", err)
	}
	if len(ll) != 1 || ll[0].Type != "address" || ll[0].Address != "127.0.0.1" {
		t.Fatalf("unexpected decode: %+v", ll)
	}
}

// TestLocalPkgMapRejectsScalar proves the candy-manifest localpkg: field is CUE-CLOSED to the
// per-format map shape (schema/candy.cue: `localpkg?: {pac?: string, rpm?: string, deb?:
// string}`) — a legacy scalar form is rejected at CUE decode time (struct vs string type
// mismatch), and the per-format map decodes into CandyYAML.LocalPkg. The rejection moved from a
// hand-written LocalPkgMap.UnmarshalYAML (deleted with *Candy) to the schema itself (SDD): the
// decode path is the SAME DecodeEntityViaCUE every candy manifest goes through.
func TestLocalPkgMapRejectsScalar(t *testing.T) {
	decode := func(body string) (spec.CandyYAML, error) {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
			t.Fatalf("parse: %v", err)
		}
		root := spec.MappingRoot(&doc)
		if root == nil {
			t.Fatalf("test candy body is not a mapping")
		}
		var ly spec.CandyYAML
		err := DecodeEntityViaCUE(root, reflect.TypeOf(spec.CandyYAML{}), &ly, "test-candy")
		return ly, err
	}

	if _, err := decode("name: t\nlocalpkg: pkg/arch\n"); err == nil {
		t.Error("scalar localpkg: should be rejected by CUE (per-format map shape), got nil error")
	}

	ly, err := decode("name: t\nlocalpkg:\n  pac: pkg/arch\n  rpm: pkg/fedora\n")
	if err != nil {
		t.Fatalf("map form should decode, got %v", err)
	}
	if ly.LocalPkg["pac"] != "pkg/arch" || ly.LocalPkg["rpm"] != "pkg/fedora" {
		t.Errorf("decoded map = %v", ly.LocalPkg)
	}
}
