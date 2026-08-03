package sdk

// phase.go — re-exports of the plugin LIFECYCLE phase vocabulary relocated to
// github.com/opencharly/spec/phase (#55 import-purity). The Phase* constants,
// PhaseOrder, and NormalizePhase live once in spec/phase (the single source,
// R3); the sdk root re-exports them so candy call sites compile UNCHANGED.
// This is the LIFECYCLE phase set (bootstrap/schema/load/build/runtime),
// DISTINCT from the step-phase enum in spec/spec/ir_enums.go — do not conflate.

import (
	"github.com/opencharly/spec/phase"
)

// Plugin lifecycle PHASES (F9) — the ordered points at which a plugin participates in charly's
// lifecycle. A plugin DECLARES its phase via ProvidedCapability.Phase (default PhaseRuntime); the
// kernel loads/invokes plugins in phase order. The BOOTSTRAP phase runs BEFORE config
// validation/migration, so an early-running capability can itself be a plugin loaded at the right
// time (today only the no-op candy/plugin-example-bootstrap registers here — neither migrate nor
// egress is a bootstrap plugin; both are verb plugins invoked the normal way).
const (
	PhaseBootstrap = phase.PhaseBootstrap // before config validation/migration; compiled-in only (no validated config exists yet to discover an out-of-process source).
	PhaseSchema    = phase.PhaseSchema    // schema / migration phase
	PhaseLoad      = phase.PhaseLoad      // config-load phase (kind decode, etc.)
	PhaseBuild     = phase.PhaseBuild     // image-build phase (OpEmit / OpResolve)
	PhaseRuntime   = phase.PhaseRuntime   // deploy / runtime phase (OpExecute / OpRun) — the DEFAULT
)

// PhaseOrder lists the phases in ascending load order; the kernel iterates plugins phase-ascending
// (bootstrap first). It is the authority for ordering + membership.
var PhaseOrder = phase.PhaseOrder

// NormalizePhase maps an empty or unrecognized declared phase to the default (PhaseRuntime), so a
// plugin that declares no phase participates at the normal (runtime) time.
var NormalizePhase = phase.NormalizePhase
