package workflowkit

// planrefs.go — the PLAN-ref mechanism, lifted out of the lowering into exported SDK API
// so it keeps ONE implementation while the `kind: pipeline` lowering moves to the plugin
// that owns the kind (opencharly/spec#304 unit 1).
//
// WHY IT LIVES HERE AND IS EXPORTED. A plan carried by a pipeline step is lowered to a
// generated charly task whose `$step.path` references travel as `${REF_n}` environment
// variables. Collecting those references and rewriting them is the ONE place the ref
// GRAMMAR (`refRe`, expr.go) is applied to a yaml plan — and the grammar is the SDK's,
// shared with the expression compiler (`RefNames`, `RefIDs`, `RewriteRefs`, `HasRef`).
// The lowering itself is the pipeline KIND's machinery and belongs in plugin-pipeline;
// it cannot come with this, because a plugin module cannot reach `refRe`. So the two
// steps that touch the grammar stay here, exported, and the plugin calls them: a copy of
// the regex in the plugin would be a SECOND implementation of one grammar (R2), and it
// would drift from the expression validator the same pipeline is judged by.
//
// Both take a *yaml.Node, never a pipeline type — they are kind-blind, which is what
// keeps them on this side of the boundary.

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// PlanRefs returns the distinct `$step.path` references in a plan, in document order.
func PlanRefs(plan *yaml.Node) []string {
	var out []string
	seen := map[string]bool{}
	walkScalarValues(plan, func(n *yaml.Node) {
		for _, m := range refRe.FindAllString(n.Value, -1) {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	})
	return out
}

// RewritePlanRefs rewrites every `$step.path` in a plan to `${REF_<n>}`, the env var the
// lobster step sets (index i → REF_(i+1), matching PlanRefs' order). The BRACE form is
// mandatory: the expander that runs the plan (kit.ExpandOpVars, kit.TestVarRefPattern) is
// brace-only, so a bare `$REF_<n>` would reach a plugin-verb input as literal text.
func RewritePlanRefs(plan *yaml.Node, refs []string) {
	index := make(map[string]string, len(refs))
	for i, r := range refs {
		index[r] = fmt.Sprintf("${REF_%d}", i+1)
	}
	walkScalarValues(plan, func(n *yaml.Node) {
		n.Value = refRe.ReplaceAllStringFunc(n.Value, func(m string) string { return index[m] })
	})
}

// walkScalarValues calls fn on every scalar VALUE in a node subtree (mapping KEYS are
// never visited — a key is not a place a reference lives).
func walkScalarValues(n *yaml.Node, fn func(*yaml.Node)) {
	if n == nil {
		return
	}
	switch n.Kind {
	case yaml.SequenceNode:
		for _, c := range n.Content {
			walkScalarValues(c, fn)
		}
	case yaml.MappingNode:
		for i := 1; i < len(n.Content); i += 2 {
			walkScalarValues(n.Content[i], fn)
		}
	case yaml.ScalarNode:
		fn(n)
	}
}
