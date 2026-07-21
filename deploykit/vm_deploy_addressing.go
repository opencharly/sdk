package deploykit

import (
	"strings"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
	"github.com/opencharly/sdk/vmshared"
)

// vm_deploy_addressing.go — the BundleConfig-shaped VM deploy-state helpers (FLOOR-SLIM Unit 3,
// relocated from charly/vm_deploy_state.go): each one is PURE over an already-loaded *BundleConfig
// / *spec.ResolvedVm, touching no LoadUnified and no charly-core type. They stay in deploykit
// (rather than vmshared) because they operate on deploykit's OWN BundleConfig type — vmshared
// cannot import deploykit (deploykit already imports vmshared; the reverse is a cycle). The
// charly-core WRITE path (saveVmDeployState/removeVmDeployEntry — pluginPrimaries-registry +
// acquireDeployConfigLock coupled) stays in charly/vm_deploy_state.go and calls these directly.

// ResolveVmSshPort picks the host-side SSH port forward, reusing the persisted vm_state.ssh_port
// (idempotent across rebuilds) when ssh.port_auto is set. The project-config READ is the one
// deploykit-coupled bit (LoadDeployConfigForRead over the per-host overlay); the
// resolution/allocation decision itself is the shared kit.ResolveVmSshPort.
func ResolveVmSshPort(sp *spec.ResolvedVm, vmName string) (int, error) {
	var persisted int
	if sp.SSH != nil && sp.SSH.PortAuto {
		if entry, ok := LoadDeployConfigForRead("charly vm ssh-port").LookupKey("vm:" + vmName); ok && entry.VmState != nil && entry.VmState.SshPort > 0 {
			persisted = entry.VmState.SshPort
		}
	}
	return kit.ResolveVmSshPort(sp, vmName, persisted)
}

// PruneStaleVmDottedTwin removes and returns any OTHER dc.Bundle key that is a dotted deploy name
// whose VmDomainIdentity sanitizes to the SAME domain identity as canonicalKey (also
// VmDomainIdentity-sanitized) — the "stale twin" a prior version's now-eliminated dotted-key
// vm-state write could leave behind: a nested (dotted) deploy's per-host state used to ALSO get
// written under its raw, unsanitized name, racing the canonical "vm:"+VmDomainIdentity(name)-keyed
// write, and poisoning the whole overlay on every subsequent load since a dotted key fails the
// loader's deployment-name validation. Pulled out as its own pure function purely for
// testability. Returns "" when no twin is found.
func PruneStaleVmDottedTwin(dc *BundleConfig, canonicalKey string) string {
	domainID := vmshared.VmDomainIdentity(canonicalKey)
	for k := range dc.Bundle {
		if k == canonicalKey || !strings.Contains(k, ".") {
			continue
		}
		if vmshared.VmDomainIdentity(k) == domainID {
			delete(dc.Bundle, k)
			return k
		}
	}
	return ""
}

// VmDeployEntryKeys resolves the per-host charly.yml bundle key(s) a VM teardown for deployName
// targets. It handles the case where a bundle's name differs from its `vm:<X>` runtime-state key:
// the literal-key delete + the `vm:`-form From-scan below remain for the DIRECT
// `charly vm destroy <entity>` path and any legacy `vm:<name>` teardown key: they target the
// literal deployName key AND — when deployName is "vm:<X>" — every bundle whose `vm:` cross-ref
// names <X>. Because domain identities are unique and never equal an entity a sibling shares, the
// From-scan can no longer over-match sibling beds during a deploy teardown.
func VmDeployEntryKeys(dc *BundleConfig, deployName string) []string {
	var keys []string
	seen := map[string]bool{}
	add := func(k string) {
		if seen[k] {
			return
		}
		if _, ok := dc.Bundle[k]; ok {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	add(deployName)
	// VmNameFromDeployName succeeds only for the prefixed "vm:<entity>" form; a
	// plain-name deployName therefore takes the literal-key path only (no scan,
	// so a non-prefixed name can never over-match unrelated bundles).
	if entity, perr := vmshared.VmNameFromDeployName(deployName); perr == nil {
		for key, entry := range dc.Bundle {
			if entry.From == entity {
				add(key)
			}
		}
	}
	return keys
}
