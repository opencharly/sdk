package deploykit

import (
	"maps"
	"sort"

	"github.com/opencharly/spec/spec"
)

// preempt_resolve.go — portable (LoadUnified-free) resource-arbiter deploy-tree helpers,
// servable off a plain map[string]spec.BundleNode: the shape both a freshly-loaded uf.Bundle
// (host-side) and a resolved-project envelope's rp.Deploy (plugin-side, via
// Executor.HostBuild("resolved-project")) already carry — spec.BundleNode is a type alias for
// spec.Deploy (charly_names.go). K1-UNBLOCK wave 1: extracted from charly/preempt.go so the
// resource-arbiter plugin (candy/plugin-preempt) and any still-core caller needing the same
// projection (e.g. a kind:vm exclusive-claimant lookup) share ONE implementation (R3) instead of
// two independent copies.

// MergedDeployTree merges an already-loaded project deploy tree with the per-host deploy-config
// overlay (~/.config/charly/charly.yml) — exactly the merge the resource arbiter's holder/claimant
// gather has always performed (former charly/preempt.go gatherDeployNodes): per-host entries win
// per-field via MergeBundleNode. project may be nil/empty (no project loaded, e.g. a project-less
// `charly vm` invocation); the per-host overlay always loads independently. context is a short
// label threaded to LoadDeployConfigForRead's stderr warning so the caller is identifiable.
func MergedDeployTree(project map[string]spec.BundleNode, context string) map[string]spec.BundleNode {
	out := make(map[string]spec.BundleNode, len(project))
	maps.Copy(out, project)
	if dc := LoadDeployConfigForRead(context); dc != nil {
		for name, node := range dc.Bundle {
			out[name] = MergeBundleNode(out[name], node)
		}
	}
	return out
}

// FilterPreemptibleHolders returns every node in tree that declares itself a preemption holder
// (IsPreemptible), projected into spec.HolderDescriptor — the candidate set the arbiter may stop.
// Deterministic (sorted by name).
func FilterPreemptibleHolders(tree map[string]spec.BundleNode) []spec.HolderDescriptor {
	names := make([]string, 0, len(tree))
	for name, node := range tree {
		if node.IsPreemptible() {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	out := make([]spec.HolderDescriptor, 0, len(names))
	for _, name := range names {
		node := tree[name]
		out = append(out, spec.HolderDescriptor{
			Name:    name,
			Holds:   node.PreemptionHolds(),
			Addr:    HolderAddrFor(name, node),
			Restore: PreemptEffectiveRestore(node.Preemptible),
		})
	}
	return out
}

// GpuVendorTokens projects a resolved resource map (spec.ResolvedProject.Resources, or the
// legacy host-side resource decode) to gpu-backed tokens -> PCI vendor — the only shape the
// arbiter's applyMode/firstPoisonedToken need. An arbitration-only token (no gpu: selector) is
// omitted, mirroring the former charly/arbiter_host.go resources() projection exactly.
func GpuVendorTokens(resources map[string]*spec.ResolvedResource) map[string]string {
	out := map[string]string{}
	for tok, rdef := range resources {
		if rdef != nil && rdef.Gpu != nil {
			out[tok] = rdef.Gpu.Vendor
		}
	}
	return out
}
