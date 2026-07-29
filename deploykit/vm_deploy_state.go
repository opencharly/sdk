package deploykit

import (
	"fmt"
	"os"

	"github.com/opencharly/spec/spec"
	"github.com/opencharly/sdk/vmshared"
)

// vm_deploy_state.go — the charly.yml persistence half of the former bundle_add_cmd_vm.go,
// R3-relocated from charly/vm_deploy_state.go (F6 vm-lifecycle move, coneB-vmlifecycle): this is
// SHARED deploy-state logic every deploy plugin (compiled-in or out-of-process) reaches, the same
// class as VmDeployEntryKeys/PruneStaleVmDottedTwin/IsAutoVmDeployEntry (FLOOR-SLIM Unit 3), which
// already live here operating on deploykit's own *BundleConfig.
//
// The TWO genuinely host-resident primitives this pair needs — the process-shared deploy-config
// FLOCK (acquireDeployConfigLock) and the plugin-primaries-coupled marshal callback
// (saveBundleConfigNodeForm) — are NOT hoistable (the marshal needs a charly-package-private
// registry-derived lookup, pluginPrimaryFor, with no sdk exposure today; see
// charly/deploy_nodeform.go's own header for the proven-but-undeferred HOW). So they are INJECTED
// callbacks, mirroring SaveBundleConfig's own marshalNode-callback shape: the caller (charly's
// hostBuildConfigPersist) supplies acquireDeployConfigLock + saveBundleConfigNodeForm, and the WHOLE
// read→decide→write critical section still runs under ONE lock hold in THIS function — atomicity is
// preserved exactly as before, just with the decision logic relocated out of charly core. Splitting
// the decision logic into a separate plugin-side call fed by a stale prior read would reintroduce
// the lost-update race RCA #6/#7 (referenced below) already fixed — that design was considered and
// rejected (team-lead ruling, #118 coneB-vmlifecycle).

// SaveVmDeployState writes the updated VmDeployState into ~/.config/charly/charly.yml for the given
// deploy name. Idempotent — overwrites the deploy.<name>.vm_state block. vmEntity is the kind:vm
// entity the deploy targets ("" → derive from a legacy "vm:<name>" deployName prefix); it is
// persisted as the entry's `vm:` cross-ref so a bundle-keyed entry (a kind:check VM bed, whose
// deploy key e.g. `check-k3s-vm` differs from `vm:<entity>`) carries the linkage teardown needs to
// find + remove it.
//
// acquireLock serializes the load→modify→save against concurrent charly processes — the SAME
// blocking deploy-config lock every other deploy-state writer uses. Without it two parallel
// `charly vm create` persist-auto-port writers (or a vm-create racing a `charly bundle add
// vm:<name>`) load → modify → save the shared ~/.config/charly/charly.yml and silently drop each
// other's entry.
func SaveVmDeployState(deployName, vmEntity string, state *spec.VmDeployState, acquireLock func() (func() error, error), save func(*BundleConfig) error) error {
	unlock, lockErr := acquireLock()
	if lockErr != nil {
		return fmt.Errorf("locking charly.yml for vm-state write: %w", lockErr)
	}
	defer func() { _ = unlock() }()

	// Load existing charly.yml (or start fresh).
	dc, err := LoadBundleConfig()
	if err != nil {
		return fmt.Errorf("loading charly.yml: %w", err)
	}
	if dc == nil {
		dc = &BundleConfig{}
	}
	if dc.Bundle == nil {
		dc.Bundle = map[string]BundleNode{}
	}

	entry, exists := dc.Bundle[deployName]
	if !exists {
		entry = BundleNode{}
	}
	entry.Target = "vm"
	// Persist the `vm:` cross-ref so the per-host entry is a well-formed bundle
	// node AND so teardown can resolve a bundle-keyed entry back to its VM entity.
	// Precedence: the explicit vmEntity (the canonical mapping the caller resolved,
	// e.g. check-k3s-vm → k3s-vm) → a legacy "vm:<entity>" deployName prefix →
	// PRESERVE the existing entry.From (never clobber a known cross-ref with "").
	switch {
	case vmEntity != "":
		entry.From = vmEntity
	default:
		if vmName, perr := vmshared.VmNameFromDeployName(deployName); perr == nil {
			entry.From = vmName
		}
	}
	// Ephemeral-registration ordering contract (RCA #7, FINAL/K5 unit 6a, live-probe-caught):
	// registerEphemeralIfMarked persists .VmState.Ephemeral under THIS SAME canonical key BEFORE
	// `charly vm create`'s own state writes run (e.g. the port_auto persist) — RCA #6's key
	// unification made this THE common case (the two writers never collided before, since
	// Writer B's now-eliminated dual key hid the interaction). A caller's `state` here is NEVER
	// told about ephemeral registration (a SEPARATE concern candy/plugin-bundle's ephemeral family
	// owns), so a wholesale `entry.VmState = state` would silently ERASE a just-registered
	// ephemeral block whenever the incoming state carries none. PRESERVE it explicitly — this is
	// NOT a general deep-merge; every OTHER VmState field is still a full overwrite, exactly as
	// before.
	var priorEphemeral *spec.EphemeralRuntime
	if entry.VmState != nil {
		priorEphemeral = entry.VmState.Ephemeral
	}
	entry.VmState = state
	if entry.VmState != nil && entry.VmState.Ephemeral == nil && priorEphemeral != nil {
		entry.VmState.Ephemeral = priorEphemeral
	}
	dc.Bundle[deployName] = entry

	// Self-healing prune (RCA #6, FINAL/K5 unit 6a): remove a stale dotted-key twin the
	// now-eliminated dual-writer path left behind for THIS SAME domain in an existing overlay —
	// nothing writes one anymore, but pre-fix overlays (real users', every bed record until now)
	// still carry it, and it poisons every subsequent load (spec.ValidateDeploymentName's
	// dot-rejection). One-touch cleanup on the next write for this domain — no new migration
	// machinery.
	if pruned := PruneStaleVmDottedTwin(dc, deployName); pruned != "" {
		fmt.Fprintf(os.Stderr, "note: pruned a stale per-host overlay entry %q for domain %q — left by a prior version's now-eliminated dotted-key vm-state write (canonical entry: %q)\n", pruned, vmshared.VmDomainIdentity(deployName), deployName)
	}

	return save(dc)
}

// RemoveVmDeployEntry strips deploy.<deployName> from charly.yml. acquireLock/save are the SAME
// injected host-resident primitives SaveVmDeployState takes — see this file's header.
func RemoveVmDeployEntry(deployName string, acquireLock func() (func() error, error), save func(*BundleConfig) error) error {
	unlock, lockErr := acquireLock()
	if lockErr != nil {
		return fmt.Errorf("locking charly.yml for vm-entry removal: %w", lockErr)
	}
	defer func() { _ = unlock() }()

	dc, err := LoadBundleConfig()
	if err != nil {
		return err
	}
	if dc == nil || dc.Bundle == nil {
		return nil
	}
	keys := VmDeployEntryKeys(dc, deployName)
	if len(keys) == 0 {
		return nil
	}
	// Destroying the VM invalidates only the RUNTIME state (vm_state). Clear
	// that, but PRESERVE every operator-authored per-host field (preemptible,
	// env, tunnel, port, security, add_candy, install_opts, …) so a
	// destroy→create cycle — which is exactly what `charly update <vm>` does
	// (the vm lifecycle hook's Rebuild shells `charly vm destroy` then `charly vm create`) —
	// never silently drops local config. (This is the root cause of the lost
	// `preemptible: {holds: [nvidia-gpu]}` on the operator workstation.)
	//
	// If, after clearing vm_state, the entry carries NOTHING operator-authored
	// beyond the fields SaveVmDeployState auto-sets (target: vm + vm:), it was a
	// pure auto-created VM-state record — e.g. a disposable check-bed VM — so
	// delete it entirely (such entries must not accumulate; that's why
	// destroy cleaned up the entry in the first place). Otherwise keep the
	// now-stateless entry so its operator config survives.
	for _, key := range keys {
		entry := dc.Bundle[key]
		entry.VmState = nil
		if IsAutoVmDeployEntry(entry) {
			delete(dc.Bundle, key)
		} else {
			dc.Bundle[key] = entry
		}
	}
	return save(dc)
}
