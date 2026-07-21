package vmshared

import (
	"fmt"
	"strings"
)

// vm_deploy_addressing.go — pure "vm:"-prefixed deploy/CLI ADDRESSING helpers (FLOOR-SLIM Unit 3,
// relocated from charly/vm_deploy_state.go). Both functions touch only strings — zero deploykit/
// spec/kit dependency — so they live here without risking the vmshared->deploykit->vmshared import
// cycle deploykit's own BundleConfig-shaped helpers would create (deploykit already imports
// vmshared; the reverse is forbidden). charly core's vm_deploy_state.go keeps thin wrappers only
// where a caller still needs the bare (unqualified) name.

// VmNameFromDeployName extracts the VM entity name from a deploy-key in the legacy
// "vm:<name>[/<instance>]" form. Callers that hold a schema-v4 deploy key (whose entity comes from
// the node's `vm:` field) resolve the entity a different way (the node's own From field); this
// helper handles the prefixed form (legacy refs + the "vm:<entity>" key the del path builds for
// ledger/teardown keying). The `instance` suffix is preserved for future per-instance addressing
// but currently unused.
func VmNameFromDeployName(deployName string) (string, error) {
	if !strings.HasPrefix(deployName, "vm:") {
		return "", fmt.Errorf("VM deploy name must start with 'vm:' (got %q)", deployName)
	}
	rest := strings.TrimPrefix(deployName, "vm:")
	if rest == "" {
		return "", fmt.Errorf("VM deploy name missing vm-name portion (got %q)", deployName)
	}
	if before, _, ok := strings.Cut(rest, "/"); ok {
		return before, nil
	}
	return rest, nil
}

// SplitVmAddress detects the "vm:"-prefixed CLI ADDRESSING form (`charly bundle add/del
// vm:<name>` / `vm:<parent.child>`) and returns the address with that prefix stripped, plus
// whether it was present. "vm:" here is an ADDRESSING HINT — "resolve this via the vm
// substrate" — NEVER an identity itself; a caller that needs the plain (tree-lookup /
// ledger-identity) form strips it via this helper, one which needs the sanitized dc.Bundle
// key form still applies "vm:"+VmDomainIdentity(...) separately (a DIFFERENT canonical form).
//
// NOT the same job as VmNameFromDeployName (which extracts the VM ENTITY and errors when the
// prefix is ABSENT — a different, already-established, unchanged contract) or VmDomainIdentity
// (which sanitizes dots/slashes for a domain-identity STRING, unconditionally, prefix or not —
// also unchanged).
func SplitVmAddress(name string) (plain string, isVm bool) {
	if strings.HasPrefix(name, "vm:") {
		return strings.TrimPrefix(name, "vm:"), true
	}
	return name, false
}
