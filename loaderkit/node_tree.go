package loaderkit

// node_tree.go — the entity-body assembly + deploy/resource-member tree-builder mechanism (K1
// unit 3b, relocated from charly/node_build.go + charly/node_deploy.go + charly/node_normalize.go).
// Operates on spec.ParsedNode (the wire-safe parsed-entity shape LoadUnified's parse already
// produces) rather than charly core's *genericNode: genericNode is a host-internal reconstruction
// consumed directly by charly/provider_kind_invoke.go's TRUE clause-M dispatch (candyIsImage /
// buildCandy, the box⊻layer bootstrap routing — clause B, permanently core), so it cannot itself
// move; but everything BELOW that dispatch layer — the entity-body assembler, the deploy-tree
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
// canonicalize a value. Returns pn.Body DIRECTLY (parser consolidation F1.5): pn.Body IS the
// parse-time output of the same yaml→map→JSON transform (parse.go entityBodyJSON), so the former
// reconstruction-then-retransform round-trip (discValue + entityBodyJSON) was pure overhead on
// data that was already canonical. The ONLY difference is the member-carrier strip — the
// authored body keeps in-substrate member keys as the fold's position channel, while every body
// EMITTER omits keys the member tree owns (Cutover C task 0) — done here in pure JSON (delete +
// re-marshal), byte-identical to the yaml round-trip it replaces.
func EntityBodyJSON(pn spec.ParsedNode) (json.RawMessage, error) {
	if len(pn.Body) == 0 {
		// An empty/absent body → the empty mapping, matching parse-time entityBodyJSON semantics.
		return json.RawMessage("{}"), nil
	}
	if len(pn.Children) == 0 {
		// No member-carrier keys to omit — the parse-produced body IS the canonical body.
		return pn.Body, nil
	}
	var asMap map[string]any
	if err := json.Unmarshal(pn.Body, &asMap); err != nil {
		return nil, fmt.Errorf("node %q: decode body: %w", pn.Name, err)
	}
	for _, ch := range pn.Children {
		delete(asMap, ch.Name)
	}
	if len(asMap) == 0 {
		asMap = map[string]any{} // keep an empty (non-nil) mapping body
	}
	out, err := json.Marshal(asMap)
	if err != nil {
		return nil, fmt.Errorf("node %q: to json: %w", pn.Name, err)
	}
	return out, nil
}

// BuildDeployNode recursively builds a DeployNode from a deploy/resource node. The discriminator
// value carries the deploy config; inline STEP children (checks) fold into the deploy's plan via
// DecodeNodeValue (the assembler); ENTITY children are RESOURCE members (deploy-into / alongside).
func BuildDeployNode(pn spec.ParsedNode, t spec.Threaded) (*spec.DeployNode, error) {
	var dn spec.DeployNode
	if err := DecodeNodeValue(pn, &dn); err != nil {
		return nil, err
	}
	// EDGE-INHERIT cutover B: the substrate kind at the EDGE is the target directly (no
	// inference from a cross-ref). group:/host: are targetless venues.
	dn.Target = DeployTargetForDisc(pn.Disc, t)
	// A scalar discriminator value (`vm: pg-vm` / `pod: img`) is the deploy's cross-ref: pod →
	// the image it runs; vm/kubernetes/local/android → the same-kind template it inherits (`from:`).
	dv, err := discValue(pn)
	if err != nil {
		return nil, err
	}
	if dv != nil && dv.Kind == yaml.ScalarNode {
		SetDeployCrossRef(&dn, pn.Disc, dv.Value, t)
	}
	// The mapping deploy form (`vm: {from: "base:golden"}`) decodes `from` into dn.From
	// via yaml DIRECTLY (bypassing SetDeployCrossRef) — normalize the unified name:tag split
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
// BuildDeployNode recursion — the SINGLE source of truth for authored member-tree decode (R3).
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
		member, err := BuildDeployNode(*rk, t)
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

// BuildDeployNodeInto builds pn into a DeployNode and registers it in the Deploy (deploy) map. acc
// is the K1-unit-1 spec.MaterializedProject accumulator — this function only ever touches the
// Deploy field.
func BuildDeployNodeInto(pn spec.ParsedNode, t spec.Threaded, acc *spec.MaterializedProject) error {
	dn, err := BuildDeployNode(pn, t)
	if err != nil {
		return err
	}
	if acc.Deploy == nil {
		acc.Deploy = map[string]spec.DeployNode{}
	}
	acc.Deploy[pn.Name] = *dn
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
	if d, has := uf.Deploy[name]; has && d.From != "" {
		if _, has := uf.VM()[d.From]; has {
			return d.From, true
		}
	}
	return "", false
}

// IsDeployShape reports whether a substrate node is a DEPLOY (vs a standalone template) — the
// ONE deploy-shape classifier (parser consolidation F1.2/F1.4), data-derived from the node
// itself (never a kind-word switch):
//
//   - any resource-member child (the fold's member channel — every parsed child of a substrate
//     node IS a resource member by construction, classified by the ONE memberDisc/IsResourceDisc
//     predicate, so a node that nests members is a deploy venue no matter what its body words);
//   - a scalar discriminator value (a cross-ref deploy: scalar `ref` under the kind key);
//   - a mapping value carrying `from:` or `image:` — a deploy;
//   - an `agent_provisioned: true` body — the imageless deploy spelling the Deploy gate
//     (spec.ValidateDeploymentTree → ValidateDeployRequiresBox) EXEMPTS from the pod box
//     requirement, so the classifier stays in lockstep with the gate (RCA 2026-09-07:
//     the minimal imageless iterate-entity classified as a standalone TEMPLATE and silently
//     vanished from acc.Deploy; locked by charly/substrate_imageless_deploy_test.go).
//
// Formerly the host OR-composed this function with a ResourceChildren walk and an extra body
// probe — both arms now live here so the fold calls ONE predicate.
func IsDeployShape(pn spec.ParsedNode) bool {
	if len(pn.Children) > 0 {
		return true
	}
	dv, err := discValue(pn)
	if err != nil || dv == nil {
		return false
	}
	if dv.Kind == yaml.ScalarNode {
		return dv.Value != ""
	}
	if dv.Kind == yaml.MappingNode {
		hasFromImage := false
		agentProvisioned := false
		for i := 0; i+1 < len(dv.Content); i += 2 {
			k := dv.Content[i].Value
			switch {
			case k == "from" || k == "image":
				hasFromImage = true
			case k == "agent_provisioned":
				// the flag's VALUE is a yaml scalar; true means the authored bool
				if dv.Content[i+1].Kind == yaml.ScalarNode && dv.Content[i+1].Value == "true" {
					agentProvisioned = true
				}
			}
		}
		return hasFromImage || agentProvisioned
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

// ResourceChildren is DELETED (parser consolidation F1.2): its only caller was the host's
// deploy-shape composite (len(ResourceChildren(pn)) > 0), subsumed by IsDeployShape's member
// channel above — the ONE member walk (BuildResourceMemberChildren) over the ONE predicate
// (IsResourceDisc) is the single source of truth for member classification. Its former frozen
// resourceKindSet-only filter also MISSED structural/external member children; the member
// channel checks the authored children themselves, so a structural or external member now
// classifies a node as deploy-shaped exactly like a builtin resource member.
