package loaderkit

// node_validate.go — the box-validate entity-tree walk (K1 unit 3c, relocated from
// charly/cue_schema.go, completing the K1 unit 2 deferral): assembleAndValidateEntitySteps validates
// one entity's plan steps against the closed #Step; ValidateEntityNodeRec recurses the parsed tree,
// additionally validating a candy LAYER node's desugared body against #CandyValue concretely;
// ValidateNodeFormSteps parses a node-form document (via the host-supplied DocParser + Threaded
// snapshot — registry-derived DATA the host builds, never queried live from here) and walks every
// top-level node; ValidateCandyManifestCUE is the `charly box validate` entry point for one candy
// manifest file: the whole-document #NodeDoc structural gate, then the entity-tree walk.
//
// Zero registry coupling of its own — the ONLY host-coupled inputs are t (the Threaded snapshot,
// already built) and parser (the resolved DocParser), both supplied by the caller exactly like
// WalkSeams/MaterializeSeams thread host-built DATA to a kind-blind mechanism.

import (
	"fmt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// assembleAndValidateEntitySteps folds an entity node's step children into a plan: sequence and
// types EACH step against the closed #Step (which embeds the closed #Op). This is the ONLY
// validation that sees plan-STEP Op fields: node-form steps are sibling nodes, so the #NodeDoc
// whole-document gate accepts them as `_`, and the post-decode struct has already dropped unknown
// keys. So an unknown Op field or a bad enum on a step is a hard error here. We validate the
// STEPS, not the whole entity against its #Kind: a deploy entity (a `vm:`/`pod:` block carrying
// disposable/lifecycle/from/install_opts) mixes deploy-envelope fields the workload #Kind does not
// model — those are gated by #NodeDoc's deploy arm, not here. plugin_input: stays open (a plugin
// step's params are validated by the plugin's own spliced schema, not base #Op).
func assembleAndValidateEntitySteps(pn spec.ParsedNode, label string) error {
	body, err := AssembleEntityBody(pn)
	if err != nil {
		return fmt.Errorf("%s: assemble: %w", label, err)
	}
	b, err := yaml.Marshal(body)
	if err != nil {
		return fmt.Errorf("%s: marshal: %w", label, err)
	}
	v, err := CueDocFromYAML(label, b)
	if err != nil {
		return err
	}
	plan := v.LookupPath(cue.ParsePath("plan"))
	if !plan.Exists() {
		return nil // no steps to type
	}
	stepDef, err := schemaDef(label, "#Step")
	if err != nil {
		return err
	}
	iter, lerr := plan.List()
	if lerr != nil {
		return nil // plan not a sequence — structure is gated by #NodeDoc
	}
	for i := 0; iter.Next(); i++ {
		if verr := iter.Value().Unify(stepDef).Validate(); verr != nil {
			return fmt.Errorf("%s: plan step %d: %s", label, i, errors.Details(verr, nil))
		}
	}
	return nil
}

// ValidateEntityNodeRec assemble-validates one entity node (when its kind is CUE-registered) and
// recurses into its sub-entity children (bundle members, nested deploys), which carry their own
// steps. A candy node's DESUGARED body is additionally validated concretely against #CandyValue
// (version+description required, unknown inline fields rejected) — the box-validate counterpart of
// the load-time host-side validateKindValueCUE (which is closedness-only). Every pn.Children entry
// is an entity child by construction (the parse-time desugar already separates step/data children
// into the plan/body fields before a spec.ParsedNode ever reaches here), so no discClass filter is
// needed.
func ValidateEntityNodeRec(pn spec.ParsedNode, path string) error {
	if err := assembleAndValidateEntitySteps(pn, fmt.Sprintf("%s: %s", path, pn.Name)); err != nil {
		return err
	}
	if pn.Disc == "candy" {
		body, err := AssembleEntityBody(pn)
		if err != nil {
			return fmt.Errorf("%s: %s: assemble: %w", path, pn.Name, err)
		}
		// The concrete gate covers LAYER manifests only (the pre-cutover
		// ValidateCandyManifestCUE scope): an IMAGE entity (base:/from:) mixes
		// build fields that stay non-concrete until merge and is gated by the
		// #NodeDoc structural pass + decode validation instead.
		if m := spec.MappingRoot(body); m != nil {
			for i := 0; i+1 < len(m.Content); i += 2 {
				if k := m.Content[i].Value; k == "base" || k == "from" {
					return nil
				}
			}
		}
		b, err := yaml.Marshal(body)
		if err != nil {
			return fmt.Errorf("%s: %s: marshal: %w", path, pn.Name, err)
		}
		cv, err := CueDocFromYAML(fmt.Sprintf("%s: %s", path, pn.Name), b)
		if err != nil {
			return err
		}
		cdef, err := schemaDef(path, "#CandyValue")
		if err != nil {
			return err
		}
		if verr := cv.Unify(cdef).Validate(cue.Concrete(true)); verr != nil {
			return fmt.Errorf("%s: candy %q: %s", path, pn.Name, errors.Details(verr, nil))
		}
	}
	for _, ch := range pn.Children {
		if err := ValidateEntityNodeRec(*ch, path); err != nil {
			return err
		}
	}
	return nil
}

// ValidateNodeFormSteps parses a node-form document and validates EVERY entity's (and nested
// sub-entity's) assembled body against its closed per-kind def — the step-typo gate for candies,
// boxes, pods, deploys, and check beds alike. Shared by ValidateCandyManifestCUE and the host's
// validateProjectCUESchemas (R3). t/parser are host-supplied (the registry-derived Threaded
// snapshot + the resolved DocParser) — this function never queries the registry itself.
func ValidateNodeFormSteps(path string, data []byte, t spec.Threaded, parser spec.DocParser) error {
	var ydoc yaml.Node
	if err := yaml.Unmarshal(data, &ydoc); err != nil {
		return fmt.Errorf("%s: yaml: %w", path, err)
	}
	_, pp, err := parser.ParseDoc(&ydoc, t)
	if err != nil {
		return fmt.Errorf("%s: parse: %w", path, err)
	}
	for i := range pp.Nodes {
		if verr := ValidateEntityNodeRec(pp.Nodes[i], path); verr != nil {
			return verr
		}
	}
	return nil
}

// ValidateCandyManifestCUE validates a candy manifest. A legacy kind-keyed manifest validates the
// WHOLE document against #NodeDoc (the structural gate), then walks the parsed + DESUGARED node
// tree: each candy node's assembled body validates against #CandyValue concretely and every
// entity's plan steps type against the closed #Step (ValidateNodeFormSteps → ValidateEntityNodeRec)
// — the desugared tree is the validation subject, never the raw sugar bytes.
func ValidateCandyManifestCUE(path string, data []byte, t spec.Threaded, parser spec.DocParser) error {
	doc, err := CueDocFromYAML(path, data)
	if err != nil {
		return err
	}
	def, err := schemaDef(path, "#NodeDoc")
	if err != nil {
		return err
	}
	if verr := doc.Unify(def).Validate(cue.Concrete(true)); verr != nil {
		return fmt.Errorf("%s: %s", path, errors.Details(verr, nil))
	}
	// #NodeDoc gates the node-form STRUCTURE but accepts each entity's body as `_`;
	// ValidateNodeFormSteps parses (and thereby DESUGARS) the tree, types every entity's plan
	// steps against the closed #Step/#Op, and concretely validates each candy node's body against
	// #CandyValue.
	return ValidateNodeFormSteps(path, data, t, parser)
}
