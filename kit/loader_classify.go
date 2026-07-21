package kit

// loader_classify.go — the kind-blind config-loader document-shape classifier:
// DocShape + ClassifyDoc. Relocated to sdk/spec (FLOOR-SLIM axis-A mechanical
// batch, zero logic change) so charly core's materialize.go can call it
// without importing kit; aliased here so every other existing kit.ClassifyDoc
// / kit.DocShape* call site (charly's validate.go/unified.go, sdk/loaderkit's
// discover.go/walk.go) keeps compiling unchanged (R3 — ONE copy, in spec now).

import "github.com/opencharly/sdk/spec"

// DocShape classifies one parsed YAML document's top level.
type DocShape = spec.DocShape

const (
	// DocShapeEmpty — a scalar-null / empty mapping document (nothing to load).
	DocShapeEmpty = spec.DocShapeEmpty
	// DocShapeNode — the unified name-first node-form. See spec.DocShapeNode.
	DocShapeNode = spec.DocShapeNode
)

// ClassifyDoc inspects a document's top level and returns its shape. See spec.ClassifyDoc.
var ClassifyDoc = spec.ClassifyDoc
