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
	"strings"

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
	// namespace-qualified.
	return bedRef(uf, ref)
}

// bedRef walks the fold (local + namespaces) for a disposable kind:check bed
// named ref. The qualified form `ns.bed` matches the namespace's own
// unqualified bed name; the unqualified form matches the local beds.
func bedRef(uf *spec.UnifiedFile, ref string) bool {
	if uf == nil {
		return false
	}
	if _, ok := uf.CheckBeds()[ref]; ok {
		return true
	}
	for ns, sub := range uf.Namespaces {
		if sub == nil {
			continue
		}
		// ONLY the qualified form (ns.bed) reaches into a namespace — the
		// unqualified form is local-only (an unqualified ref into a namespace
		// would be ambiguous; the runtime template lookup has the same
		// contract: ProjectTemplates().ByKind keys are ns-qualified).
		if strings.HasPrefix(ref, ns+".") {
			if _, ok := sub.CheckBeds()[strings.TrimPrefix(ref, ns+".")]; ok {
				return true
			}
		}
	}
	return false
}
