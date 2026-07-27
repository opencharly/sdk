package vmshared

// poll_reexport.go — the poll/readiness primitive moved to the floor-legal
// package spec (spec/poll.go + spec/readiness.go) so kernel-floor charly files
// reach these bounds without a vmshared import. These thin re-exports keep every
// existing vmshared.X consumer (sdk/deploykit, sdk/kit, the VM/check/preempt/spice
// plugins) compiling unchanged — the SAME sdk-side re-export pattern as
// spec_aliases.go. ONE home per symbol: the definitions live in spec.

import "github.com/opencharly/sdk/spec"

type (
	PollCondition     = spec.PollCondition
	PollConfig        = spec.PollConfig
	PollClass         = spec.PollClass
	ResolvedReadiness = spec.ResolvedReadiness
)

var (
	ErrPollStalled     = spec.ErrPollStalled
	ErrPollCapExceeded = spec.ErrPollCapExceeded
	ErrPollFatal       = spec.ErrPollFatal
	ErrPollConfig      = spec.ErrPollConfig

	// ResolveReadiness + PollUntil are re-exported functions (the resolver + the
	// poll driver).
	ResolveReadiness = spec.ResolveReadiness
	PollUntil        = spec.PollUntil
)

const (
	PollLocal  = spec.PollLocal
	PollRemote = spec.PollRemote
	PollHeavy  = spec.PollHeavy

	ReadinessAbsoluteCapFallback     = spec.ReadinessAbsoluteCapFallback
	ReadinessIntervalHeavyFallback   = spec.ReadinessIntervalHeavyFallback
	ReadinessIntervalLocalFallback   = spec.ReadinessIntervalLocalFallback
	ReadinessIntervalRemoteFallback  = spec.ReadinessIntervalRemoteFallback
	ReadinessNoProgressFallback      = spec.ReadinessNoProgressFallback
	ReadinessPerAttemptFallback      = spec.ReadinessPerAttemptFallback
	ReadinessPerAttemptHeavyFallback = spec.ReadinessPerAttemptHeavyFallback
	ReadinessStopGraceFallback       = spec.ReadinessStopGraceFallback
)
