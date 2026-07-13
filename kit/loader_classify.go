package kit

// loader_classify.go — the kind-blind config-loader document-shape classifier:
// DocShape + ClassifyDoc. Relocated here (from charly/unified.go, where
// sdk/loaderkit had also grown a faithful-port duplicate) so charly core AND
// sdk/loaderkit share ONE copy (R3). Behavior-identical: same shapes, same
// error messages.

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// DocShape classifies one parsed YAML document's top level.
type DocShape int

const (
	// DocShapeEmpty — a scalar-null / empty mapping document (nothing to load).
	DocShapeEmpty DocShape = iota
	// DocShapeNode — the unified name-first node-form: reserved document
	// directives (version/import/discover/defaults/repo/provides) plus a flat
	// map of arbitrary-name entity nodes (each `<name>: {<discriminator>: …}`),
	// and NO top-level kind-map key. The ONE authoring surface; a legacy
	// kind-keyed / root-shape document is hard-rejected downstream (the host's
	// #NodeDoc CUE gate), not here.
	DocShapeNode
)

// ClassifyDoc inspects a document's top level and returns its shape: a non-empty
// mapping is a node-form document (arbitrary entity-name nodes and/or the reserved
// directives version/import/discover/…); a scalar-null / empty mapping is
// DocShapeEmpty; a non-mapping top level is an error. Entity + directive validation
// happens downstream (the loader parse + the #NodeDoc CUE gate).
func ClassifyDoc(node *yaml.Node) (DocShape, error) {
	if node == nil || node.Kind == 0 {
		return DocShapeEmpty, nil
	}
	// yaml.NewDecoder wraps content in a DocumentNode.
	inner := node
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return DocShapeEmpty, nil
		}
		inner = node.Content[0]
	}
	if inner.Kind == yaml.ScalarNode && inner.Tag == "!!null" {
		return DocShapeEmpty, nil
	}
	if inner.Kind != yaml.MappingNode {
		return 0, fmt.Errorf("top-level must be a mapping, got kind=%v", inner.Kind)
	}
	if len(inner.Content) == 0 {
		return DocShapeEmpty, nil
	}

	// A non-empty top-level mapping is a node-form document (arbitrary entity-name
	// nodes and/or the reserved directives version/import/discover/…). The bilingual
	// legacy-kind-map reader was deleted; entity + directive validation is downstream
	// (the loader parse + the #NodeDoc CUE gate).
	return DocShapeNode, nil
}
