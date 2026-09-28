package loaderkit

import (
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// descent_stamp.go — the DATA-driven venue-hop descent stamp (K1-LOADER RELOCATION). The former
// charly/deploy_tree.go stampFleetDescents queried the provider registry live (deployTraitsFor →
// providerRegistry.ResolveKind) for every deploy node's substrate word; this relocation reads the
// registry-derived spec.Threaded.DeployTraits DATA snapshot instead, so the stamp is registry-FREE
// and runs identically host-side OR plugin-side (boundary law clause D: a kind-blind mechanism
// consults host-threaded DATA, never the registry). The host fills Threaded.DeployTraits from the
// SAME deployTraitsFor it used inline, so behaviour is byte-identical; a word absent from the map
// resolves to the external-in-place default via kit.DescentFromTraits(nil), matching the former
// deployTraitsFor's nil-for-unrecognized-word return.

// StampDeployDescents stamps every deploy node's venue-hop descent descriptor from the DeployTraits
// DATA snapshot, replacing the former registry-live charly stampFleetDescents. Idempotent.
//
// It recurses into EVERY imported NAMESPACE (nested `nsA.nsB`), not just the root map: a namespaced
// bed resolves from its OWNING namespace's UnifiedFile, so an unstamped namespace node reaches the
// run with Descent==nil — deploy.IsVmVenue then returns false and a `vm:` root is misclassified as a
// pod (empty Image → `deploy add` fails). This was the roster defect: `charly.check-agentteams-vm`
// resolved IsVM=false from the umbrella root while the SAME bed was IsVM=true run locally.
//
// The guard is a path-scoped ANCESTOR stack (mirroring Beds()), so a mutual import
// (main↔sub) terminates while a shared namespace legitimately mounted at multiple alias paths is
// still stamped at each path.
func StampDeployDescents(uf *spec.UnifiedFile, t spec.Threaded) {
	stampDescents(uf, t, map[*spec.UnifiedFile]bool{})
}

func stampDescents(uf *spec.UnifiedFile, t spec.Threaded, ancestors map[*spec.UnifiedFile]bool) {
	if uf == nil || ancestors[uf] {
		return
	}
	ancestors[uf] = true
	defer delete(ancestors, uf)
	traitsFor := func(word string) *spec.DeployTraits { return t.DeployTraits[word] }
	for name, node := range uf.Deploy {
		n := node
		kit.StampDescent(&n, traitsFor)
		uf.Deploy[name] = n
	}
	for _, sub := range uf.Namespaces {
		stampDescents(sub, t, ancestors)
	}
}
