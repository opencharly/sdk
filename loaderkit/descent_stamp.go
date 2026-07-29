package loaderkit

import (
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// descent_stamp.go — the DATA-driven venue-hop descent stamp (K1-LOADER RELOCATION). The former
// charly/deploy_tree.go stampBundleDescents queried the provider registry live (deployTraitsFor →
// providerRegistry.ResolveKind) for every deploy node's substrate word; this relocation reads the
// registry-derived spec.Threaded.DeployTraits DATA snapshot instead, so the stamp is registry-FREE
// and runs identically host-side OR plugin-side (boundary law clause D: a kind-blind mechanism
// consults host-threaded DATA, never the registry). The host fills Threaded.DeployTraits from the
// SAME deployTraitsFor it used inline, so behaviour is byte-identical; a word absent from the map
// resolves to the external-in-place default via kit.DescentFromTraits(nil), matching the former
// deployTraitsFor's nil-for-unrecognized-word return.

// StampBundleDescents stamps every deploy node's venue-hop descent descriptor from the DeployTraits
// DATA snapshot, replacing the former registry-live charly stampBundleDescents. Idempotent.
func StampBundleDescents(uf *UnifiedFile, t spec.Threaded) {
	if uf == nil {
		return
	}
	traitsFor := func(word string) *spec.DeployTraits { return t.DeployTraits[word] }
	for name, node := range uf.Bundle {
		n := node
		kit.StampDescent(&n, traitsFor)
		uf.Bundle[name] = n
	}
}
