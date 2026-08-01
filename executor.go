package sdk

// executor.go — re-exports of the plugin-side Executor handle to the host's live
// DeployExecutor over the E3b reverse channel, relocated to
// github.com/opencharly/spec/exec (#55 import-purity). The Executor type, its
// transport-invisible accessors (ExecutorFromInvoke/NewInProcExecutor/
// ContextWithExecutor/ExecutorFromContext/ExecutorForInvoke), and every
// venue/host-step method (Venue/RunSystem/RunUser/PutFile/RunCapture/
// RunInteractive/RunStream/Venue*/GetFile/RunHostStep/InvokeProvider/HostBuild/
// DescribeProvider) live once in spec/exec; the sdk root re-exports them so candy
// call sites compile UNCHANGED. InvokeProviderOpts is re-exported from spec/ops.
//
// StepContract: the SDK-facing authoring form (string Scope / int Venue / string
// Gate, in schema.go) is NOT re-exported here — it stays the authoring shape candies
// construct. spec/exec.Executor.DescribeProvider returns *spec.StepContract (the
// TYPED form), which charly core already uses directly; candy callers that read the
// returned contract touch only .Emits (bool), which is field-identical across both
// forms, so the alias is call-site-compatible. The string→typed boundary conversion
// lives in spec/exec.DescribeProvider via spec.ScopeFromName/spec.Venue/spec.Gate.

import (
	"github.com/opencharly/spec/exec"
	"github.com/opencharly/spec/ops"
)

// Executor is the plugin-side handle to the host's live DeployExecutor over the E3b
// reverse channel. An out-of-process deploy/step/builder plugin runs shell/SSH ops on
// the real venue by calling these; the host executes them with the executor it stood
// up on the broker for this Invoke. The plugin never holds the (unmarshallable)
// executor itself.
type Executor = exec.Executor

// ExecutorFromInvoke dials the host's ExecutorService using the broker id the host
// passed in InvokeRequest.executor_broker_id. Errors if this plugin was not served
// over go-plugin (no broker) or the id is 0 (no executor attached — a verb/kind op,
// or a deploy op the host ran in-proc).
var ExecutorFromInvoke = exec.ExecutorFromInvoke

// NewInProcExecutor wraps an in-proc pb.ExecutorServiceClient (an adapter delegating
// DIRECTLY to the host's executorReverseServer, no socket) as an *Executor — the
// IN-PROCESS twin of the go-plugin broker path in ExecutorFromInvoke.
var NewInProcExecutor = exec.NewInProcExecutor

// ContextWithExecutor returns ctx carrying an in-proc *Executor. The host's in-proc
// dispatch calls this before invoking a compiled-in plugin so the plugin's
// ExecutorForInvoke can reach the reverse channel without a broker.
var ContextWithExecutor = exec.ContextWithExecutor

// ExecutorFromContext returns the in-proc *Executor carried on ctx, if any. The public
// counterpart to ContextWithExecutor.
var ExecutorFromContext = exec.ExecutorFromContext

// ExecutorForInvoke resolves the host executor for a plugin's Invoke, transport-invisibly:
// an IN-PROC compiled-in plugin gets it from the context (ContextWithExecutor); an
// OUT-OF-PROCESS plugin falls back to the go-plugin broker id in its InvokeRequest.
var ExecutorForInvoke = exec.ExecutorForInvoke

// InvokeProviderOpts carries the OPTIONAL extras to an InvokeProvider peer-dispatch call.
// The zero value is byte-identical to the pre-S1 behavior.
type InvokeProviderOpts = ops.InvokeProviderOpts
