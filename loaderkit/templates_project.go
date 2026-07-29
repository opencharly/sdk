package loaderkit

import (
	"encoding/json"

	"github.com/opencharly/spec/spec"
)

// templates_project.go — the uf-COUPLED kind-TEMPLATE projection of the resolved-project envelope
// (K3 build-engine, Unit 2 body — the loaderkit half). Relocated from charly/resolved_project_host.go:
// it reads *UnifiedFile (uf.Local/K8s/Pod/VM/Android + uf.Namespaces), so its cycle-safe home is
// loaderkit (the package that owns UnifiedFile). Both charly core AND candy/plugin-build (running the
// build-engine RESOLVE plugin-side) call the ONE copy (R3).

// ProjectTemplates decodes the uf.Local/K8s/Pod/VM/Android raw template maps (map[string]json.RawMessage)
// into the existing spec kind types — the resolved kind-template maps validate/check-include/status read.
// Returns nil when no template kind is present. ALSO recurses into uf.Namespaces, mirroring FillBoxPlans's
// prefix-accumulation pattern exactly, so a namespace-qualified template ref (`local: <ns>.<tmpl>`,
// `kind:k8s` entity `<ns>.<name>`, …) is visible in the envelope too — previously only root-level names
// were. Purely ADDITIVE (qualified keys never collide with a bare name, since a bare name can never
// contain "."), so every existing root-scoped consumer is unaffected.
func ProjectTemplates(uf *UnifiedFile) *spec.ProjectTemplates {
	t := &spec.ProjectTemplates{}
	fillNamespacedTemplates(uf, "", t, map[*UnifiedFile]bool{})
	if t.Local == nil && t.K8s == nil && t.Pod == nil && t.VM == nil && t.Android == nil {
		return nil
	}
	return t
}

// fillNamespacedTemplates recursively copies uf's OWN template maps (qualified by prefix) into t, then
// descends into uf.Namespaces with the accumulated prefix. The visited set guards the pointer-keyed
// namespace cache against a self-referential cycle (mirrors FillBoxPlans's own guard).
func fillNamespacedTemplates(uf *UnifiedFile, prefix string, t *spec.ProjectTemplates, visited map[*UnifiedFile]bool) {
	if uf == nil || visited[uf] {
		return
	}
	visited[uf] = true
	// KIND-BLIND copy: the raw template bytes ride into the envelope verbatim as opaque RawBody. The
	// host NEVER decodes them into a concrete spec.<Kind> (that would be per-kind knowledge in the
	// kernel — a boundary-law violation the TestNoConcreteKindInKernel gate catches). The consuming
	// PLUGINS decode a RawBody into the concrete kind they need.
	cp := func(src map[string]json.RawMessage, dst *map[string]spec.RawBody) {
		for name, raw := range src {
			qualified := name
			if prefix != "" {
				qualified = prefix + "." + name
			}
			if *dst == nil {
				*dst = make(map[string]spec.RawBody, len(src))
			}
			(*dst)[qualified] = raw
		}
	}
	cp(uf.Local(), &t.Local)
	cp(uf.K8s(), &t.K8s)
	cp(uf.Pod(), &t.Pod)
	cp(uf.VM(), &t.VM)
	cp(uf.Android(), &t.Android)
	for ns, sub := range uf.Namespaces {
		child := ns
		if prefix != "" {
			child = prefix + "." + ns
		}
		fillNamespacedTemplates(sub, child, t, visited)
	}
}
