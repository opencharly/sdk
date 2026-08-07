package deploykit

import (
	"fmt"
	"os"

	"github.com/opencharly/sdk/vmshared"
	"github.com/opencharly/spec/spec"
)

// vm_deploy_state.go — the charly.yml persistence half of the former fleet_add_cmd_vm.go,
// R3-relocated from charly/vm_deploy_state.go (F6 vm-lifecycle move, coneB-vmlifecycle): this is
// SHARED deploy-state logic every deploy plugin (compiled-in or out-of-process) reaches, the same
// class as VmDeployEntryKeys/PruneStaleVmDottedTwin/IsAutoVmDeployEntry (FLOOR-SLIM Unit 3), which
// already live here operating on deploykit's own *FleetConfig.
//
// The plugin-primaries-coupled marshal callback (saveFleetConfigNodeForm) is NOT hoistable — the
// marshal needs a charly-package-private registry-derived lookup, pluginPrimaryFor, with no sdk
// exposure today (see charly/deploy_nodeform.go's own header for the proven-but-undeferred HOW) —
// so it stays an INJECTED save callback, mirroring SaveFleetConfig's own marshalNode-callback
// shape. The process-shared deploy-config FLOCK is NO LONGER injected: both bodies below run inside
// MutateFleetConfig (deploy_config_cycle.go), THE one locked read-modify-write cycle every overlay
// writer now shares, so the WHOLE read→decide→write critical section still runs under ONE lock hold
// exactly as before while the two per-candy lock copies this pair used to be handed are deleted
// (R3). Splitting the decision logic into a separate plugin-side call fed by a stale prior read
// would reintroduce the
// lost-update race RCA #6/#7 (referenced below) already fixed — that design was considered and
// rejected (team-lead ruling, #118 coneB-vmlifecycle).

// SaveVmDeployState writes the updated VmDeployState into ~/.config/charly/charly.yml for the given
// deploy name. Idempotent — overwrites the deploy.<name>.vm_state block. vmEntity is the kind:vm
// entity the deploy targets ("" → derive from a legacy "vm:<name>" deployName prefix); it is
// persisted as the entry's `vm:` cross-ref so a fleet-keyed entry (a kind:check VM bed, whose
// deploy key e.g. `check-k3s-vm` differs from `vm:<entity>`) carries the linkage teardown needs to
// find + remove it.
//
// The load→modify→save runs inside MutateFleetConfig, so it holds the process-shared blocking
// deploy-config lock across the whole cycle and re-reads the overlay INSIDE that lock. Without
// that, two parallel `charly vm create` persist-auto-port writers (or a vm-create racing a `charly
// fleet add vm:<name>`) load → modify → save the shared ~/.config/charly/charly.yml and silently
// drop each other's entry.
//
// read is the current-state re-read that cycle performs. A nil read falls back to
// LoadFleetConfig — the DeployStateHost-backed host read — so an IN-PROCESS host caller passes nil
// and behaves exactly as before. A plugin caller (out-of-process command:vm) injects its OWN
// loader-backed reader (loaderkit.LoadHostFleetConfigViaExecutor), so SaveVmDeployState no longer
// requires the DeployStateHost package var (#55 coneC-dsh config-write seam-collapse — mirrors the
// SaveFleetConfig/SaveDeployState reader-callback precedent).
func SaveVmDeployState(deployName, vmEntity string, state *spec.VmDeployState, save func(*FleetConfig) error, read func() (*FleetConfig, error)) error {
	loadBase := read
	if loadBase == nil {
		loadBase = LoadFleetConfig
	}
	_, err := MutateFleetConfig(loadBase, save, func(dc *FleetConfig) (bool, error) {
		saveVmStateInto(dc, deployName, vmEntity, state)
		return true, nil
	})
	return err
}

// saveVmStateInto applies the vm-state write to a FRESH overlay read under the deploy-config lock.
// Split out of SaveVmDeployState only so the mutation is a plain function over fresh state — the
// shape MutateFleetConfig requires and the reason the lock never has to span a caller's
// orchestration.
func saveVmStateInto(dc *FleetConfig, deployName, vmEntity string, state *spec.VmDeployState) {
	entry, exists := dc.Fleet[deployName]
	if !exists {
		entry = FleetNode{}
	}
	entry.Target = "vm"
	// Persist the `vm:` cross-ref so the per-host entry is a well-formed fleet
	// node AND so teardown can resolve a fleet-keyed entry back to its VM entity.
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
	// told about ephemeral registration (a SEPARATE concern candy/plugin-fleet's ephemeral family
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
	dc.Fleet[deployName] = entry

	// Self-healing prune (RCA #6, FINAL/K5 unit 6a): remove a stale dotted-key twin the
	// now-eliminated dual-writer path left behind for THIS SAME domain in an existing overlay —
	// nothing writes one anymore, but pre-fix overlays (real users', every bed record until now)
	// still carry it, and it poisons every subsequent load (spec.ValidateDeploymentName's
	// dot-rejection). One-touch cleanup on the next write for this domain — no new migration
	// machinery.
	if pruned := PruneStaleVmDottedTwin(dc, deployName); pruned != "" {
		fmt.Fprintf(os.Stderr, "note: pruned a stale per-host overlay entry %q for domain %q — left by a prior version's now-eliminated dotted-key vm-state write (canonical entry: %q)\n", pruned, vmshared.VmDomainIdentity(deployName), deployName)
	}
}

// RemoveVmDeployEntry strips deploy.<deployName> from charly.yml. save is the SAME injected
// node-form persist callback SaveVmDeployState takes — see this file's header — and the
// load→decide→write runs under the SAME MutateFleetConfig lock hold. read is the SAME
// reader-callback SaveVmDeployState takes (nil → DeployStateHost-backed LoadFleetConfig; a plugin
// caller injects its own loader-backed reader).
func RemoveVmDeployEntry(deployName string, save func(*FleetConfig) error, read func() (*FleetConfig, error)) error {
	loadBase := read
	if loadBase == nil {
		loadBase = LoadFleetConfig
	}
	_, err := MutateFleetConfig(loadBase, save, func(dc *FleetConfig) (bool, error) {
		return removeVmEntriesFrom(dc, deployName), nil
	})
	return err
}

// removeVmEntriesFrom applies the vm-entry removal to a FRESH overlay read under the deploy-config
// lock, reporting whether anything changed (false skips the write). Split out for the same reason
// saveVmStateInto is: MutateFleetConfig takes a function over fresh state.
func removeVmEntriesFrom(dc *FleetConfig, deployName string) bool {
	keys := VmDeployEntryKeys(dc, deployName)
	if len(keys) == 0 {
		return false
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
		entry := dc.Fleet[key]
		entry.VmState = nil
		if IsAutoVmDeployEntry(entry) {
			delete(dc.Fleet, key)
		} else {
			dc.Fleet[key] = entry
		}
	}
	return true
}
