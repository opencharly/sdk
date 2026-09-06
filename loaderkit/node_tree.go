package loaderkit

// node_tree.go — the entity-body assembly + fleet/resource-member tree-builder mechanism (K1
// unit 3b, relocated from charly/node_build.go + charly/node_fleet.go + charly/node_normalize.go).
// Operates on spec.ParsedNode (the wire-safe parsed-entity shape LoadUnified's parse already
// produces) rather than charly core's *genericNode: genericNode is a host-internal reconstruction
// consumed directly by charly/provider_kind_invoke.go's TRUE clause-M dispatch (candyIsImage /
// buildCandy, the box⊻layer bootstrap routing — clause B, permanently core), so it cannot itself
// move; but everything BELOW that dispatch layer — the entity-body assembler, the fleet-tree
// builder, the standalone-template shape detection — never needed genericNode's yaml.Node
// convenience wrapper specifically, only the SAME name/disc/body/children shape spec.ParsedNode
// already carries. The host constructs *genericNode ONLY where a call genuinely needs it
// (charly/provider_kind_invoke.go's foldCandyKind, immediately before candyIsImage/buildCandy);
// every other call in the materialize path threads spec.ParsedNode straight through, never
// round-tripping through genericNode at all.

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// discValue reconstructs the yaml.Node the entity's body decodes to — the SAME reconstruction
// charly/node_parsed.go's parsedNodeToGeneric performs for its genericNode.discValue field: pn.Body
// (already-canonical JSON) decoded through Go's empty interface, then yaml-encoded, so a scalar
// stays a scalar (any JSON scalar type, not just a string) and a mapping stays a mapping. Returns
// nil for an empty/absent body.
func discValue(pn spec.ParsedNode) (*yaml.Node, error) {
	if len(pn.Body) == 0 {
		return nil, nil
	}
	var asAny any
	if err := json.Unmarshal(pn.Body, &asAny); err != nil {
		return nil, fmt.Errorf("node %q: decode body: %w", pn.Name, err)
	}
	var dv yaml.Node
	if err := dv.Encode(asAny); err != nil {
		return nil, fmt.Errorf("node %q: encode body: %w", pn.Name, err)
	}
	return &dv, nil
}

// AssembleEntityBody returns the DOCUMENT-wrapped entity-body mapping to decode: pn's body value
// (an empty mapping when the value is null/absent or a scalar cross-ref like `vm: pg-vm`, which
// the constructor consumes separately via discValue).
func AssembleEntityBody(pn spec.ParsedNode) (*yaml.Node, error) {
	dv, err := discValue(pn)
	if err != nil {
		return nil, err
	}
	if dv == nil || dv.Kind != yaml.MappingNode {
		return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}, nil
	}
	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{dv}}
	if spec.MappingRoot(doc) == nil {
		return nil, fmt.Errorf("node %q: %q value must be a mapping", pn.Name, pn.Disc)
	}
	return doc, nil
}

// DecodeNodeValue decodes pn's body via the shared CUE entity decoder (decode_entity.go) into out
// (a *struct) — the SAME entity-body assembler + CUE decode every candy/kind/node-form decode
// goes through.
func DecodeNodeValue(pn spec.ParsedNode, out any) error {
	body, err := AssembleEntityBody(pn)
	if err != nil {
		return err
	}
	return DecodeEntityViaCUE(body, reflect.TypeOf(out).Elem(), out, "node "+pn.Name)
}

// EntityBodyJSON returns a node's kind-value mapping as canonical JSON, generically — with NO
// concrete-kind Go type. It is the single body→wire mechanism for both the op.Params plugin-kind
// path and the substrate TEMPLATE thread, so the kernel never types a spec.<Kind> merely to
// canonicalize a value. Reuses parse.go's entityBodyJSON (R3: the SAME yaml→map→JSON transform,
// already the parse's own body-serialization step — this is that same transform applied to a
// RECONSTRUCTED discValue rather than the freshly-parsed one).
func EntityBodyJSON(pn spec.ParsedNode) (json.RawMessage, error) {
	dv, err := discValue(pn)
	if err != nil {
		return nil, err
	}
	return entityBodyJSON(pn.Name, dv)
}

// BuildFleetNode recursively builds a FleetNode from a fleet/resource node. The discriminator
// value carries the deploy config; inline STEP children (checks) fold into the fleet's plan via
// DecodeNodeValue (the assembler); ENTITY children are RESOURCE members (deploy-into / alongside).
func BuildFleetNode(pn spec.ParsedNode, t spec.Threaded) (*spec.FleetNode, error) {
	var dn spec.FleetNode
	if err := DecodeNodeValue(pn, &dn); err != nil {
		return nil, err
	}
	// EDGE-INHERIT cutover B: the substrate kind at the EDGE is the target directly (no
	// inference from a cross-ref). group:/host: are targetless venues.
	dn.Target = FleetTargetForDisc(pn.Disc, t)
	// A scalar discriminator value (`vm: pg-vm` / `pod: img`) is the deploy's cross-ref: pod →
	// the image it runs; vm/kubernetes/local/android → the same-kind template it inherits (`from:`).
	dv, err := discValue(pn)
	if err != nil {
		return nil, err
	}
	if dv != nil && dv.Kind == yaml.ScalarNode {
		SetFleetCrossRef(&dn, pn.Disc, dv.Value, t)
	}
	// The mapping deploy form (`vm: {from: "base:golden"}`) decodes `from` into dn.From
	// via yaml DIRECTLY (bypassing SetFleetCrossRef) — normalize the unified name:tag split
	// here so BOTH authored forms produce From+FromSnapshot. Guarded to the non-ImageBacked
	// arm (an image ref with a `:` tag is never split); the scalar form is already split
	// (FromSnapshot set) and passes through untouched.
	if dn.From != "" && dn.FromSnapshot == "" && dn.Image == "" {
		dn.From, dn.FromSnapshot = splitVMSnapshotRef(dn.From)
	}

	children, err := BuildResourceMemberChildren(pn, t)
	if err != nil {
		return nil, err
	}
	for name, member := range children {
		// A targetless GROUP (no own workload target) places members ALONGSIDE (shared net →
		// Peer); a WORKLOAD places its resource children INSIDE its venue (deploy-into →
		// Nested).
		if dn.Target == "" {
			if dn.Members == nil {
				dn.Members = map[string]*spec.FleetNode{}
			}
			dn.Members[name] = member
		} else {
			if dn.Children == nil {
				dn.Children = map[string]*spec.FleetNode{}
			}
			dn.Children[name] = member
		}
	}
	return &dn, nil
}

// BuildResourceMemberChildren decodes pn's RESOURCE-MEMBER entity children into a name→*FleetNode
// map via the SAME BuildFleetNode recursion — the SINGLE source of truth for authored member-tree
// decode (R3). Every pn.Children entry is an entity child by construction (the parse-time desugar
// already separates step/data children into the plan/body fields before a spec.ParsedNode ever
// reaches here — see charly/node_parse.go), so no discClass filter is needed. A non-resource
// entity child is a hard error (deploy/resource children must be pod/vm/kubernetes/local/android/group).
func BuildResourceMemberChildren(pn spec.ParsedNode, t spec.Threaded) (map[string]*spec.FleetNode, error) {
	var out map[string]*spec.FleetNode
	for _, rk := range pn.Children {
		if !IsResourceDisc(rk.Disc, t) {
			return nil, fmt.Errorf("node %q: a %q child %q is not a resource member (deploy/resource children must be pod/vm/kubernetes/local/android)", pn.Name, rk.Disc, rk.Name)
		}
		member, err := BuildFleetNode(*rk, t)
		if err != nil {
			return nil, err
		}
		if out == nil {
			out = map[string]*spec.FleetNode{}
		}
		out[rk.Name] = member
	}
	return out, nil
}

// BuildFleetNodeInto builds pn into a FleetNode and registers it in the Deploy (fleet) map. acc
// is the K1-unit-1 spec.MaterializedProject accumulator — this function only ever touches the
// Fleet field.
func BuildFleetNodeInto(pn spec.ParsedNode, t spec.Threaded, acc *spec.MaterializedProject) error {
	dn, err := BuildFleetNode(pn, t)
	if err != nil {
		return err
	}
	if acc.Fleet == nil {
		acc.Fleet = map[string]spec.FleetNode{}
	}
	acc.Fleet[pn.Name] = *dn
	return nil
}

// IsDeployShape reports whether a substrate node is a DEPLOY (vs a standalone template): a scalar
// discriminator value (`vm: pg-vm` / `pod: img`) is a cross-ref deploy, and a mapping value
// carrying `from:` or `image:` is a deploy.
func IsDeployShape(pn spec.ParsedNode) bool {
	dv, err := discValue(pn)
	if err != nil || dv == nil {
		return false
	}
	if dv.Kind == yaml.ScalarNode {
		return dv.Value != ""
	}
	if dv.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(dv.Content); i += 2 {
			if k := dv.Content[i].Value; k == "from" || k == "image" {
				return true
			}
		}
	}
	return false
}

// DecodeStandaloneTemplateJSON canonicalizes pn (a substrate TEMPLATE node — no cross-ref, no
// resource members) to the JSON the host threads to candy/plugin-substrate (op.Env), GENERICALLY
// via EntityBodyJSON — with NO concrete-kind Go type.
func DecodeStandaloneTemplateJSON(pn spec.ParsedNode, t spec.Threaded) (json.RawMessage, error) {
	if !IsStandaloneResourceKind(pn.Disc, t) {
		return nil, fmt.Errorf("node %q: %q is not a standalone resource kind", pn.Name, pn.Disc)
	}
	return EntityBodyJSON(pn)
}

// ResourceChildren returns pn's children whose discriminator is itself a resource/fleet kind (the
// markers of a fleet-shaped node). The deployable set is the CUE-derived resourceKindSet
// (#ResourceKind) — the fixed vocab alone, not the registry-derived external-substrate extension
// (mirrors the original's own scope).
func ResourceChildren(pn spec.ParsedNode) []spec.ParsedNode {
	var out []spec.ParsedNode
	for _, ch := range pn.Children {
		if resourceKindSet[ch.Disc] {
			out = append(out, *ch)
		}
	}
	return out
}
