// candy_node.go — the candy box⊻layer routing restated on the PARSED node (parser
// consolidation F2.1). The former charly-core genericNode candy constructor (node_candy.go: a
// yaml-discValue key scan + a gn→pn re-conversion) is DELETED; this is the ONE implementation:
// CandyIsImage scans the canonical JSON body (pn.Body) for the box base⊻from marker, and
// BuildCandy decodes through the SAME shared CUE entity decoder (DecodeNodeValue) every other
// candy/kind decode goes through. Both are pure functions of the wire-safe spec.ParsedNode — no
// genericNode reconstruction exists anywhere in the loader or the host anymore.
package loaderkit

import (
	"encoding/json"
	"fmt"

	"github.com/opencharly/spec/spec"
)

// CandyIsImage reports whether a candy: node is a full IMAGE (the former box:): its authored
// JSON body carries the box base⊻from marker — `base:` (an external base) or `from:` (a
// builder ref). A LAYER fragment has neither. This is the parsed-node restatement of the deleted
// genericNode yaml-key scan (a JSON-body scan for base/from; genericNode.discValue's mapping
// content walk is gone). The dedicated candy KindProvider calls it in-proc to pick uf.Box vs
// uf.Candy, and the discovered-candy pre-check uses it to distinguish a lazy LAYER ref from an
// eager IMAGE decode.
func CandyIsImage(pn spec.ParsedNode) bool {
	var body map[string]json.RawMessage
	if len(pn.Body) == 0 || json.Unmarshal([]byte(pn.Body), &body) != nil {
		// A non-mapping body (a scalar cross-ref / an empty node) is not an image by
		// construction — matches the old nil/non-mapping discValue arm.
		return false
	}
	for _, marker := range []string{"base", "from"} {
		if _, ok := body[marker]; ok {
			return true
		}
	}
	return false
}

// BuildCandy turns a candy-discriminator parsed node into an InlineCandy — decoded via the SAME
// shared CUE entity decoder (DecodeNodeValue) every candy/kind decode goes through. The candy
// name is the node name (the entity-map key), stamped onto the returned InlineCandy (the
// node-form body carries no name:). The parsed-node restatement of the deleted genericNode
// re-conversion (buildCandy, node_candy.go).
func BuildCandy(pn spec.ParsedNode) (string, *spec.InlineCandy, error) {
	if pn.Disc != "candy" {
		return "", nil, fmt.Errorf("buildCandy: node %q is not a candy (disc %q)", pn.Name, pn.Disc)
	}
	// Decode-ONLY at load (fast, runs on every invocation): the full closed-schema
	// CUE validation (CalVer/enum/unknown-key checks) runs at `charly box validate`
	// (the loader seam's ValidateCandyManifestCUE), not here — matching the legacy parseCandyYAML.
	var c spec.CandyYAML
	if err := DecodeNodeValue(pn, &c); err != nil {
		return "", nil, err
	}
	// Name is the node KEY in node-form (the migration moves a legacy body
	// `name:` up to the key), so stamp it — the decoded body carries no `name:`.
	c.Name = pn.Name
	return pn.Name, &spec.InlineCandy{CandyYAML: c}, nil
}
