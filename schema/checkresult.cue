// CUE schema for the check-engine's per-step VERDICT envelope (FLOOR-SLIM Unit 4). NOT an
// authoring kind (never in #Node/#Op) — a pure generated wire/render struct, single-sourced
// here so `task cue:gen` produces the Go struct charly core's registry-coupled floor files
// (provider.go/provider_verb.go/verb_builtins.go/unified_targets.go/provider_checkenv.go)
// reference directly (spec.CheckResult), with zero new sdk/kit import.
//
// #CheckResult covers every field EXCEPT the engine-internal `DeadlineExceeded` retry
// signal — the ONE spike-proven exception (P12, cited at sdk/kit/checkrun_seam.go and
// sdk/schema/seam.cue): `DeadlineExceeded bool json:"-"` has no gengotypes construct (a
// field that exists in memory but is excluded from marshaling). sdk/kit.CheckResult embeds
// this generated type and adds ONLY that one hand-written field back.
//
// FLOOR-SLIM deliberately renames the wire keys to snake_case (Op→op, Verb→verb,
// Status→status, Message→message, Elapsed→elapsed) — the former hand-written type carried
// NO json tag on these fields, so encoding/json defaulted to the bare, inconsistent
// PascalCase Go field name. This is a documented, deliberate breaking wire-format fix for
// `--format json`/TAP consumers of `charly check box/live/run`, not an accident: it brings
// CheckResult in line with every other CUE-sourced wire type's snake_case convention. Every
// field that ALWAYS serialized before (op/verb/status/message/elapsed) stays REQUIRED (`!`)
// here so gengotypes omits `omitempty` — an omitempty regression would silently drop
// zero-valued fields from output, a SEPARATE wire change the rename must not introduce.
// Every field that already carried `,omitempty` (attempts/total_elapsed/captured_value)
// stays optional (`?`).
//
// Status is carried as a plain int (@go(Status,type=Status) — Status is the check-engine's
// pass/fail/skip enum, HAND-WRITTEN in spec/status_result.go: gengotypes has no construct
// for an iota-based enum + String() method, so CUE owns the wire VALUE SET (an int) and Go
// owns the formatting behavior (String()), mirroring the #SubstrateKind split (status.cue) —
// there the enum is string-backed and suppressed via @go(-); here it is int-backed and
// referenced directly since there is no separate disjunction def to suppress.
//
// Elapsed / TotalElapsed carry a nanosecond count wire-typed as time.Duration
// (@go(Elapsed,type=time.Duration)) — the RDD spike (T-P12, cited in /charly-internals:go)
// proved a custom-scalar @go(,type=…) override generates faithfully.
#CheckResult: {
	op!:             #Op    @go(Op, type=*Op)
	verb!:           string @go(Verb)
	status!:         int    @go(Status, type=Status)
	message!:        string @go(Message)
	elapsed!:        int    @go(Elapsed, type=time.Duration)
	attempts?:       int    @go(Attempts, type=int)
	total_elapsed?:  int    @go(TotalElapsed, type=time.Duration)
	captured_value?: string @go(CapturedValue)
}

// #CheckEnv is the SINGLE-SOURCED scalar snapshot of a check verb's invocation context (K1-unblock
// W3 Unit B) — the ONE #CheckEnv def now generating the struct all THREE of its consumers share
// (a hand-written mirror per consumer is the exact "wire type not CUE-generated" violation SDD
// forbids in fresh code): (1) charly/provider_checkenv.go's host-side CheckEnv, filled by
// snapshotCheckEnv from a live *kit.Runner and threaded to an out-of-process verb's Invoke
// envelope; (2) sdk's out-of-process verb-serve decode (sdk/checkverb.go), which reconstructs a
// kit.CheckContext's scalar legs from this exact snapshot; (3) candy/plugin-check's
// InvokeProvider-backed VerbResolver (verb_resolver.go), which marshals this same shape when
// asking the host to dispatch a verb on its behalf. A 4th consumer (charly/plugin_dispatch_reverse.go's
// InvokeProvider host handler) DECODES it host-side to construct a detached kit.CheckContext for
// a CheckVerbProvider target — the SAME snapshot, not a second shape.
//
// Every field is optional (a caller fills only what it has — an in-proc/live snapshot has a live
// Runner to read from; a box-mode run has no ContainerName/Venue; a detached construction may
// lack DialTimeoutNs). container_name/venue are HOST-COMPUTED-ONLY fields (never authored,
// carried for the out-of-process appium/vm-target verbs that need charly's naming convention
// without re-deriving it) — present on the wire regardless of which of the three marshal sites
// populates them, since all three now share this one shape.
#CheckEnv: {
	box?:             string   @go(Box)
	instance?:        string   @go(Instance)
	mode?:            string   @go(Mode) // "live" | "box"
	container_name?:  string   @go(ContainerName)
	distros?:         [...string] @go(Distros)
	venue?:           string   @go(Venue)      // r.Exec.Venue()
	venue_kind?:      string   @go(VenueKind)  // r.Exec.Kind()
	dial_timeout_ns?: int      @go(DialTimeoutNs, type=int64)
}
