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

// TestPackagingRejectsScalar proves the candy-manifest packaging: field is CUE-CLOSED to the
// #Packaging shape (schema/candy.cue) — a scalar form is rejected at CUE decode time (struct vs
// string type mismatch), and a proper map decodes into CandyYAML.Packaging. The old localpkg:
// per-format map (deleted with the nFPM cutover) had the same closed-shape test; the packaging:
// section replaced it as the single source of package metadata. The decode path is the SAME
// DecodeEntityViaCUE every candy manifest goes through.
func TestPackagingRejectsScalar(t *testing.T) {
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

	if _, err := decode("name: t\npackaging: charly\n"); err == nil {
		t.Error("scalar packaging: should be rejected by CUE (#Packaging struct shape), got nil error")
	}

	ly, err := decode(`name: t
packaging:
  name: charly
  description: The charly CLI
  maintainer: OpenCharly
  variants:
    minimal:
      description: Minimal charly
      plugins: [command:box]
  formats:
    deb:
      depends: [curl]
`)
	if err != nil {
		t.Fatalf("map form should decode, got %v", err)
	}
	if ly.Packaging == nil {
		t.Fatal("decoded packaging = nil")
	}
	if ly.Packaging.Name != "charly" || ly.Packaging.Maintainer != "OpenCharly" {
		t.Errorf("decoded packaging = %+v, want name=charly maintainer=OpenCharly", ly.Packaging)
	}
	if ly.Packaging.Variants["minimal"] == nil || len(ly.Packaging.Variants["minimal"].Plugins) != 1 {
		t.Errorf("decoded variants = %+v, want minimal with one plugin", ly.Packaging.Variants)
	}
	if ly.Packaging.Formats["deb"] == nil || len(ly.Packaging.Formats["deb"].Depends) != 1 {
		t.Errorf("decoded formats = %+v, want deb with one dep", ly.Packaging.Formats)
	}
}
