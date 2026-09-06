// Package loaderkit is the importable form of charly's unified-config PARSE — the parse half of
// LoadUnified relocated out of charly core (P6). It decomposes one node-form YAML document into
// the generic, sdk-expressible spec.ParsedProject the host MATERIALIZES into the typed
// *spec.UnifiedFile. Shared by the loader plugin candy (candy/plugin-loader, its OpLoad) and, during
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

	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// The per-document PARSE seam interface (spec.DocParser) + the host-threaded kind-recognition
// DATA (spec.Threaded) are the CONTRACT — they live in sdk/spec (alongside the generated
// #ParsedProject / #LoadedProject wire types) so the host and the loader plugin reference them
// without importing each other. loaderkit is the ONE implementation of the parse. The former
// in-loaderkit DefaultParser died in K1 (the compiled-in candy/plugin-loader is the sole parser,
// registered at init before any load; a nil parser is a FATAL host-side, never a silent fallback).

// reserved-word sets, CUE-sourced spec vocab (never a registry query).
var (
	resourceKindSet  = sliceSet(spec.ResourceKinds)
	stepKeywordSet   = sliceSet(spec.StepKeywords)
	docDirectiveSet  = sliceSet(spec.DocDirectives)
	authoringVerbSet = sliceSet(spec.AuthoringVerbs) // core's authoredOpFieldSet
	kindWordSet      = sliceSet(spec.KindWords)
	// instrumentModifiers are the non-verb authoring fields of a #Instrument capture
	// entry (Cutover A, plan 3): id/phase/pipeline. A capture entry carries exactly
	// ONE verb-position key beside them, desugared identically to a step verb. Mirrors
	// spec's #Instrument authoring contract (spec A-task-1); the CUE def gates any
	// other authored key.
	instrumentModifiers = sliceSet([]string{"id", "phase", "pipeline"})
)

func sliceSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

// memberDisc is the ONE Threaded-fed MEMBER-discriminator classification (Cutover C task 0,
// parser review 8a): clause-D DATA only — the CUE-derived resource kinds ∪ the threaded external
// structural kinds ∪ the threaded external deploy substrates. The three former drift-prone
// spellings (classifyKind's asChild arm, parseNode's parent-allow check, IsResourceDisc's fold
// set) all consume THIS function, so a member classifies identically at every consult site —
// an external-structural child under an external-structural parent classifies (the 8a bug:
// the child arm never consulted t.StructuralKinds — dies here).
func memberDisc(word string, t spec.Threaded) bool {
	return resourceKindSet[word] || t.StructuralKinds[word] || t.DeploySubstrates[word]
}

// classifyKind reports whether k is a recognized KIND word in this position. At the top level
// every registered/threaded kind + external deploy substrate classifies; as a member child the
// ONE member classification (memberDisc) decides — resource kinds, structural kinds, and deploy
// substrates alike.
func classifyKind(k string, asChild bool, t spec.Threaded) bool {
	if asChild {
		return memberDisc(k, t)
	}
	if kindWordSet[k] || stepKeywordSet[k] {
		return kindWordSet[k]
	}
	if t.Kinds[k] {
		return true
	}
	return memberDisc(k, t)
}

// ParseDoc decomposes a node-form document mapping into its reserved directives + the generic
// spec.ParsedProject (its top-level entity nodes, each with the opaque JSON body the host
// materializes). Faithful port of core's parseNodeTree + genericNodeToParsed.
func ParseDoc(doc *yaml.Node, t spec.Threaded) (directives map[string]*yaml.Node, pp spec.ParsedProject, err error) {
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
func parseNode(name string, m *yaml.Node, asChild bool, t spec.Threaded) (spec.ParsedNode, error) {
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
	// Desugar the body in place (plan steps + instrument/pipeline entries — plugin sugar →
	// plugin/plugin_input) BEFORE the body is serialized (and before any consumer — including
	// the raw-value CUE gates — sees it).
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
	// Cutover C task 0 — the parse PRESERVES the authored depth (indentation IS the intent):
	// an entity-kind key INSIDE the disc value body is an IN-SUBSTRATE member of that substrate
	// (deployed into its venue), so it parses as a member child OF THIS NODE — recursively —
	// exactly like a deploy-level sibling does; the substrate's closed-schema gates stop seeing
	// the member keys because every body EMITTER (discValue / EntityBodyJSON) omits keys the
	// member tree owns, while the authored body here keeps them as the position channel the fold
	// stamps Member.Position from. A key BESIDE the disc (a sibling of the kind key) is a
	// DEPLOY-LEVEL member (brought up alongside). One rule at every depth.
	//
	// memberDisc(disc) is the ONE parent-allow classification (8a): a deployable resource kind,
	// an external structural plugin kind, or an external deploy substrate may nest members.
	memberNames := map[string]bool{}
	appendChild := func(child spec.ParsedNode) error {
		if memberNames[child.Name] {
			return fmt.Errorf("node %q (kind %q): duplicate member name %q — a node's member children (in-body and deploy-level alike) are globally unique within the node", name, disc, child.Name)
		}
		memberNames[child.Name] = true
		pn.Children = append(pn.Children, &child)
		return nil
	}
	allowMembers := memberDisc(disc, t)
	// In-substrate members first — the parent-disc guard (#222) with the REFINED two-rule scan:
	// the scan fires ONLY where member nesting is MEANINGFUL — a DEPLOY-SUBSTRATE body or a
	// STRUCTURAL body (nestingKinds = t.DeploySubstrates ∪ t.StructuralKinds, data-driven from
	// Threaded, zero hardcoded words). An arbitrary resource-kind body (a check/candy/box body)
	// has a CLOSED schema and never scans; its members are DEPLOY-LEVEL siblings only (the
	// memberPairs loop below).
	//
	// SUBSTRATE parent (vm/pod/local/android/kubernetes) — UNCHANGED: the entity-keyed value-shape
	// scan (discEntityPairs/isEntityValue). Substrate bodies have no colliding declared fields
	// (their unknown keys are already rejected by their own decoders).
	//
	// STRUCTURAL parent (an F5 structural kind, e.g. group) — the KEY rule: an in-body key is a
	// member iff the KEY ITSELF is a memberDisc word, minus the kind's DECLARED input-schema
	// fields (t.StructuralDeclaredFields — the registered #XInput body fields the host threads).
	// A non-kind key is opaque data whose value is NEVER looked inside (no transitive recursion —
	// the `iterate:`-carrying-`agent:` collision class dies here); a kind-word key that IS a
	// declared field stays data. A kind with NO declared schema threaded falls back to every
	// kind-word key being a member (the documented, tested fallback).
	if t.DeploySubstrates[disc] {
		for _, c := range discEntityPairs(discValue, t) {
			child, err := parseNode(c.k.Value, c.v, true, t)
			if err != nil {
				return spec.ParsedNode{}, err
			}
			if err := appendChild(child); err != nil {
				return spec.ParsedNode{}, err
			}
		}
	} else if t.StructuralKinds[disc] {
		declared := t.StructuralDeclaredFields[disc]
		for i := 0; i+1 < len(discValue.Content); i += 2 {
			k, v := discValue.Content[i], discValue.Content[i+1]
			if !memberDisc(k.Value, t) || declared[k.Value] {
				continue // opaque data (never look inside v) — or a declared field → data
			}
			child, err := parseNode(k.Value, v, true, t)
			if err != nil {
				return spec.ParsedNode{}, err
			}
			if err := appendChild(child); err != nil {
				return spec.ParsedNode{}, err
			}
		}
	}
	// Deploy-level siblings second: entity keys beside the disc key, in authored order.
	for _, c := range memberPairs {
		if !allowMembers {
			return spec.ParsedNode{}, fmt.Errorf("node %q (kind %q): child %q is not allowed — only deployable kinds (pod/vm/kubernetes/local/android/group) or an external structural plugin kind nest sub-entity members; an old-shape data/step child must be migrated (run: charly migrate)", name, disc, c.k.Value)
		}
		child, err := parseNode(c.k.Value, c.v, true, t)
		if err != nil {
			return spec.ParsedNode{}, err
		}
		if err := appendChild(child); err != nil {
			return spec.ParsedNode{}, err
		}
	}
	return pn, nil
}

// discEntityPairs returns the ENTITY-keyed pairs of a disc value body — the in-substrate member
// candidates: a pair whose value is a mapping that carries a kind discriminator (memberDisc —
// the ONE member classification). Data fields (scalars, sequences, mappings of non-kind keys)
// never classify, so the body's own data travels untouched. Consulted ONLY under the
// parent-disc guard (the caller scans bodies of kinds that nest members — deploy substrates
// and external structural kinds) — never inside an arbitrary resource-kind body whose closed
// schema may define fields colliding with kind words.
func discEntityPairs(discValue *yaml.Node, t spec.Threaded) []struct{ k, v *yaml.Node } {
	if discValue == nil || discValue.Kind != yaml.MappingNode {
		return nil
	}
	type kv = struct{ k, v *yaml.Node }
	var out []kv
	for i := 0; i+1 < len(discValue.Content); i += 2 {
		k, v := discValue.Content[i], discValue.Content[i+1]
		if isEntityValue(v, t) {
			out = append(out, kv{k, v})
		}
	}
	return out
}

// isEntityValue reports whether a body value parses as an entity node: a mapping carrying a
// member-discriminator key (memberDisc). The SAME rule the node level applies to a member
// child's own mapping — the key is the member's NAME, the disc lives inside its value.
func isEntityValue(v *yaml.Node, t spec.Threaded) bool {
	if v == nil || v.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(v.Content); i += 2 {
		if memberDisc(v.Content[i].Value, t) {
			return true
		}
	}
	return false
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

// desugarEntityPlan desugars every `plan:` step of an entity body in place, plus the
// `instrument:` capture entries (Cutover A) — each entry's verb-position key and its
// `pipeline:` word list. All of them rewrite authored `<word>: <input>` plugin-verb
// sugar into the internal plugin/plugin_input pair with byte-identical semantics; the
// CUE defs (the closed #Step/#Instrument bodies + each plugin's own input schema) gate
// everything else, so the parse stays kind-blind.
func desugarEntityPlan(entity string, body *yaml.Node, t spec.Threaded) error {
	if plan := mapValue(body, "plan"); plan != nil {
		if plan.Kind != yaml.SequenceNode {
			return fmt.Errorf("node %q: plan must be a step LIST (got yaml kind %v); run: charly migrate", entity, plan.Kind)
		}
		for i, st := range plan.Content {
			if err := desugarStep(entity, i, st, t); err != nil {
				return err
			}
		}
	}
	// Capture entries live on the substrate-node body beside plan: (the CUE substrate
	// schema gates where instrument: may appear — not the parse's job).
	if inst := mapValue(body, "instrument"); inst != nil {
		if inst.Kind != yaml.SequenceNode {
			return fmt.Errorf("node %q: instrument must be a capture entry LIST (got yaml kind %v)", entity, inst.Kind)
		}
		for i, entry := range inst.Content {
			if err := desugarInstrumentEntry(entity, i, entry, t); err != nil {
				return err
			}
		}
	}
	return nil
}

// desugarStep rewrites one plan step's `<word>: <input>` plugin-verb sugar into the internal
// plugin/plugin_input pair. Faithful port of core's desugarStep.
func desugarStep(entity string, idx int, st *yaml.Node, t spec.Threaded) error {
	if st.Kind != yaml.MappingNode {
		return fmt.Errorf("node %q: plan[%d] must be a mapping step", entity, idx)
	}
	path := fmt.Sprintf("plan[%d]", idx)
	sugarKeys, err := sugarKeyIndexes(entity, path, st, nil)
	if err != nil {
		return err
	}
	intents := 0
	for i := 0; i+1 < len(st.Content); i += 2 {
		if stepKeywordSet[st.Content[i].Value] {
			intents++
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
		return fmt.Errorf("node %q: plan[%d] carries multiple non-#Op keys (%s) — a step takes at most ONE plugin-verb sugar key", entity, idx, sugarKeyNames(st, sugarKeys))
	}
	return desugarVerbKey(entity, path, st, sugarKeys[0], t)
}

// desugarInstrumentEntry desugars one `instrument:` capture entry in place: its single
// verb-position key (rewritten into plugin/plugin_input exactly like a step verb) and
// its `pipeline:` word list. Authoring plugin:/plugin_input: directly in a capture
// entry is a hard load error, exactly as in a step.
func desugarInstrumentEntry(entity string, idx int, entry *yaml.Node, t spec.Threaded) error {
	if entry.Kind != yaml.MappingNode {
		return fmt.Errorf("node %q: instrument[%d] must be a mapping capture entry", entity, idx)
	}
	path := fmt.Sprintf("instrument[%d]", idx)
	sugarKeys, err := sugarKeyIndexes(entity, path, entry, instrumentModifiers)
	if err != nil {
		return err
	}
	if len(sugarKeys) > 1 {
		return fmt.Errorf("node %q: %s carries multiple non-#Op keys (%s) — a capture entry takes at most ONE verb-position sugar key", entity, path, sugarKeyNames(entry, sugarKeys))
	}
	if len(sugarKeys) == 1 {
		if err := desugarVerbKey(entity, path, entry, sugarKeys[0], t); err != nil {
			return err
		}
	}
	if pl := mapValue(entry, "pipeline"); pl != nil {
		if pl.Kind != yaml.SequenceNode {
			return fmt.Errorf("node %q: %s.pipeline must be a word LIST (got yaml kind %v)", entity, path, pl.Kind)
		}
		for j, w := range pl.Content {
			if err := desugarPipelineWord(entity, fmt.Sprintf("%s.pipeline[%d]", path, j), w, t); err != nil {
				return err
			}
		}
	}
	return nil
}

// desugarPipelineWord desugars one pipeline word — a one-verb-key map like
// `transcode: {to: mp4}` — into the internal plugin/plugin_input pair (the evidence
// phase dispatches the word blindly through the registry). Exactly ONE verb key per
// word; the internal pair is a hard load error here too.
func desugarPipelineWord(entity, path string, w *yaml.Node, t spec.Threaded) error {
	if w.Kind != yaml.MappingNode {
		return fmt.Errorf("node %q: %s must be a one-verb-key word map", entity, path)
	}
	sugarKeys, err := sugarKeyIndexes(entity, path, w, nil)
	if err != nil {
		return err
	}
	if len(sugarKeys) != 1 {
		return fmt.Errorf("node %q: %s must carry exactly ONE verb key (got %d)", entity, path, len(sugarKeys))
	}
	return desugarVerbKey(entity, path, w, sugarKeys[0], t)
}

// sugarKeyIndexes returns the positions of a sugar-bearing mapping's verb-position keys —
// the authored `<word>: <input>` plugin-verb candidates. plugin:/plugin_input: authored
// directly is a hard load error (the envelope is internal-only). Every other KNOWN field
// stays as-is: the #Op authoring verbs and the step intent keywords (the step rule,
// shared by capture entries and pipeline words) plus the context's own modifiers (known,
// e.g. an instrument entry's id/phase/pipeline). Everything else is a verb candidate.
func sugarKeyIndexes(entity, path string, m *yaml.Node, known map[string]bool) ([]int, error) {
	var idx []int
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i].Value
		switch {
		case k == "plugin" || k == "plugin_input":
			return nil, fmt.Errorf("node %q: %s authors %q — the plugin envelope is internal-only; author the verb as `<word>: <input>` sugar (run: charly migrate)", entity, path, k)
		case authoringVerbSet[k] || stepKeywordSet[k] || (known != nil && known[k]):
			// a builtin verb, a shared modifier/intent keyword, or a context modifier — stays as-is
		default:
			idx = append(idx, i)
		}
	}
	return idx, nil
}

// sugarKeyNames renders the sorted names of the given sugar keys for the multi-key error.
func sugarKeyNames(m *yaml.Node, idx []int) string {
	names := make([]string, 0, len(idx))
	for _, i := range idx {
		names = append(names, m.Content[i].Value)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// desugarVerbKey rewrites one mapping pair in place — the authored `<word>: <input>`
// plugin-verb sugar at (m.Content[i], m.Content[i+1]) — into the internal
// plugin/plugin_input pair, byte-identical for plan steps, capture entries and pipeline
// words. plugin_input mirrors #Op's declared shape: an opaque map (the plugin's own
// served CUE schema validates it); scalar shorthand wraps the verb's declared primary.
func desugarVerbKey(entity, path string, m *yaml.Node, i int, t spec.Threaded) error {
	wordNode, valNode := m.Content[i], m.Content[i+1]
	word := wordNode.Value
	var input *yaml.Node
	switch valNode.Kind {
	case yaml.MappingNode:
		input = valNode
	case yaml.ScalarNode, yaml.SequenceNode:
		prim, ok := t.Primaries[word]
		if !ok {
			return fmt.Errorf("node %q: %s plugin verb %q takes a MAP input (it declares no primary field for the scalar shorthand)", entity, path, word)
		}
		input = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: prim},
			valNode,
		}}
	default:
		// a null value is an input-less verb
		input = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	}
	m.Content[i] = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "plugin",
		HeadComment: wordNode.HeadComment, LineComment: wordNode.LineComment, FootComment: wordNode.FootComment}
	m.Content[i+1] = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: word}
	m.Content = append(m.Content,
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
