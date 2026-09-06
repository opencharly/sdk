package loaderkit

// fleet_decode.go — fleet/resource-member kind-decode SUPPORT helpers (K1 unit 3a, relocated
// from charly/node_fleet.go + charly/node_normalize.go): pure functions of a discriminator word
// + the registry-derived spec.Threaded snapshot (never a live registry query), consumed by the
// TRUE clause-M dispatch (charly/provider_kind_invoke.go) and its BuildFleetEntity fallback
// (charly/node_fleet.go's buildFleetNodeInto). DATA-driven via t.DeploySubstrates/t.DeployTraits
// — the SAME snapshot loaderThreaded() already fills for the parse — never a kind-word switch (the
// boundary law's self-test).
//
// resourceKindSet (the CUE-derived #ResourceKind vocab) is already declared in parse.go — reused
// here (R3), never re-threaded, exactly like decode_entity.go/cue_validate.go reuse docDirectiveSet.

import (
	"encoding/json"
	"strings"

	"github.com/opencharly/spec/spec"
)

// IsResourceDisc reports whether a discriminator names a deploy-substrate kind (the markers of a
// fleet member / fleet-shaped node) — the CUE-derived resourceKindSet (#ResourceKind), OR a
// recognized external DEPLOY substrate word (t.DeploySubstrates, a registered/pre-scanned
// out-of-process deploy provider, e.g. `exampledeploy`), so a deploy whose edge is an external
// target is built as a fleet node.
func IsResourceDisc(d string, t spec.Threaded) bool {
	return resourceKindSet[d] || t.DeploySubstrates[d]
}

// FleetTargetForDisc maps a node discriminator to the FleetNode Target — DATA-driven via
// t.DeployTraits (P9's plugin-declared #DeployTraits, the D-clause fact every substrate word
// resolves against), never a kind-word switch: a word with no declared deploy traits is
// TARGETLESS (`group` — the only such word today; a plugin-declared external deploy substrate
// DOES carry traits, the Venue="none" external-in-place default).
func FleetTargetForDisc(d string, t spec.Threaded) string {
	if t.DeployTraits[d] == nil {
		return "" // targetless (e.g. group — no own workload target)
	}
	return d // pod | vm | kubernetes | local | android | an external deploy substrate word
}

// SetFleetCrossRef sets the deploy's cross-ref from a scalar discriminator value
// (EDGE-INHERIT cutover B): DATA-driven via t.DeployTraits' ImageBacked trait (declared true for
// pod alone, per the canonical #DeployTraits table) rather than a kind-word switch — an
// image-backed substrate's scalar is the IMAGE it runs; every other substrate's scalar is the
// same-kind template it inherits (`from:`). A targetless word (traits == nil) sets neither.
func SetFleetCrossRef(dn *spec.FleetNode, disc, ref string, t spec.Threaded) {
	traits := t.DeployTraits[disc]
	if traits == nil {
		return
	}
	if traits.ImageBacked {
		dn.Image = ref
	} else {
		// The unified `from: name:tag` spelling (tag = snapshot name — Cutover A
		// addendum): split on the LAST `:`; a VM entity name may not contain `:`,
		// so the split is unambiguous. Image refs never reach this arm.
		dn.From, dn.FromSnapshot = splitVMSnapshotRef(ref)
	}
}

// splitVMSnapshotRef splits a VM deploy cross-ref of the unified `name:tag` spelling on the
// LAST `:` — a VM entity name may not contain `:`, so the split is unambiguous; no `:`
// → the plain name with no snapshot. pod/kubernetes IMAGE refs (the ImageBacked arm) never
// reach this helper: image tags legitimately contain `:`.
func splitVMSnapshotRef(ref string) (name, snapshot string) {
	if i := strings.LastIndex(ref, ":"); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	return ref, ""
}

// IsStandaloneResourceKind reports whether disc names one of the 5 substrate kinds
// (pod/vm/kubernetes/local/android) — the kinds that are BOTH a standalone TEMPLATE (→ the typed
// uf.Pod/uf.VM/… map) and a deploy (→ uf.Fleet). DATA-driven via t.DeployTraits — the SAME
// kind-blind fact FleetTargetForDisc/SetFleetCrossRef resolve against — rather than a
// hand-kept kind-word switch. group is a structural kind too but resolves false here — it
// declares no #DeployTraits (no per-substrate template map; it always folds to uf.Fleet).
func IsStandaloneResourceKind(disc string, t spec.Threaded) bool {
	return t.DeployTraits[disc] != nil
}

// FoldStandaloneTemplateReply folds candy/plugin-substrate's ECHOED template JSON into
// acc.PluginKinds[disc][name] — the C2-substrate TEMPLATE fold arm (the standalone counterpart of
// runPluginKind's deploy fold into acc.Fleet). GENERIC by construction: no per-kind-word switch —
// every standalone-template kind (vm/pod/kubernetes/local/android) folds into the SAME map[disc][name]
// shape PluginKinds already uses for every other templated kind (distro/builder/init/sidecar/
// resource/agent), so a new standalone-template kind needs no core edit here. disc is validated by
// the caller (foldSubstrateKind only reaches here for a kind IsStandaloneResourceKind already
// confirmed), so no error return is needed in practice — kept for seam-signature symmetry with the
// other MaterializedProject-folding calls.
func FoldStandaloneTemplateReply(disc, name string, replyJSON json.RawMessage, acc *spec.MaterializedProject) error {
	if acc.PluginKinds == nil {
		acc.PluginKinds = map[string]map[string]json.RawMessage{}
	}
	if acc.PluginKinds[disc] == nil {
		acc.PluginKinds[disc] = map[string]json.RawMessage{}
	}
	acc.PluginKinds[disc][name] = replyJSON
	return nil
}
