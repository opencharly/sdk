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
//
// The member-carrier keys are OMITTED: an in-substrate member's key was authored inside the disc
// body (the parse keeps it there as the fold's position channel), but the member tree owns the
// value — every body EMITTER (this reconstruction feeding DecodeNodeValue/AssembleEntityBody/
// EntityBodyJSON) hands the closed per-kind schema gates a body without it, which is how substrate
// closedness stops rejecting resource-member keys (Cutover C task 0). BuildResourceMemberChildren
// reads pn.Body RAW for the position stamp.
func discValue(pn spec.ParsedNode) (*yaml.Node, error) {
	if len(pn.Body) == 0 {
		return nil, nil
	}
	var asAny any
	if err := json.Unmarshal(pn.Body, &asAny); err != nil {
		return nil, fmt.Errorf("node %q: decode body: %w", pn.Name, err)
	}
	if asMap, ok := asAny.(map[string]any); ok && len(pn.Children) > 0 {
		for _, ch := range pn.Children {
			delete(asMap, ch.Name)
		}
		if len(asMap) == 0 {
			asMap = map[string]any{} // keep an empty (non-nil) mapping body
		}
		asAny = asMap
	}
	var dv yaml.Node
	if err := dv.Encode(asAny); err != nil {
		return nil, fmt.Errorf("node %q: encode body: %w", pn.Name, err)
	}
	return &dv, nil
}

// authoredBodyKeys returns the TOP-LEVEL keys of pn's authored disc body (the RAW pn.Body —
// before the member-carrier strip) — the position channel: a member child whose name is a body
// key was authored INSIDE the kind body (in-substrate); one that wasn't is a deploy-level
// sibling of the kind key.
func authoredBodyKeys(pn spec.ParsedNode) (map[string]bool, error) {
	out := map[string]bool{}
	if len(pn.Body) == 0 {
		return out, nil
	}
	var asMap map[string]any
	if err := json.Unmarshal(pn.Body, &asMap); err != nil {
		return nil, fmt.Errorf("node %q: decode body: %w", pn.Name, err)
	}
	for k := range asMap {
		out[k] = true
	}
	return out, nil
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

	// Cutover C task 0 — the fold stamps Member.Position from the authored depth ALONE (the
	// parse-preserved body-membership channel) and attaches the ONE ordered member list. The
	// former root-kind branch (dn.Target == "" → alongside, else deploy-into) is DELETED: one
	// authored shape, one meaning, classified by position — never re-derived from the node's
	// kind.
	members, err := BuildResourceMemberChildren(pn, t)
	if err != nil {
		return nil, err
	}
	dn.Member = members
	return &dn, nil
}

// BuildResourceMemberChildren decodes pn's RESOURCE-MEMBER entity children into the uniform
// ordered member ENTRIES (authored order preserved — pn.Children is a slice) via the SAME
// BuildFleetNode recursion — the SINGLE source of truth for authored member-tree decode (R3).
// Every pn.Children entry is an entity child by construction (the parse's own member extraction
// plus the desugar separate step/data children into the plan/body fields before a spec.ParsedNode
// ever reaches here), so no discClass filter is needed. A non-member entity child is a hard error
// (deploy/resource children must be pod/vm/kubernetes/local/android/group or a recognized
// external kind — the ONE memberDisc classification). The FOLD stamps each entry's Position from
// the authored depth alone: a child NAMED by a top-level key of pn's authored disc body hung
// INSIDE the kind body (in-substrate); one that didn't is a deploy-level sibling of the kind key.
func BuildResourceMemberChildren(pn spec.ParsedNode, t spec.Threaded) ([]spec.Member, error) {
	inBody, err := authoredBodyKeys(pn)
	if err != nil {
		return nil, err
	}
	var out []spec.Member
	for _, rk := range pn.Children {
		if !IsResourceDisc(rk.Disc, t) {
			return nil, fmt.Errorf("node %q: a %q child %q is not a resource member (deploy/resource children must be pod/vm/kubernetes/local/android)", pn.Name, rk.Disc, rk.Name)
		}
		member, err := BuildFleetNode(*rk, t)
		if err != nil {
			return nil, err
		}
		pos := spec.PositionDeployLevel
		if inBody[rk.Name] {
			pos = spec.PositionInSubstrate
		}
		out = append(out, spec.Member{Name: rk.Name, Position: pos, Node: member})
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

// DeployTargetEntity resolves a deploy-hop name to the TERMINAL kind:vm entity it builds
// from (Phase 3, the unified from: name:tag clone spelling): a plain entity name passes
// through; a name that is a kind:check BED (the clone-base bed — a deploy whose from:
// names the template) resolves its from: chain to the entity. The chain is ONE hop today
// (the base bed's from:); the walker is the single canonical resolution so plugin-vm's
// build drive, the create's config-resolve, and the validator never re-implement it (R3).
func DeployTargetEntity(uf *spec.UnifiedFile, name string) (string, bool) {
	if uf == nil || name == "" {
		return "", false
	}
	if _, has := uf.VM()[name]; has {
		return name, true
	}
	if d, has := uf.Fleet[name]; has && d.From != "" {
		if _, has := uf.VM()[d.From]; has {
			return d.From, true
		}
	}
	return "", false
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
// (#ResourceKind) — the fixed vocab alone (the frozen seam takes no Threaded snapshot; the
// registry-aware member classification is memberDisc, consumed by the parse and the fold).
func ResourceChildren(pn spec.ParsedNode) []spec.ParsedNode {
	var out []spec.ParsedNode
	for _, ch := range pn.Children {
		if resourceKindSet[ch.Disc] {
			out = append(out, *ch)
		}
	}
	return out
}
