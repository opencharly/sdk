package vmshared

// vm_domain_reexport.go — VmDomainIdentity moved to the floor-legal package spec
// (spec/vm_domain.go) so both kernel-floor charly files and a plugin (candy/plugin-check's
// bed_session.go, #55 W3 B2-full) derive the per-deploy VM domain identity without a vmshared
// import. This thin
// re-export keeps every existing vmshared.VmDomainIdentity consumer (sdk/deploykit,
// sdk/kit, the VM/check/preempt/kube/bundle plugins) — and this package's own
// internal callers — compiling unchanged. ONE home: the definition lives in spec.

import "github.com/opencharly/spec/spec"

var VmDomainIdentity = spec.VmDomainIdentity
