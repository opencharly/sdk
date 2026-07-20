package kit

// vm_ssh_port.go — the VM guest SSH host-port resolution (P13-KERNEL): shared by
// charly core (the direct-read path, via its own persisted-state lookup) and any
// future in-proc consumer that already holds AllocateAutoPorts. The out-of-process
// candy/plugin-vm keeps its own copy (a documented, deliberately-tolerated
// below-the-export-bar duplicate, candy/plugin-vm/vm_util_copies.go) since it reads
// persisted state over a host seam rather than in-process.

import (
	"fmt"

	"github.com/opencharly/sdk/spec"
)

// ResolveVmSshPort resolves the guest SSH host port from the resolved spec:
//   - ssh.port_auto: true → reuse persistedPort when the caller already resolved one
//     (idempotent across rebuilds), else allocate a free host port.
//   - ssh.port: N        → that fixed port.
//   - neither            → 2222.
//
// The persisted-state READ is the caller's concern (it differs by placement: charly
// core reads its project config directly; an out-of-process plugin reads over a host
// seam) — this is the PURE resolution/allocation decision only.
func ResolveVmSshPort(vm *spec.ResolvedVm, vmName string, persistedPort int) (int, error) {
	if vm.SSH != nil && vm.SSH.PortAuto {
		if persistedPort > 0 {
			return persistedPort, nil
		}
		alloc, err := AllocateAutoPorts([]int{22}, nil)
		if err != nil {
			return 0, fmt.Errorf("vm %q: ssh.port_auto allocation failed: %w", vmName, err)
		}
		return alloc[0].Host, nil
	}
	if vm.SSH != nil && vm.SSH.Port > 0 {
		return vm.SSH.Port, nil
	}
	return 2222, nil
}
