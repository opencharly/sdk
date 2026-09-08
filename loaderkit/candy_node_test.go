// candy_node_test.go — the candy box⊻layer routing over the PARSED node (parser consolidation
// F2.1). MOVED from charly's bridge test files (node_candy_test.go / node_parsed_test.go) and
// rewritten against spec.ParsedNode fixtures: the genericNode bridge those files drove is
// deleted; CandyIsImage scans the canonical JSON body and BuildCandy decodes through the SAME
// DecodeNodeValue every candy/kind decode uses — so the parsed-node restatement is BY
// CONSTRUCTION byte-identical to the direct body decode (the former round-trip machine's
// guarantee, now structural).
package loaderkit

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// candyThreaded recognizes `candy` + `pod` as kinds, `pod` as a member-nesting kind, and the
// `file` plugin-verb scalar-shorthand primary — enough to parse the node-form candy fixtures.
var candyThreaded = spec.Threaded{
	Kinds:            map[string]bool{"candy": true, "pod": true},
	DeploySubstrates: map[string]bool{},
	StructuralKinds:  map[string]bool{"pod": true},
	Primaries:        map[string]string{"file": "file"},
}

// candyNodeFromYAML parses ONE single-entity node-form doc and returns its parsed node.
func candyNodeFromYAML(t *testing.T, doc string) spec.ParsedNode {
	t.Helper()
	var ydoc yaml.Node
	if err := yaml.Unmarshal([]byte(doc), &ydoc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	_, pp, err := ParseDoc(&ydoc, candyThreaded)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	if len(pp.Nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(pp.Nodes))
	}
	return pp.Nodes[0]
}

// TestCandyIsImage_BaseFromScan is the parsed-node restatement of the yaml-key scan: a candy
// body carrying `base:` (full IMAGE) or `from:` (builder-ref IMAGE) reports image; a LAYER
// fragment (neither), an EMPTY body and a non-mapping body do not.
func TestCandyIsImage_BaseFromScan(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"base marker", `{"base":"fedora","version":"2026.150.0000"}`, true},
		{"from marker", `{"from":"builder","description":"x"}`, true},
		{"layer fragment", `{"version":"2026.150.0000","package":["git"]}`, false},
		{"empty body", ``, false},
		{"non-mapping body", `"a scalar cross-ref"`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pn := spec.ParsedNode{Name: "n", Disc: "candy", Body: json.RawMessage(tc.body)}
			if got := CandyIsImage(pn); got != tc.want {
				t.Fatalf("CandyIsImage(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

// decodeCandyKindFirst decodes a kind-first candy BODY through the same CUE entity
// decoder (handles PackageItem string-coercion + shorthand).
func decodeCandyKindFirst(t *testing.T, body string) spec.CandyYAML {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("parsing kind-first candy: %v", err)
	}
	var c spec.CandyYAML
	if err := DecodeEntityViaCUE(&doc, reflect.TypeOf(spec.CandyYAML{}), &c, "kind-first"); err != nil {
		t.Fatalf("CUE-decoding kind-first candy: %v", err)
	}
	return c
}

// candyKindFirst is the candy BODY in the internal (desugared, wire) shape — scalars +
// composition refs + env map + package list + service + plan as candy FIELDS, with the plan
// step carrying the internal plugin/plugin_input envelope (the form the parse-time desugar
// produces and #Op validates). MOVED from charly's node_candy_test.go (parser consolidation F2.1).
const candyKindFirst = `
version: "2026.150.0000"
description: in-memory store
status: working
candy: [supervisord]
require: [python]
env:
  REDIS_DATA: /var/lib/redis
  REDIS_PORT: "6379"
package:
  - redis
  - redis-cli
service:
  - name: redis
    exec: /usr/bin/redis-server
plan:
  - check: the binary exists
    plugin: file
    plugin_input:
      file: /usr/bin/redis-server
`

// candyNodeForm is the SAME candy in the COMPACT authored node-form: the FULL body lives inline
// in the `candy:` discriminator value, and the plan step authors the `file:` plugin sugar the
// parse-time desugar rewrites into the internal envelope above. MOVED from node_candy_test.go.
const candyNodeForm = `
redis:
  candy:
    version: "2026.150.0000"
    description: in-memory store
    status: working
    candy: [supervisord]
    require: [python]
    env:
      REDIS_DATA: /var/lib/redis
      REDIS_PORT: "6379"
    package:
      - redis
      - redis-cli
    service:
      - name: redis
        exec: /usr/bin/redis-server
    plan:
      - check: the binary exists
        file: /usr/bin/redis-server
`

// TestBuildCandy_RoundTrip proves the parsed-node candy constructor (parse + desugar + decode —
// BuildCandy → DecodeNodeValue) produces EXACTLY the CandyYAML the direct body decode of the
// internal wire shape produces — the non-brittleness proof for the richest kind, restated over
// the parsed node (equal structs ⇒ every downstream consumer sees identical input from the
// authored compact form). Equal structs in a bare test binary PROVE the deleted round-trip
// machine's guarantee is now structural: BuildCandy IS the canonical decode, no bridge.
func TestBuildCandy_RoundTrip(t *testing.T) {
	want := decodeCandyKindFirst(t, candyKindFirst)

	pn := candyNodeFromYAML(t, candyNodeForm)
	if pn.Name != "redis" || pn.Disc != "candy" {
		t.Fatalf("parsed node = %q/%q, want redis/candy", pn.Name, pn.Disc)
	}
	name, ic, err := BuildCandy(pn)
	if err != nil {
		t.Fatalf("BuildCandy: %v", err)
	}
	if name != "redis" {
		t.Fatalf("candy name = %q, want redis", name)
	}
	// In node-form the candy NAME is the top-level key (BuildCandy stamps it);
	// the kind-first body fixture carries no `name:`, so reflect that here.
	want.Name = name
	if !reflect.DeepEqual(ic.CandyYAML, want) {
		t.Fatalf("node-form candy != kind-first candy\n node-form: %#v\n kind-first: %#v", ic.CandyYAML, want)
	}
}

// TestBuildCandy_NonCandyDisc rejects a non-candy discriminator with the same error the deleted
// genericNode buildCandy produced.
func TestBuildCandy_NonCandyDisc(t *testing.T) {
	pn := spec.ParsedNode{Name: "n", Disc: "pod", Body: json.RawMessage(`{"image":"codex"}`)}
	if _, _, err := BuildCandy(pn); err == nil {
		t.Fatal("BuildCandy on a pod node must error")
	}
}
