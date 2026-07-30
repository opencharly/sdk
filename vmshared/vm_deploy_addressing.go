package vmshared

import "github.com/opencharly/spec/spec"

// vm_deploy_addressing.go — re-export FORWARDERS for the pure "vm:"-prefixed deploy/CLI ADDRESSING
// helpers, RELOCATED to spec/spec/vm_domain.go (#55 vmshared Bucket B). charly core reaches them as
// spec.* directly (import-purity); the plugin/test callers that hold the vmshared.* name keep it via
// these forwarders. Both are pure string helpers; the spec home keeps them beside VmDomainIdentity,
// the sibling always-floor-legal VM-addressing leaf.

// VmNameFromDeployName re-exports spec.VmNameFromDeployName. See spec/spec/vm_domain.go.
var VmNameFromDeployName = spec.VmNameFromDeployName

// SplitVmAddress re-exports spec.SplitVmAddress. See spec/spec/vm_domain.go.
var SplitVmAddress = spec.SplitVmAddress
