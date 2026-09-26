package deploykit

import (
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// vm_deploy_addressing.go — the DeployConfig-shaped VM deploy-state helpers (FLOOR-SLIM Unit 3,
// relocated from charly/vm_deploy_state.go): each one is PURE over an already-loaded *DeployConfig
// / *spec.ResolvedVm, touching no LoadUnified and no charly-core type. They stay in deploykit
// (rather than vmshared) because they operate on deploykit's OWN DeployConfig type — vmshared
// cannot import deploykit (deploykit already imports vmshared; the reverse is a cycle). The WRITE
// path (SaveVmDeployState/RemoveVmDeployEntry, vm_deploy_state.go — F6 vm-lifecycle move,
// coneB-vmlifecycle) now lives in THIS SAME package and calls these directly; only the two
// genuinely host-resident primitives it needs (the process-shared flock + the
// pluginPrimaries-registry-coupled marshal callback) stay charly-core, injected as callbacks.

// ResolveVmSshPort picks the host-side SSH port forward, reusing the persisted vm_state.ssh_port
// (idempotent across rebuilds) when ssh.port_auto is set. The project-config READ is the one
// deploykit-coupled bit (LoadDeployConfigForRead over the per-host overlay); the
// resolution/allocation decision itself is the shared kit.ResolveVmSshPort. The overlay entry is
// keyed by the deploy IDENTITY (vmName), the same string the tree/config/CLI use.
func ResolveVmSshPort(sp *spec.ResolvedVm, vmName string) (int, error) {
	if sp == nil {
		// NIL-SPEC guard (the live-check panic RCA 2026-09-06): the check-live spec
		// lookup can legitimately miss (the deploy-hop name vs the template) — a nil
		// spec means the port resolution falls back to the shared allocator with the
		// entity's name (no port-forward), never a deref panic.
		return kit.ResolveVmSshPort(nil, vmName, 0)
	}
	var persisted int
	if sp.SSH != nil && sp.SSH.PortAuto {
		// NIL-SAFE read (RCA 2026-09-06): LoadDeployConfigForRead returns nil when the
		// DeployStateHost is unregistered — the out-of-process plugin processes (deploy-vm,
		// check live) never register it (the host registers it in ITS OWN process at init), so
		// the raw call chain panicked on a nil .LookupKey in the plugin live-check path
		// (check-live crash, instrument bed). A nil config simply means "no persisted state" —
		// the shared allocator (kit.ResolveVmSshPort) picks a fresh port, exactly the
		// plugin-process contract the deploy-vm's resolvePriorVmState already uses.
		cfg := LoadDeployConfigForRead("charly vm ssh-port")
		if cfg != nil {
			if entry, ok := cfg.LookupKey(vmName); ok && entry.VmState != nil && entry.VmState.SSHPort > 0 {
				persisted = entry.VmState.SSHPort
			}
		}
	}
	return kit.ResolveVmSshPort(sp, vmName, persisted)
}

// VmDeployEntryKeys resolves the per-host charly.yml deploy key(s) a VM teardown for deployName
// targets: the deploy IDENTITY key (a VM deploy is keyed by its identity like every other
// substrate), plus — for the DIRECT `charly vm destroy <entity>` path, whose argument is the VM
// ENTITY not a deploy identity — every deploy whose `vm:` cross-ref names that entity. Domain
// identities are unique and never equal an entity a sibling shares, so the From-scan cannot
// over-match sibling beds during a deploy teardown.
func VmDeployEntryKeys(dc *DeployConfig, deployName string) []string {
	var keys []string
	seen := map[string]bool{}
	add := func(k string) {
		if seen[k] {
			return
		}
		if _, ok := dc.Deploy[k]; ok {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	add(deployName)
	// Direct `charly vm destroy <entity>`: the argument is a VM ENTITY, not a deploy
	// identity, so scan for every entry whose `vm:` cross-ref names it. A deploy-identity
	// teardown passes the identity and takes the literal-key path only.
	for key, entry := range dc.Deploy {
		if entry.From == deployName {
			add(key)
		}
	}
	return keys
}
