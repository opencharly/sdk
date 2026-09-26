package loaderkit

// resolve_entity_ref.go — the ONE canonical entity-reference resolution surface
// (R3): a bed's `from:` cross-ref resolves against the fold as (a) a template
// entity — local or namespace-qualified via ProjectTemplates().ByKind (the
// git-linked import namespaces fold as `ns.entity` through
// fillNamespacedTemplates) — or (b) a clone-base deploy-hop — a kind:check bed,
// local or namespace-qualified, whose own from: is validated in its own
// context. The load-time bed validator (validate_check_beds.go) and the
// runtime entity resolver (resolve_kind_entity_via_executor.go) share this ONE
// implementation; the former local-only PluginKinds lookup is retired here.
//
// The namespace scope is the axis the two surfaces diverged on (the RCA'd
// seam): the validator enumerated the LOCAL maps only, so a git-linked
// (namespace-qualified) from: was runtime-green by design but validate-red.
// This resolver makes the scope identical everywhere: EVERYTHING in charly is
// referenceable — locally AND via git-linked imports.

import (
	"encoding/json"

	"github.com/opencharly/spec/spec"
)

// ResolveEntityRef reports whether ref names a resolvable entity of the given
// kind in the fold: a template entity (local or namespace-qualified) OR a
// clone-base deploy-hop (a kind:check bed, local or namespace-qualified).
func ResolveEntityRef(uf *spec.UnifiedFile, kind, ref string) bool {
	if uf == nil || ref == "" {
		return false
	}
	// (a) the template entity — ProjectTemplates().ByKind is namespace-aware
	// (fillNamespacedTemplates accumulates the `ns.` prefix), so a qualified
	// ref like `omarchy.check-charly-omarchy-vm` resolves here.
	if _, ok := uf.ProjectTemplates().ByKind(kind)[ref]; ok {
		return true
	}
	// (b) the clone-base deploy-hop — a kind:check bed, local or
	// namespace-qualified, via the ONE bed resolver (spec.UnifiedFile.ResolveBed).
	_, ok := uf.ResolveBed(ref)
	return ok
}

// ResolveKindEntityBody returns the opaque kind:<word> template body named ref
// — local or namespace-qualified (the git-linked import form). The runtime
// counterpart of ResolveEntityRef: a ref that validates must resolve here.
//
// The lookup goes through ProjectTemplates().ByKind, which folds EVERY imported
// namespace RECURSIVELY into fully-qualified keys (`nsA.nsB.name`). The former
// single-level descent (`for ns, sub := range uf.Namespaces { HasPrefix(ref, ns+".") }`)
// missed a NESTED namespace entity such as `charly.omarchy.omarchy-vm` (the umbrella
// root imports charly/, which imports omarchy/), failing `charly vm build
// charly.omarchy.omarchy-vm` with "no kind:vm entity in charly.yml".
func ResolveKindEntityBody(uf *spec.UnifiedFile, kind, ref string) (json.RawMessage, bool) {
	if uf == nil || ref == "" {
		return nil, false
	}
	if t := uf.ProjectTemplates(); t != nil {
		if body, ok := t.ByKind(kind)[ref]; ok {
			return json.RawMessage(body), true
		}
	}
	return nil, false
}
