package loaderkit

import "github.com/opencharly/spec/spec"

// namespace_walk.go — the ONE cycle guard for every walk of the spec.UnifiedFile.Namespaces
// graph, in this module and in its consumers: ResolveProjectSeams.FillNamespacedBoxes carries
// this type across the seam, so a plugin implementing that leg inherits the semantics instead
// of re-deriving them.
//
// The guard MUST be PATH-SCOPED (an ancestor stack), NOT a global pointer-identity set: the
// loader mounts the SAME *spec.UnifiedFile at MULTIPLE namespace paths BY DESIGN — a diamond
// import (`c` reached through both `a` and `b`, i.e. `a.c` AND `b.c` as one pointer via the
// REFERENCE-mount pointer identity) and a multi-alias mount (one repo at `arch` +
// `cachyos.arch`). A global guard records only the FIRST path reachable in map-iteration order
// (nondeterministic) and silently DROPS every later alias; an ancestor stack records EVERY path
// while still terminating a genuine back-edge — a namespace that contains itself up the CURRENT
// path, e.g. the intentional `main<->sub` mutual import.
//
// It lives here ONCE because that subtlety was re-derived at six call sites, and a walk that
// gets it wrong fails in ways nothing else notices: PluginKinds restored empty at a later alias
// made a namespaced bed (`b.c.check-*`, `from: vm`) false-fail "not defined", and a walk with NO
// guard at all — ProjectCandiesScanned before opencharly/sdk#352 — died with
// `fatal error: stack overflow` on the same graph.
type NamespaceAncestors map[*spec.UnifiedFile]bool

// Enter marks uf as being on the CURRENT path. ok is false when uf is nil or already an ancestor
// on this path — a genuine back-edge — and the caller MUST then return WITHOUT descending. A
// caller that descended MUST `defer leave()`, which pops uf again so the same node reached by a
// second path still walks. Index a NamespaceAncestors directly (a[sub]) to TEST a candidate
// without descending into it.
func (a NamespaceAncestors) Enter(uf *spec.UnifiedFile) (leave func(), ok bool) {
	if uf == nil || a[uf] {
		return nil, false
	}
	a[uf] = true
	return func() { delete(a, uf) }, true
}
