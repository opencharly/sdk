package loaderkit

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/opencharly/sdk/deploykit"
	"github.com/opencharly/spec/spec"
)

// validate_capabilities.go — the LOAD-time CAPABILITY validators (the kind:android box⊻adb XOR and
// the preemptible / requires_exclusive / requires_shared invariants), relocated from charly core
// (unified.go's validateAndroidDevices + validate_preempt.go). Their LOGIC is kind-blind clause-R
// capability validation; the ONLY host coupling is the per-kind registry RESOLVE (android/resource/vm
// templates → their resolved envelopes), which the host threads in as a resolve CALLBACK (the same
// spec.ResolvePluginKindViaPlugin pattern the vocabulary accessors use). So they run identically host-side
// (the compiled-in placement passes its in-proc registry callbacks) OR plugin-side (a genuine
// out-of-module loader passes InvokeProvider-backed callbacks) — the same relocation shape
// ValidateCheckBeds / ValidateEphemeral already took. Error accumulation reuses the loaderkit
// spec.Diagnostics helpers (addErr / diagErrors / diagJoin, validate_ephemeral.go), R3.

// ValidateAndroidDevices enforces the kind:android device source invariant: a device is EXACTLY ONE
// of an in-pod emulator (box:) XOR a remote/physical adb endpoint (adb:) — never both, never neither.
// This is the entity-level XOR the #Android CUE schema formerly expressed via a trailing
// `& ({box:_} | {adb:_})` disjunction (dropped because gengotypes collapses an entity-level
// disjunction to an empty struct — see schema/android.cue). Runs at LOAD time so EVERY command that
// resolves a device (charly fleet add android:, charly check run, charly box validate, …) sees the
// same friendly error. resolveAndroid projects one opaque android template body into its
// *spec.ResolvedAndroid via the registry (host-threaded).
func ValidateAndroidDevices(uf *spec.UnifiedFile, resolveAndroid func(json.RawMessage) (*spec.ResolvedAndroid, error)) error {
	if uf == nil {
		return nil
	}
	for name, sp := range spec.ResolvePluginKindViaPlugin(uf, "android", resolveAndroid) {
		if sp == nil {
			continue
		}
		hasBox := sp.Box != ""
		hasAdb := sp.Adb != nil
		switch {
		case hasBox && hasAdb:
			return fmt.Errorf("kind:android device %q sets both box: and adb: — a device is EXACTLY ONE of an in-pod emulator (box:) or a remote/physical adb endpoint (adb:)", name)
		case !hasBox && !hasAdb:
			return fmt.Errorf("kind:android device %q sets neither box: nor adb: — a device must declare EXACTLY ONE source (box: <kind:box emulator> or adb: {host: …})", name)
		}
	}
	return nil
}

// ValidatePreemptibleOnNode checks one deploy node's preemptible + requires_exclusive/requires_shared
// fields and accumulates problems into d:
//
//   - preemptible.holds must be non-empty (a holder that holds nothing is meaningless — nothing for a
//     claimant to contend over).
//   - preemptible.stop must be "shutdown" (the ONLY mechanism that frees a VFIO passthrough device;
//     pause/managedsave/destroy are rejected with a reason).
//   - preemptible.restore must be "always" or "on-success".
//   - requires_exclusive / requires_shared entries must be non-empty strings.
//   - a node may not claim a resource BOTH exclusively and shared, nor both hold and require/share the
//     SAME token (self-contention).
func ValidatePreemptibleOnNode(name string, node *spec.FleetNode, d *spec.Diagnostics) {
	if node == nil {
		return
	}
	if p := node.Preemptible; p != nil {
		if len(spec.DedupeNonEmpty(p.Holds)) == 0 {
			addErr(d, "deploy %q: `preemptible.holds` must list at least one exclusive-resource token — a preemptible holder that holds nothing is meaningless", name)
		}
		if p.Stop != "" && p.Stop != spec.PreemptStopShutdown {
			addErr(d, "deploy %q: `preemptible.stop: %s` is not supported — only %q (graceful shutdown, disk preserved) frees a passthrough device; pause/managedsave keep the device assigned to the holder", name, p.Stop, spec.PreemptStopShutdown)
		}
		if p.Restore != "" && p.Restore != spec.PreemptRestoreAlways && p.Restore != spec.PreemptRestoreSuccess {
			addErr(d, "deploy %q: `preemptible.restore: %s` is invalid — must be %q or %q", name, p.Restore, spec.PreemptRestoreAlways, spec.PreemptRestoreSuccess)
		}
	}
	for _, tok := range node.RequiresExclusive {
		if strings.TrimSpace(tok) == "" {
			addErr(d, "deploy %q: `requires_exclusive` contains an empty token", name)
		}
	}
	for _, tok := range node.RequiresShared {
		if strings.TrimSpace(tok) == "" {
			addErr(d, "deploy %q: `requires_shared` contains an empty token", name)
		}
	}
	// A node claims a resource EITHER exclusively (sole use — a VM) OR shared (refcounted — pods),
	// never both: the arbiter dispatches on whichever list is set, and the driver MODE a resource is
	// in (vfio for exclusive, nvidia for shared) is mutually exclusive.
	if len(node.RequiresExclusive) > 0 && len(node.RequiresShared) > 0 {
		addErr(d, "deploy %q: declares both `requires_exclusive` and `requires_shared` — a deploy claims a resource one way (sole use) or the other (shared), not both", name)
	}
	if node.Preemptible != nil {
		if shared := preemptIntersect(node.Preemptible.Holds, node.RequiresExclusive); len(shared) > 0 {
			addErr(d, "deploy %q: cannot both hold and require the same exclusive token(s): %s — a holder cannot contend with itself", name, strings.Join(shared, ", "))
		}
		if shared := preemptIntersect(node.Preemptible.Holds, node.RequiresShared); len(shared) > 0 {
			addErr(d, "deploy %q: cannot both hold and share the same token(s): %s — a holder cannot contend with itself", name, strings.Join(shared, ", "))
		}
	}
}

// ValidatePreemptible validates preemptible / requires_exclusive / requires_shared across a unified
// project's deploy map (which includes folded kind:check beds), plus the resource-vocabulary
// cross-check, returning the first batch of errors for the LoadUnified hard-fail path. resolveResource
// / resolveVm project the opaque resource: / vm: plugin-kind bodies into their resolved envelopes via
// the registry (host-threaded).
func ValidatePreemptible(uf *spec.UnifiedFile,
	resolveResource func(json.RawMessage) (*spec.ResolvedResource, error),
	resolveVm func(json.RawMessage) (*spec.ResolvedVm, error),
) error {
	if uf == nil {
		return nil
	}
	var d spec.Diagnostics
	for name, node := range uf.Fleet {
		n := node
		ValidatePreemptibleOnNode(name, &n, &d)
	}
	validateResourceDefs(uf, resolveResource, resolveVm, &d)
	if diagErrors(&d) {
		return fmt.Errorf("preemptible / requires_exclusive validation:\n  %s", diagJoin(&d))
	}
	return nil
}

// validateResourceDefs checks the `resource:` vocabulary and its interaction with exclusive-venue
// claimants:
//
//   - a `gpu:` selector MUST carry a non-empty vendor (auto-allocation matches DetectVFIO's reported
//     PCI vendor against it);
//   - an ExclusiveVenue claimant (today: only `vm`) requiring a GPU resource needs `backend: libvirt`
//     on its VM entity — a PCI <hostdev> does not render under the qemu backend, so auto-allocation
//     would silently fail at create time. Read BY TRAIT (the stamped node.Descent.ExclusiveVenue —
//     StampFleetDescents runs before this validator in LoadUnified), never by switching on the
//     substrate kind word (the boundary law).
func validateResourceDefs(uf *spec.UnifiedFile,
	resolveResource func(json.RawMessage) (*spec.ResolvedResource, error),
	resolveVm func(json.RawMessage) (*spec.ResolvedVm, error),
	d *spec.Diagnostics,
) {
	resources := spec.ResolvePluginKindViaPlugin(uf, "resource", resolveResource)
	for name, rdef := range resources {
		if rdef == nil {
			continue
		}
		if rdef.Gpu != nil && strings.TrimSpace(rdef.Gpu.Vendor) == "" {
			addErr(d, "resource %q: `gpu.vendor` is required (e.g. \"0x10de\" for NVIDIA) — it is the PCI vendor auto-allocation matches", name)
		}
	}
	if len(resources) == 0 {
		return
	}
	for name, node := range uf.Fleet {
		n := node
		if n.Descent == nil || !n.Descent.ExclusiveVenue { // vm (exclusive venue)
			continue
		}
		if !preemptNeedsGPUResource(&n, resources) {
			continue
		}
		vmName := n.From
		if vmName == "" {
			base, _ := deploykit.ParseDeployKey(name)
			vmName = base
		}
		if vmSpec, _ := resolveVm(uf.VM()[vmName]); vmSpec != nil && vmSpec.Backend == "qemu" {
			addErr(d, "deploy %q requires an auto-allocated GPU but its VM %q pins `backend: qemu` — GPU passthrough needs `backend: libvirt` (PCI <hostdev> does not render under qemu)", name, vmName)
		}
	}
}

// preemptNeedsGPUResource reports whether a claimant's requires_exclusive tokens include one mapping
// to a `resource:` carrying a gpu selector. Loaderkit-private copy of charly's requiredGPUResource
// (the arbiter's own copy travels with it in candy/plugin-preempt / candy/plugin-vm, their separate
// modules) — the validator here needs only the boolean.
func preemptNeedsGPUResource(cnode *spec.FleetNode, resources map[string]*spec.ResolvedResource) bool {
	if cnode == nil {
		return false
	}
	for _, tok := range cnode.RequiredExclusive() {
		if rdef := resources[tok]; rdef != nil && rdef.Gpu != nil {
			return true
		}
	}
	return false
}

// preemptIntersect returns the set intersection of a and b (loaderkit-private copy; the token-list
// normalizer it pairs with consolidated to spec.DedupeNonEmpty, K-wave 2 cone R2).
func preemptIntersect(a, b []string) []string {
	set := map[string]bool{}
	for _, s := range a {
		set[s] = true
	}
	var out []string
	seen := map[string]bool{}
	for _, s := range b {
		if set[s] && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
