// Package loaderkit is the importable form of charly's unified-config PARSE — the parse half of
// LoadUnified relocated out of charly core (P6). It decomposes one node-form YAML document into
// the generic, sdk-expressible spec.ParsedProject the host MATERIALIZES into the typed
// *UnifiedFile. Shared by the loader plugin candy (candy/plugin-loader, its OpLoad) and, during
// the transition, by charly core — the SAME parse, one copy (R3), the way sdk/kit is the one
// copy of the check walk.
//
// The parse consults ONLY spec vocabulary (CUE-sourced) + yaml + host-threaded kind-recognition
// DATA (Threaded) — never the provider registry directly. The registry is core fabric; the host
// snapshots which words it recognizes (kinds / deploy substrates / structural / scalar-sugar
// primaries) into Threaded before the parse, and the re-entrant connect-then-reload re-parses
// with an updated snapshot. That keeps the parse a kind-blind mechanism (boundary law clause D).
package loaderkit

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/opencharly/sdk/spec"
	"gopkg.in/yaml.v3"
)

// DocParser is the swappable per-document PARSE seam (P6): the loader plugin candy implements it
// (the default via the package ParseDoc), and the host resolves the registered loader provider to
// it and calls it for every document — so an alternative loader plugin serves a different config
// front-end by implementing this. Typed (no wire envelope) since it runs on every document load.
type DocParser interface {
	ParseDoc(doc *yaml.Node, t Threaded) (directives map[string]*yaml.Node, pp spec.ParsedProject, err error)
}

// DefaultParser is the built-in node-form DocParser — the package ParseDoc. It is what
// candy/plugin-loader serves and the host's fallback before any loader plugin registers.
type DefaultParser struct{}

// ParseDoc implements DocParser via the package parse.
func (DefaultParser) ParseDoc(doc *yaml.Node, t Threaded) (map[string]*yaml.Node, spec.ParsedProject, error) {
	return ParseDoc(doc, t)
}

// Threaded is the host-computed, registry-derived DATA the parse consults instead of querying
// the provider registry: which words are recognized kinds / deploy substrates, which external
// kinds may nest sub-entity members, and each plugin verb's scalar-sugar primary field.
type Threaded struct {
	Kinds            map[string]bool   // recognizedKind
	DeploySubstrates map[string]bool   // recognizedDeploySubstrate
	StructuralKinds  map[string]bool   // externalKindMayNestMembers
	Primaries        map[string]string // pluginPrimaryFor: verb word → scalar-sugar primary field
}

// reserved-word sets, CUE-sourced spec vocab (never a registry query).
var (
	resourceKindSet  = sliceSet(spec.ResourceKinds)
	stepKeywordSet   = sliceSet(spec.StepKeywords)
	docDirectiveSet  = sliceSet(spec.DocDirectives)
	authoringVerbSet = sliceSet(spec.AuthoringVerbs) // core's authoredOpFieldSet
	kindWordSet      = sliceSet(spec.KindWords)
)

func sliceSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

// classifyKind reports whether k is a recognized KIND word in this position. At the top level
// every registered/threaded kind + external deploy substrate classifies; as a member child only
// the deployable resource kinds do.
func classifyKind(k string, asChild bool, t Threaded) bool {
	if asChild {
		return resourceKindSet[k]
	}
	if kindWordSet[k] || stepKeywordSet[k] {
		return kindWordSet[k]
	}
	if t.Kinds[k] {
		return true
	}
	return t.DeploySubstrates[k]
}

// ParseDoc decomposes a node-form document mapping into its reserved directives + the generic
// spec.ParsedProject (its top-level entity nodes, each with the opaque JSON body the host
// materializes). Faithful port of core's parseNodeTree + genericNodeToParsed.
func ParseDoc(doc *yaml.Node, t Threaded) (directives map[string]*yaml.Node, pp spec.ParsedProject, err error) {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return nil, pp, fmt.Errorf("node-form document: expected a top-level mapping, got yaml kind %v", doc.Kind)
	}
	directives = map[string]*yaml.Node{}
	// A single document's top-level node names are GLOBALLY UNIQUE.
	seen := map[string]string{}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		key, val := doc.Content[i], doc.Content[i+1]
		if docDirectiveSet[key.Value] {
			directives[key.Value] = val
			continue
		}
		pn, e := parseNode(key.Value, val, false, t)
		if e != nil {
			return nil, spec.ParsedProject{}, e
		}
		if prior, dup := seen[pn.Name]; dup {
			return nil, spec.ParsedProject{}, fmt.Errorf("node %q: duplicate top-level entity name (already declared as a %q node) — a single document's top-level node names are globally unique; rename one (keep the user-facing deploy name, suffix the template)", pn.Name, prior)
		}
		seen[pn.Name] = pn.Disc
		pp.Nodes = append(pp.Nodes, pn)
	}
	return directives, pp, nil
}

// parseNode builds a spec.ParsedNode from a node mapping (the value under `name:`). asChild is
// true when the node is a member of another node (vs top-level).
func parseNode(name string, m *yaml.Node, asChild bool, t Threaded) (spec.ParsedNode, error) {
	if m.Kind == yaml.DocumentNode && len(m.Content) == 1 {
		m = m.Content[0]
	}
	if m.Kind != yaml.MappingNode {
		return spec.ParsedNode{}, fmt.Errorf("node %q: expected a mapping value, got yaml kind %v", name, m.Kind)
	}
	var disc string
	var discValue *yaml.Node
	type kv struct{ k, v *yaml.Node }
	var memberPairs []kv
	for i := 0; i+1 < len(m.Content); i += 2 {
		key, val := m.Content[i], m.Content[i+1]
		if classifyKind(key.Value, asChild, t) {
			if disc != "" {
				return spec.ParsedNode{}, fmt.Errorf("node %q: two kind discriminators (%q and %q) — an entity has exactly one, and a member child must not be NAMED like a kind word", name, disc, key.Value)
			}
			disc, discValue = key.Value, val
			continue
		}
		memberPairs = append(memberPairs, kv{key, val})
	}
	if disc == "" {
		return spec.ParsedNode{}, fmt.Errorf("node %q: no kind discriminator — collections and plan steps live INLINE in the kind value (the named child-node shape was removed); run: charly migrate", name)
	}
	// Desugar the body's plan steps in place (plugin sugar → plugin/plugin_input) BEFORE the
	// body is serialized (and before any consumer — including the raw-value CUE gates — sees it).
	if discValue != nil && discValue.Kind == yaml.MappingNode {
		if err := desugarEntityPlan(name, discValue, t); err != nil {
			return spec.ParsedNode{}, err
		}
	}
	body, err := entityBodyJSON(name, discValue)
	if err != nil {
		return spec.ParsedNode{}, err
	}
	pn := spec.ParsedNode{Name: name, Disc: disc, Body: body}
	for _, c := range memberPairs {
		if !resourceKindSet[disc] && !t.StructuralKinds[disc] {
			return spec.ParsedNode{}, fmt.Errorf("node %q (kind %q): child %q is not allowed — only deployable kinds (pod/vm/k8s/local/android/group) or an external structural plugin kind nest sub-entity members; an old-shape data/step child must be migrated (run: charly migrate)", name, disc, c.k.Value)
		}
		child, err := parseNode(c.k.Value, c.v, true, t)
		if err != nil {
			return spec.ParsedNode{}, err
		}
		pn.Children = append(pn.Children, &child)
	}
	return pn, nil
}

// entityBodyJSON serializes a node's kind-value mapping to the opaque JSON body the host fold
// consumes — the SAME yaml→map→JSON transform core's entityBodyJSON used (R3).
func entityBodyJSON(name string, discValue *yaml.Node) (json.RawMessage, error) {
	if discValue == nil || discValue.Kind != yaml.MappingNode {
		// An empty/scalar body → an empty mapping, matching core's entityBodyMapping.
		return json.RawMessage("{}"), nil
	}
	yamlBytes, err := yaml.Marshal(discValue)
	if err != nil {
		return nil, fmt.Errorf("node %q: marshal: %w", name, err)
	}
	var asMap any
	if err := yaml.Unmarshal(yamlBytes, &asMap); err != nil {
		return nil, fmt.Errorf("node %q: reparse: %w", name, err)
	}
	j, err := json.Marshal(asMap)
	if err != nil {
		return nil, fmt.Errorf("node %q: to json: %w", name, err)
	}
	return j, nil
}

// desugarEntityPlan desugars every `plan:` step of an entity body in place.
func desugarEntityPlan(entity string, body *yaml.Node, t Threaded) error {
	plan := mapValue(body, "plan")
	if plan == nil {
		return nil
	}
	if plan.Kind != yaml.SequenceNode {
		return fmt.Errorf("node %q: plan must be a step LIST (got yaml kind %v); run: charly migrate", entity, plan.Kind)
	}
	for i, st := range plan.Content {
		if err := desugarStep(entity, i, st, t); err != nil {
			return err
		}
	}
	return nil
}

// desugarStep rewrites one plan step's `<word>: <input>` plugin-verb sugar into the internal
// plugin/plugin_input pair. Faithful port of core's desugarStep.
func desugarStep(entity string, idx int, st *yaml.Node, t Threaded) error {
	if st.Kind != yaml.MappingNode {
		return fmt.Errorf("node %q: plan[%d] must be a mapping step", entity, idx)
	}
	intents := 0
	var sugarKeys []int
	for i := 0; i+1 < len(st.Content); i += 2 {
		k := st.Content[i].Value
		switch {
		case k == "plugin" || k == "plugin_input":
			return fmt.Errorf("node %q: plan[%d] authors %q — the plugin envelope is internal-only; author the verb as `<word>: <input>` sugar (run: charly migrate)", entity, idx, k)
		case stepKeywordSet[k]:
			intents++
		case authoringVerbSet[k]:
			// a builtin verb or shared step modifier — stays as-is
		default:
			sugarKeys = append(sugarKeys, i)
		}
	}
	if intents == 0 {
		return fmt.Errorf("node %q: plan[%d] has no intent keyword (run/check/agent-run/agent-check/include)", entity, idx)
	}
	if intents > 1 {
		return fmt.Errorf("node %q: plan[%d] has multiple intent keywords — a step has exactly one", entity, idx)
	}
	if len(sugarKeys) == 0 {
		return nil
	}
	if len(sugarKeys) > 1 {
		names := make([]string, 0, len(sugarKeys))
		for _, i := range sugarKeys {
			names = append(names, st.Content[i].Value)
		}
		sort.Strings(names)
		return fmt.Errorf("node %q: plan[%d] carries multiple non-#Op keys (%s) — a step takes at most ONE plugin-verb sugar key", entity, idx, strings.Join(names, ", "))
	}
	i := sugarKeys[0]
	wordNode, valNode := st.Content[i], st.Content[i+1]
	word := wordNode.Value
	var input *yaml.Node
	switch valNode.Kind {
	case yaml.MappingNode:
		input = valNode
	case yaml.ScalarNode, yaml.SequenceNode:
		prim, ok := t.Primaries[word]
		if !ok {
			return fmt.Errorf("node %q: plan[%d] plugin verb %q takes a MAP input (it declares no primary field for the scalar shorthand)", entity, idx, word)
		}
		input = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: prim},
			valNode,
		}}
	default:
		// a null value is an input-less verb
		input = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	}
	st.Content[i] = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "plugin",
		HeadComment: wordNode.HeadComment, LineComment: wordNode.LineComment, FootComment: wordNode.FootComment}
	st.Content[i+1] = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: word}
	st.Content = append(st.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "plugin_input"}, input)
	return nil
}

// mapValue returns the value node for key in a mapping node, or nil. (Local copy of the tiny
// yaml helper — loaderkit stays dependency-light.)
func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil {
		return nil
	}
	root := m
	if root.Kind == yaml.DocumentNode && len(root.Content) == 1 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			return root.Content[i+1]
		}
	}
	return nil
}
