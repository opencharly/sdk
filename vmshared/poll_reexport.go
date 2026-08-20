package vmshared

// poll_reexport.go — the poll/readiness primitive moved to the floor-legal
// package spec (spec/poll (poll.go + readiness.go)) so kernel-floor charly files
// reach these bounds without a vmshared import. These thin re-exports keep every
// existing vmshared.X consumer (sdk/deploykit, sdk/kit, the VM/check/preempt/spice
// plugins) compiling unchanged — the SAME sdk-side re-export pattern as
// spec_aliases.go. ONE home per symbol: the definitions live in poll.

import "github.com/opencharly/spec/poll"

type (
	PollCondition     = poll.PollCondition
	PollConfig        = poll.PollConfig
	PollClass         = poll.PollClass
	ResolvedReadiness = poll.ResolvedReadiness
)

var (
	ErrPollStalled     = poll.ErrPollStalled
	ErrPollCapExceeded = poll.ErrPollCapExceeded
	ErrPollFatal       = poll.ErrPollFatal
	ErrPollConfig      = poll.ErrPollConfig

	// ResolveReadiness + PollUntil are re-exported functions (the resolver + the
	// poll driver).
	ResolveReadiness = poll.ResolveReadiness
	PollUntil        = poll.PollUntil
)

const (
	PollLocal  = poll.PollLocal
	PollRemote = poll.PollRemote
	PollHeavy  = poll.PollHeavy

	ReadinessAbsoluteCapFallback     = poll.ReadinessAbsoluteCapFallback
	ReadinessIntervalHeavyFallback   = poll.ReadinessIntervalHeavyFallback
	ReadinessIntervalLocalFallback   = poll.ReadinessIntervalLocalFallback
	ReadinessIntervalRemoteFallback  = poll.ReadinessIntervalRemoteFallback
	ReadinessNoProgressFallback      = poll.ReadinessNoProgressFallback
	ReadinessPerAttemptFallback      = poll.ReadinessPerAttemptFallback
	ReadinessPerAttemptHeavyFallback = poll.ReadinessPerAttemptHeavyFallback
	ReadinessStopGraceFallback       = poll.ReadinessStopGraceFallback
)
