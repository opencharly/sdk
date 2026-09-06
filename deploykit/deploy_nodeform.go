package deploykit

import (
	"gopkg.in/yaml.v3"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// deploy_nodeform.go — the canonical FleetNode → compact node-form deploy serializer. MarshalFleetNode
// emits the COMPACT name-first node the per-host overlay (~/.config/charly/charly.yml) is read back in:
// the kind discriminator (pod/vm/kubernetes/local/android/group/fleet) carries the FULL body inline (scalars,
// collections, and the `plan:` step list), and only nested/peer members become child nodes (their names
// are load-bearing). Plan steps are RESUGARED (the internal plugin/plugin_input pair back to the authored
// `<word>: <input>` sugar, collapsing a single-primary map to the scalar shorthand) so the written file
// round-trips through the loader's parse-time desugar instead of tripping its authored-envelope ban.
//
// P13 residue clear (deploy_nodeform.go convergence): this file previously held a STALE, DEAD copy — no
// primaries param, resugar via kit.ResugarPlan (no scalar-collapse), and a fleetDiscForEntity MISSING the
// pod-classification fixes — while charly-core kept the LIVE copy that diverged with those fixes. Converged
// to the live shape here: it reads the plugin-verb WORD → primary-field D-fact as plain DATA (the
// `primaries` param — the SAME map spec.ResolvedProject.Primaries carries and spec.Threaded.Primaries
// snapshots), so charly-core's host writer sources it from loaderThreaded().Primaries and any plugin holding
// the resolved-project envelope drives the identical marshal — no charly-private pluginPrimaryFor registry
// consult (that host-side LOAD-path gate stays in charly/node_desugar.go). Consumed by charly core's
// deploy_state_host.go host wrapper (feeding loaderThreaded().Primaries) + reachable by any deploy plugin.

// fleetCrossRefKeys are the fleet-value scalar keys that NAME another top-level
// entity (the key equals the referenced entity's kind).
var fleetCrossRefKeys = map[string]bool{
	"box": true, "vm": true, "kubernetes": true, "local": true, "android": true,
}

// MarshalFleetNode emits a FleetNode as the compact name-first node-form the per-host
// overlay loader accepts: the kind discriminator carries the FULL body inline (scalars,
// collections, the resugared plan: step list), and nested/peer members become child nodes
// (recursive; discriminator inferred by fleetDiscForEntity). The loader-derived target /
// descent are dropped (target → the discriminator; descent → never persisted, a stored
// descent trips #DeployValue's descent?: _|_ on reload). Comment-preserving (yaml.v3 node
// API).
func MarshalFleetNode(node *spec.Deploy, primaries map[string]string) (*yaml.Node, error) {
	// Marshal the struct to capture all scalar/collection fields (env, port, volume, ...).
	nb, err := yaml.Marshal(node)
	if err != nil {
		return nil, err
	}
	var nd yaml.Node
	if err := yaml.Unmarshal(nb, &nd); err != nil {
		return nil, err
	}
	fullBody := &yaml.Node{Kind: yaml.MappingNode}
	if len(nd.Content) == 1 && nd.Content[0].Kind == yaml.MappingNode {
		fullBody = nd.Content[0]
	}
	// Compute the discriminator from the struct body (reads target + cross-ref keys +
	// workload indicators BEFORE the structural keys are filtered out).
	disc := fleetDiscForEntity(fullBody)

	content := &yaml.Node{Kind: yaml.MappingNode}
	value := &yaml.Node{Kind: yaml.MappingNode}
	content.Content = append(content.Content, kit.ScalarNode(disc), value)
	// Copy ONLY the inline fields — skip the structural keys handled specially: target (→
	// the discriminator), the loader-built member tree (member/member_of → recursive child
	// nodes below), descent (loader-derived, never persisted), name (the map key, never a
	// body field). Plan steps get resugared.
	skip := map[string]bool{"target": true, "member": true, "member_of": true, "descent": true, "name": true}
	for i := 0; i+1 < len(fullBody.Content); i += 2 {
		k, v := fullBody.Content[i], fullBody.Content[i+1]
		if skip[k.Value] {
			continue
		}
		if k.Value == "plan" {
			resugarPlan(v, primaries)
		}
		value.Content = append(value.Content, k, v)
	}
	// Recursive child nodes — the node-form UNWRAPS the member tree: each member is a
	// direct SIBLING of the discriminator (its name load-bearing), in the ONE ordered
	// Member list's authored order — both positions flatten to siblings, and the loader
	// re-derives the position grouping from the authored depth on reload (the parse
	// preserves it), so the writer emits the flat sibling form.
	for i := range node.Member {
		m := &node.Member[i]
		if m.Node == nil {
			continue
		}
		child, cerr := MarshalFleetNode(m.Node, primaries)
		if cerr != nil {
			return nil, cerr
		}
		content.Content = append(content.Content, kit.ScalarNode(m.Name), child)
	}
	return content, nil
}

// fleetDiscForEntity picks the node-form discriminator for a deploy/check entity
// whose `target:` key is about to be dropped. A same-kind cross-ref (box/vm/local/
// kubernetes/android) uses `fleet:` (buildFleetNode infers the workload target from it);
// the SAVE path marshals FleetNode.Target, so the disc is that target — an empty
// target with a POD-WORKLOAD indicator (image/resolved_image/resolved_port/port/
// volume_project_checked) is a POD (the DEFAULT substrate), and an empty target with
// NO workload is a targetless deploy GROUP (`host` is the pre-rename spelling of `local`).
func fleetDiscForEntity(body *yaml.Node) string {
	if body != nil {
		for i := 0; i+1 < len(body.Content); i += 2 {
			if fleetCrossRefKeys[body.Content[i].Value] {
				return "fleet"
			}
		}
	}
	switch t := scalarFieldValue(body, "target"); t {
	case "host":
		return "local"
	case "":
		// An empty target with a POD-WORKLOAD indicator (an image: field, a resolved pod-port
		// map, an authored port:, a resolved_image: — the add_candy: overlay ref a pod's
		// PrepareVenue persists — or volume_project_checked: — the project-declared volume persist
		// marker) is a POD — the DEFAULT substrate — NOT a targetless group. A `group:` deploy
		// carries only MEMBERS and no own workload; misclassifying an image-backed pod as a group
		// writes its pod-only field under `group:`, which #GroupInput rejects at the next load (the
		// 2026-07 `charly config <image-ref>` config corruption; its sibling for resolved_image; and
		// the VolumeProjectChecked one — a BRAND-NEW pod deploy's FIRST-EVER `charly config` persists
		// a State patch carrying ONLY VolumeProjectChecked before Image/port ever land). A truly
		// targetless deploy (members only, no workload) stays a group.
		if kit.FindMappingValue(body, "image") != nil ||
			kit.FindMappingValue(body, "resolved_image") != nil ||
			kit.FindMappingValue(body, "resolved_port") != nil ||
			kit.FindMappingValue(body, "port") != nil ||
			kit.FindMappingValue(body, "volume_project_checked") != nil {
			return "pod"
		}
		return "group"
	default:
		return t // pod | vm | kubernetes | local | android
	}
}

// scalarFieldValue returns the scalar value of key in m, or "" when absent / non-scalar.
func scalarFieldValue(m *yaml.Node, key string) string {
	if v := kit.FindMappingValue(m, key); v != nil && v.Kind == yaml.ScalarNode {
		return v.Value
	}
	return ""
}

// resugarPlan is the parse-time desugar's INVERSE, used by the deploy-state WRITER: each step's
// internal plugin/plugin_input pair rewrites back to the authored `<word>: <input>` sugar
// (collapsing a single-primary map to the scalar shorthand), so a written file round-trips through
// the parse-time desugar instead of tripping its authored-envelope ban.
//
// primaries is the plugin-verb WORD → primary-field D-fact (spec.ResolvedProject.Primaries /
// spec.Threaded.Primaries): resugar reads it as DATA rather than dialing a provider registry, so
// any holder of the envelope drives the identical resugar. (Replaces the pre-convergence
// kit.ResugarPlan, which lacked the primaries-driven scalar-collapse.)
func resugarPlan(plan *yaml.Node, primaries map[string]string) {
	if plan == nil || plan.Kind != yaml.SequenceNode {
		return
	}
	for _, st := range plan.Content {
		if st.Kind != yaml.MappingNode {
			continue
		}
		pluginIdx, inputIdx := -1, -1
		for i := 0; i+1 < len(st.Content); i += 2 {
			switch st.Content[i].Value {
			case "plugin":
				pluginIdx = i
			case "plugin_input":
				inputIdx = i
			}
		}
		if pluginIdx < 0 {
			continue
		}
		word := st.Content[pluginIdx+1].Value
		input := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		if inputIdx >= 0 {
			input = st.Content[inputIdx+1]
		}
		// scalar-collapse: input == {<primary>: <scalar>}
		if prim, ok := primaries[word]; ok && input.Kind == yaml.MappingNode &&
			len(input.Content) == 2 && input.Content[0].Value == prim &&
			input.Content[1].Kind == yaml.ScalarNode {
			input = input.Content[1]
		}
		nc := make([]*yaml.Node, 0, len(st.Content))
		for i := 0; i+1 < len(st.Content); i += 2 {
			switch i {
			case pluginIdx:
				nc = append(nc, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: word,
					HeadComment: st.Content[i].HeadComment}, input)
			case inputIdx:
				// dropped — folded into the sugar key's value
			default:
				nc = append(nc, st.Content[i], st.Content[i+1])
			}
		}
		st.Content = nc
	}
}
