package deploykit

import (
	"gopkg.in/yaml.v3"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
)

// deploy_nodeform.go — the canonical BundleNode → compact node-form deploy serializer (K4:
// relocated from charly/deploy_nodeform.go, a genuinely pure yaml.Node transform with no
// project-loader dependency). MarshalBundleNode emits the COMPACT name-first node the per-host
// overlay (~/.config/charly/charly.yml) is read back in: the kind discriminator (pod/vm/k8s/
// local/android/group/bundle) carries the FULL body inline (scalars, collections, and the `plan:`
// step list), and only nested/peer members become child nodes (their names are load-bearing).
// Plan steps are RESUGARED (kit.ResugarPlan) so the written file round-trips through the loader's
// parse-time desugar instead of tripping its authored-envelope ban. Consumed directly by charly
// core's remaining caller (deploy_state_host.go) and candy/plugin-deploy-pod's deploy-state writer.

// bundleCrossRefKeys are the bundle-value scalar keys that NAME another top-level
// entity (the key equals the referenced entity's kind).
var bundleCrossRefKeys = map[string]bool{
	"box": true, "vm": true, "k8s": true, "local": true, "android": true,
}

// MarshalBundleNode emits a BundleNode as the compact name-first node-form the per-host
// overlay loader accepts: the kind discriminator carries the FULL body inline (scalars,
// collections, the resugared plan: step list), and nested/peer members become child nodes
// (recursive; discriminator inferred by bundleDiscForEntity). The loader-derived target /
// descent are dropped (target → the discriminator; descent → never persisted, a stored
// descent trips #DeployValue's descent?: _|_ on reload). Comment-preserving (yaml.v3 node
// API).
func MarshalBundleNode(node *spec.Deploy) (*yaml.Node, error) {
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
	disc := bundleDiscForEntity(fullBody)

	content := &yaml.Node{Kind: yaml.MappingNode}
	value := &yaml.Node{Kind: yaml.MappingNode}
	content.Content = append(content.Content, kit.ScalarNode(disc), value)
	// Copy ONLY the inline fields — skip the structural keys handled specially: target (→
	// the discriminator), nested/peer (→ recursive child nodes), descent (loader-derived,
	// never persisted), name (the map key, never a body field). Plan steps get resugared.
	skip := map[string]bool{"target": true, "nested": true, "peer": true, "descent": true, "name": true}
	for i := 0; i+1 < len(fullBody.Content); i += 2 {
		k, v := fullBody.Content[i], fullBody.Content[i+1]
		if skip[k.Value] {
			continue
		}
		if k.Value == "plan" {
			kit.ResugarPlan(v)
		}
		value.Content = append(value.Content, k, v)
	}
	// Recursive child nodes — the node-form UNWRAPS nested/peer: each child/member is a
	// direct SIBLING of the discriminator (its name load-bearing), NOT under a `nested:`/
	// `peer:` key. The loader re-derives the nested/peer grouping from the deploy tree
	// structure, so the writer emits the flat sibling form.
	appendChildNodes := func(m map[string]*spec.Deploy) error {
		if len(m) == 0 {
			return nil
		}
		for _, k := range SortedNestedKeys(m) {
			child, cerr := MarshalBundleNode(m[k])
			if cerr != nil {
				return cerr
			}
			content.Content = append(content.Content, kit.ScalarNode(k), child)
		}
		return nil
	}
	if err := appendChildNodes(node.Children); err != nil {
		return nil, err
	}
	if err := appendChildNodes(node.Members); err != nil {
		return nil, err
	}
	return content, nil
}

// bundleDiscForEntity picks the node-form discriminator for a deploy/check entity
// whose `target:` key is about to be dropped. A same-kind cross-ref (box/vm/local/
// k8s/android) uses `bundle:` (buildBundleNode infers the workload target from it);
// the SAVE path marshals BundleNode.Target, so the disc is that target — an empty
// target with a POD-WORKLOAD indicator (image/resolved_port/port) is a POD (the
// DEFAULT substrate), and an empty target with NO workload is a targetless deploy
// GROUP (`host` is the pre-rename spelling of `local`).
func bundleDiscForEntity(body *yaml.Node) string {
	if body != nil {
		for i := 0; i+1 < len(body.Content); i += 2 {
			if bundleCrossRefKeys[body.Content[i].Value] {
				return "bundle"
			}
		}
	}
	switch t := scalarFieldValue(body, "target"); t {
	case "host":
		return "local"
	case "":
		// An empty target with a POD-WORKLOAD indicator (an image: field, a resolved pod-port
		// map, or an authored port:) is a POD — the DEFAULT substrate — NOT a targetless group.
		// A `group:` deploy carries only MEMBERS and no own workload; misclassifying an
		// image-backed pod as a group writes its pod-only resolved_port under `group:`, which
		// #GroupInput rejects at the next load (the 2026-07 `charly config <image-ref>` config
		// corruption). A truly targetless deploy (members only, no workload) stays a group.
		if kit.FindMappingValue(body, "image") != nil ||
			kit.FindMappingValue(body, "resolved_port") != nil ||
			kit.FindMappingValue(body, "port") != nil {
			return "pod"
		}
		return "group"
	default:
		return t // pod | vm | k8s | local | android
	}
}

// scalarFieldValue returns the scalar value of key in m, or "" when absent / non-scalar.
func scalarFieldValue(m *yaml.Node, key string) string {
	if v := kit.FindMappingValue(m, key); v != nil && v.Kind == yaml.ScalarNode {
		return v.Value
	}
	return ""
}
