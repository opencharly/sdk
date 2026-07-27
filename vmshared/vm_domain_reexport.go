package vmshared

// vm_domain_reexport.go — VmDomainIdentity moved to the floor-legal package spec
// (spec/vm_domain.go) so kernel-floor charly files (host_build_check_bed.go)
// derive the per-deploy VM domain identity without a vmshared import. This thin
// re-export keeps every existing vmshared.VmDomainIdentity consumer (sdk/deploykit,
// sdk/kit, the VM/check/preempt/kube/bundle plugins) — and this package's own
// internal callers — compiling unchanged. ONE home: the definition lives in spec.

import "github.com/opencharly/sdk/spec"

var VmDomainIdentity = spec.VmDomainIdentity
