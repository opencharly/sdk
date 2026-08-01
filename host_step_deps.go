package sdk

// host_step_deps.go — re-exports of the in-proc ctx-threading channel for the
// deploy-leg host-engine step bodies, relocated to github.com/opencharly/spec/exec
// (#55 import-purity). HostStepDeps, ContextWithHostStepDeps, and
// HostStepDepsFromCtx live once in spec/exec; the sdk root re-exports them so
// candy call sites (candy/plugin-installstep's OpExecute handler) compile
// UNCHANGED. The deploy-leg bodies recover these via HostStepDepsFromCtx and run
// the SAME deploykit bodies the broker used to run host-side (R3 — one body,
// relocated, not duplicated).

import (
	"github.com/opencharly/spec/exec"
)

// HostStepDeps carries the live, non-serializable inputs a compiled-in class:step plugin needs
// to run a deploy-leg host-engine step body (Builder / LocalPkgInstall / SystemPackages) on the
// host venue. IN-PROC-ONLY (the typed executor + closures cannot cross the wire).
type HostStepDeps = exec.HostStepDeps

// ContextWithHostStepDeps threads the live host-step deps onto ctx for a compiled-in
// class:step plugin's OpExecute handler to recover via HostStepDepsFromCtx.
var ContextWithHostStepDeps = exec.ContextWithHostStepDeps

// HostStepDepsFromCtx recovers the threaded host-step deps (nil when absent — an out-of-process
// placement, or a ctx that never carried them).
var HostStepDepsFromCtx = exec.HostStepDepsFromCtx
