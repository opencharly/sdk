package kit

// readiness.go — the executor-side readiness seam. The host-side SSHExecutor's
// wait-for-SSH bounds come from the project's defaults.readiness, which only the
// charly HOST can LoadUnified. Rather than the executor self-loading the project
// (a loader coupling that would drag the whole config front-end into kit), the host
// injects its project-aware resolver into ReadinessProvider at init; a standalone
// kit consumer falls back to the built-in defaults (ResolveReadiness(nil) — always
// safe + never-hang).

import "github.com/opencharly/sdk/vmshared"

type (
	PollCondition     = vmshared.PollCondition
	ResolvedReadiness = vmshared.ResolvedReadiness
)

var (
	PollRemote = vmshared.PollRemote
	pollUntil  = vmshared.PollUntil
)

// ReadinessProvider returns the resolved readiness bounds the executors' waits use.
// Defaults to the built-in bounds; charly overrides it at init with its project-aware
// loadedReadiness (charly/readiness_config.go).
var ReadinessProvider = func() ResolvedReadiness {
	rr, _ := vmshared.ResolveReadiness(nil)
	return rr
}
