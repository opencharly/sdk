package deploykit

import (
	"fmt"
	"maps"
	"os"
	"sort"

	"github.com/opencharly/spec/spec"
)

// preempt_resolve.go — portable (LoadUnified-free) resource-arbiter deploy-tree helpers,
// servable off a plain map[string]spec.FleetNode: the shape both a freshly-loaded uf.Fleet
// (host-side) and a resolved-project envelope's rp.Deploy (plugin-side, via
// Executor.InvokeProvider("build","project", OpResolve) — the former HostBuild("resolved-project")
// seam is DELETED) already carry — spec.FleetNode is a type alias for
// spec.Deploy (charly_names.go). K1-UNBLOCK wave 1: extracted from charly/preempt.go so the
// resource-arbiter plugin (candy/plugin-preempt) and any still-core caller needing the same
// projection (e.g. a kind:vm exclusive-claimant lookup) share ONE implementation (R3) instead of
// two independent copies.

// MergedDeployTree merges an already-loaded project deploy tree with the per-host deploy-config
// overlay (~/.config/charly/charly.yml) — exactly the merge the resource arbiter's holder/claimant
// gather has always performed (former charly/preempt.go gatherDeployNodes): per-host entries win
// per-field via MergeFleetNode. project may be nil/empty (no project loaded, e.g. a project-less
// `charly vm` invocation); the per-host overlay loads via read. context is a short label threaded to
// the reader's stderr warning so the caller is identifiable.
//
// read is the per-host-overlay loader (placement-invariant: a plugin caller injects
// loaderkit.LoadHostFleetConfigViaExecutor; a host in-proc caller may pass a LoadFleetConfig
// wrapper). #55 coneC-dsh β2+δ seam-death: MergedDeployTree no longer hard-wires
// LoadDeployConfigForRead (the DeployStateHost-backed read that silently degraded to project-only
// when DeployStateHost was nil — a placement-dependent correctness regression for the compiled-in
// arbiter); the reader is injected so the compiled-in candy/plugin-preempt arbiter and the
// plugin-vm config-resolve Claimant computation load the per-host overlay placement-invariantly.
// A nil read → no merge (project passes through unchanged) — the semantics the
// TestMergedDeployTree_ProjectOnlyWhenNoLocalConfig corpus pins.
func MergedDeployTree(project map[string]spec.FleetNode, context string, read func() (*FleetConfig, error)) map[string]spec.FleetNode {
	out := make(map[string]spec.FleetNode, len(project))
	maps.Copy(out, project)
	if read == nil {
		return out
	}
	dc, err := read()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %s: per-host deploy overlay unavailable for read: %v\n", context, err)
		return out
	}
	if dc != nil {
		for name, node := range dc.Fleet {
			out[name] = MergeFleetNode(out[name], node)
		}
	}
	return out
}

// FilterPreemptibleHolders returns every node in tree that declares itself a preemption holder
// (IsPreemptible), projected into spec.HolderDescriptor — the candidate set the arbiter may stop.
// Deterministic (sorted by name).
func FilterPreemptibleHolders(tree map[string]spec.FleetNode) []spec.HolderDescriptor {
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
