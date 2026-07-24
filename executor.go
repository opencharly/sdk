package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	plugin "github.com/hashicorp/go-plugin"

	pb "github.com/opencharly/sdk/proto"
	"github.com/opencharly/sdk/spec"
)

// servedBroker is the go-plugin GRPCBroker captured when this plugin's gRPC server
// starts (grpcPlugin.GRPCServer in sdk.go). A deploy/step/builder plugin dials the
// host's E3b reverse-channel ExecutorService through it. One broker per plugin
// process (go-plugin's model), so a package var is the natural home.
var servedBroker *plugin.GRPCBroker

// Executor is the plugin-side handle to the host's live DeployExecutor over the E3b
// reverse channel. An out-of-process deploy/step/builder plugin runs shell/SSH ops on
// the real venue by calling these; the host executes them with the executor it stood
// up on the broker for this Invoke. The plugin never holds the (unmarshallable)
// executor itself.
type Executor struct {
	client pb.ExecutorServiceClient
}

// ExecutorFromInvoke dials the host's ExecutorService using the broker id the host
// passed in InvokeRequest.executor_broker_id. Errors if this plugin was not served
// over go-plugin (no broker) or the id is 0 (no executor attached — a verb/kind op,
// or a deploy op the host ran in-proc).
func ExecutorFromInvoke(brokerID uint32) (*Executor, error) {
	if servedBroker == nil {
		return nil, errors.New("sdk: no go-plugin broker (plugin not served over go-plugin)")
	}
	if brokerID == 0 {
		return nil, errors.New("sdk: no host executor attached (executor_broker_id=0)")
	}
	conn, err := servedBroker.Dial(brokerID)
	if err != nil {
		return nil, err
	}
	return &Executor{client: pb.NewExecutorServiceClient(conn)}, nil
}

// NewInProcExecutor wraps an in-proc pb.ExecutorServiceClient (an adapter delegating
// DIRECTLY to the host's executorReverseServer, no socket) as an *Executor — the
// IN-PROCESS twin of the go-plugin broker path in ExecutorFromInvoke. It is what lets a
// COMPILED-IN plugin reach the SAME reverse-channel seam (HostBuild / RunHostStep / …) an
// out-of-process plugin dials over gRPC: the host threads the resulting *Executor onto the
// Invoke context (ContextWithExecutor), and the plugin picks it up via ExecutorForInvoke —
// so the plugin's Invoke code is byte-identical in both placements (placement-invisible
// above the registry, the whole point of the plugin abstraction).
func NewInProcExecutor(client pb.ExecutorServiceClient) *Executor { return &Executor{client: client} }

// executorCtxKey keys the in-proc *Executor carried on the Invoke context (the compiled-in
// placement's reverse channel — an out-of-process plugin gets its executor via the broker id
// in the request instead). Private so only ContextWithExecutor / executorFromContext touch it.
type executorCtxKey struct{}

// ContextWithExecutor returns ctx carrying an in-proc *Executor. The host's in-proc dispatch
// (inprocProvider.InvokeWithExecutor) calls this before invoking a compiled-in plugin so the
// plugin's ExecutorForInvoke can reach the reverse channel without a broker.
func ContextWithExecutor(ctx context.Context, e *Executor) context.Context {
	return context.WithValue(ctx, executorCtxKey{}, e)
}

// executorFromContext returns the in-proc *Executor carried on ctx, if any.
func executorFromContext(ctx context.Context) (*Executor, bool) {
	e, ok := ctx.Value(executorCtxKey{}).(*Executor)
	return e, ok && e != nil
}

// ExecutorForInvoke resolves the host executor for a plugin's Invoke, transport-invisibly:
// an IN-PROC compiled-in plugin gets it from the context (ContextWithExecutor); an
// OUT-OF-PROCESS plugin falls back to the go-plugin broker id in its InvokeRequest. Plugin
// Invoke code calls this ONE accessor and works in either placement unchanged.
func ExecutorForInvoke(ctx context.Context, brokerID uint32) (*Executor, error) {
	if e, ok := executorFromContext(ctx); ok {
		return e, nil
	}
	return ExecutorFromInvoke(brokerID)
}

// Venue returns the host executor's stable venue identifier.
func (e *Executor) Venue(ctx context.Context) (string, error) {
	r, err := e.client.Venue(ctx, &pb.Empty{})
	if err != nil {
		return "", err
	}
	return r.GetVenue(), nil
}

// RunSystem runs a root (sudo) script on the venue; optsJSON is a marshalled EmitOpts
// (nil for none). A non-empty reply error is the script's failure on the venue.
func (e *Executor) RunSystem(ctx context.Context, script string, optsJSON []byte) error {
	return runErr(e.client.RunSystem(ctx, &pb.RunRequest{Script: script, OptsJson: optsJSON}))
}

// RunUser runs an unprivileged script on the venue (see RunSystem).
func (e *Executor) RunUser(ctx context.Context, script string, optsJSON []byte) error {
	return runErr(e.client.RunUser(ctx, &pb.RunRequest{Script: script, OptsJson: optsJSON}))
}

func runErr(r *pb.RunReply, err error) error {
	if err != nil {
		return err
	}
	if r.GetError() != "" {
		return errors.New(r.GetError())
	}
	return nil
}

// PutFile places file content at a path on the venue — the deploy/step file-PLACEMENT
// leg. An out-of-process deploy/step plugin that EXECUTES an InstallPlan's steps ships
// the bytes (a service unit, an env.d file, the charly binary, a builder artifact);
// the host materializes them and delegates to the live DeployExecutor.PutFile.
// ownerRoot == true installs the file as root:root (root-owned system paths); mode is
// the octal permission bits. Binary-safe (proto bytes). A non-empty reply error is the
// placement failure on the venue.
func (e *Executor) PutFile(ctx context.Context, remotePath string, content []byte, mode uint32, ownerRoot bool) error {
	r, err := e.client.PutFile(ctx, &pb.PutFileRequest{
		Path:      remotePath,
		Content:   content,
		Mode:      mode,
		OwnerRoot: ownerRoot,
	})
	if err != nil {
		return err
	}
	if r.GetError() != "" {
		return errors.New(r.GetError())
	}
	return nil
}

// RunCapture runs a command on the venue and returns stdout/stderr/exit separately —
// the check-verb capture leg (an out-of-process exec-based check verb probing the live
// container). A non-empty reply error is an EXECUTION failure, NOT a non-zero exit
// (which rides the returned exit code). Mirrors kit.Executor.RunCapture over the wire.
func (e *Executor) RunCapture(ctx context.Context, script string) (stdout, stderr string, exit int, err error) {
	r, callErr := e.client.RunCapture(ctx, &pb.RunRequest{Script: script})
	if callErr != nil {
		return "", "", 0, callErr
	}
	if r.GetError() != "" {
		return r.GetStdout(), r.GetStderr(), int(r.GetExitCode()), errors.New(r.GetError())
	}
	return r.GetStdout(), r.GetStderr(), int(r.GetExitCode()), nil
}

// RunInteractive runs a command on the venue wired to the operator's LIVE TTY (F12): the HOST
// reverse-server runs it inheriting os.Stdin/os.Stdout/os.Stderr (= the operator's terminal), so
// stdio never crosses the wire — only script→exit. Blocks until the session ends. The live-stdio
// sibling of RunCapture (consumers: charly shell/-cmd via the plugin's OpAttach).
func (e *Executor) RunInteractive(ctx context.Context, script string) (int, error) {
	r, callErr := e.client.RunInteractive(ctx, &pb.RunRequest{Script: script})
	if callErr != nil {
		return -1, callErr
	}
	if r.GetError() != "" {
		return int(r.GetExitCode()), errors.New(r.GetError())
	}
	return int(r.GetExitCode()), nil
}

// RunStream runs a command on the venue streaming stdout/stderr LIVE to the operator (F12; no
// stdin). Blocks until exit. Consumer: charly logs --follow via the plugin's OpLogs.
func (e *Executor) RunStream(ctx context.Context, script string) (int, error) {
	r, callErr := e.client.RunStream(ctx, &pb.RunRequest{Script: script})
	if callErr != nil {
		return -1, callErr
	}
	if r.GetError() != "" {
		return int(r.GetExitCode()), errors.New(r.GetError())
	}
	return int(r.GetExitCode()), nil
}

// VenueHasTool reports whether tool is on PATH on the venue — an EXEC-based check verb's
// tool-presence probe over the reverse channel.
func (e *Executor) VenueHasTool(ctx context.Context, tool string) bool {
	_, _, exit, err := e.RunCapture(ctx, "command -v "+tool+" >/dev/null 2>&1")
	return err == nil && exit == 0
}

// VenueCapture runs a command on the venue and returns stdout, surfacing stderr on a
// non-zero exit — an EXEC-based check verb's capture-or-fail helper over the reverse channel.
func (e *Executor) VenueCapture(ctx context.Context, script string) (string, error) {
	stdout, stderr, exit, err := e.RunCapture(ctx, script)
	if err != nil {
		return "", err
	}
	if exit != 0 {
		if s := strings.TrimSpace(stderr); s != "" {
			return "", fmt.Errorf("%s", s)
		}
		return "", fmt.Errorf("command exited %d", exit)
	}
	return stdout, nil
}

// VenueRunSilent runs a command on the venue discarding output, returning an error on a
// non-zero exit — an EXEC-based check verb's fire-and-forget helper over the reverse channel.
func (e *Executor) VenueRunSilent(ctx context.Context, script string) error {
	_, _, exit, err := e.RunCapture(ctx, script)
	if err != nil {
		return err
	}
	if exit != 0 {
		return fmt.Errorf("command exited %d", exit)
	}
	return nil
}

// GetFile reads a venue file back to the host (asRoot reads via sudo) — the check-verb
// artifact-pull leg (a screenshot / recording produced on the venue).
func (e *Executor) GetFile(ctx context.Context, path string, asRoot bool) ([]byte, error) {
	r, callErr := e.client.GetFile(ctx, &pb.GetFileRequest{Path: path, AsRoot: asRoot})
	if callErr != nil {
		return nil, callErr
	}
	if r.GetError() != "" {
		return nil, errors.New(r.GetError())
	}
	return r.GetContent(), nil
}

// RunHostStep is the HOST-ENGINE channel leg (the generalization of the former F3 build channel): a
// deploy/step plugin walking an InstallPlan that hits one of the five step kinds it cannot
// execute itself — BuilderStep (podman / makepkg / EnsureImagePresent), LocalPkgInstallStep,
// SystemPackagesStep (the DistroConfig package-template render), an act-verb OpStep (a
// builtin ProvisionActor that needs the in-proc registry), or an ExternalPluginStep (a verb
// served by ANOTHER out-of-process plugin, dispatched over a nested reverse channel) — drives
// this. The host reconstructs the step, runs the existing in-core machinery on the host,
// applies the effect onto the venue, and returns the step's recorded reverse ops. The plugin
// folds them into its DeployReply (sdk.BuildDeployReply) so `charly bundle del` replays them
// (record-and-replay teardown). The plugin owns the plan WALK; the host owns the host ENGINE.
// A non-nil error is a host-engine/apply FAILURE on the venue.
func (e *Executor) RunHostStep(ctx context.Context, step spec.InstallStepView, optsJSON []byte) ([]spec.ReverseOp, error) {
	stepJSON, err := json.Marshal(step)
	if err != nil {
		return nil, err
	}
	r, callErr := e.client.RunHostStep(ctx, &pb.HostStepRequest{StepJson: stepJSON, OptsJson: optsJSON})
	if callErr != nil {
		return nil, callErr
	}
	if r.GetError() != "" {
		return nil, errors.New(r.GetError())
	}
	var ops []spec.ReverseOp
	if len(r.GetReverseOpsJson()) > 0 {
		if err := json.Unmarshal(r.GetReverseOpsJson(), &ops); err != nil {
			return nil, err
		}
	}
	return ops, nil
}

// InvokeProviderOpts carries the OPTIONAL extras to an InvokeProvider peer-dispatch call. The zero
// value is byte-identical to the pre-S1 behavior: no venue descriptor, so the host threads the
// CALLING plugin's own enclosing executor (if any) onto the target — exactly as before this field
// existed.
type InvokeProviderOpts struct {
	// VenueDescriptor optionally supplies a SELF-DESCRIBED venue (S1 — the
	// venue-scoped-executor-session seam): the host re-materializes it into a FRESH DeployExecutor
	// and threads THAT onto the target's InvokeWithExecutor instead of the caller's own executor.
	// Use this when the calling plugin holds no enclosing executor of its own (e.g. a verb/kind
	// Invoke with no deploy-context broker) but still wants the target Invoked WITH a live venue.
	// Nil (the default) — no descriptor; the caller's own executor, if any, is forwarded unchanged.
	VenueDescriptor *spec.VenueDescriptor

	// ExtraRef optionally supplies a canonical candy ref (S3b — the Pass-2 lazy-connect gap) for
	// the host's S2 lazy-connect fallback: connectPluginByWordRef(class, word, ExtraRef). Empty
	// (the default) only ever reaches Pass-1 (the calling project's own candy closure) — a target
	// declared nowhere in that closure but resolvable via an explicit @github canonical ref (the
	// same Pass-2 fetch the credential/vm/kube host adapters already use) needs this set.
	ExtraRef string
}

// InvokeProvider asks the host to invoke ANOTHER provider (class, word, op) on this plugin's behalf
// (F10 plugin↔plugin) — the host resolves it in the registry (lazily connecting it from the
// project's candy closure on a miss, S2) and Invokes it, returning the raw result JSON. params/env
// are the op's plugin_input / env (nil for none). opts carries the optional S1 venue descriptor +
// the optional S3b canonical-ref fallback; the zero value InvokeProviderOpts{} reproduces the
// pre-S1 behavior exactly.
func (e *Executor) InvokeProvider(ctx context.Context, class, word, op string, params, env []byte, opts InvokeProviderOpts) ([]byte, error) {
	req := &pb.InvokeProviderRequest{Class: class, Reserved: word, Op: op, ParamsJson: params, EnvJson: env, ExtraRef: opts.ExtraRef}
	if opts.VenueDescriptor != nil {
		vdj, err := json.Marshal(opts.VenueDescriptor)
		if err != nil {
			return nil, fmt.Errorf("sdk: InvokeProvider: marshal venue descriptor: %w", err)
		}
		req.VenueDescriptorJson = vdj
	}
	r, err := e.client.InvokeProvider(ctx, req)
	if err != nil {
		return nil, err
	}
	return r.GetResultJson(), nil
}

// HostBuild asks the host to run the registered host-builder for kind (F10 host-build) with the
// opaque spec, returning the builder's opaque result JSON. A non-empty reply error is a build
// failure (the RPC itself succeeded).
func (e *Executor) HostBuild(ctx context.Context, kind string, spec []byte) ([]byte, error) {
	r, err := e.client.HostBuild(ctx, &pb.HostBuildRequest{Kind: kind, SpecJson: spec})
	if err != nil {
		return nil, err
	}
	if r.GetError() != "" {
		return nil, errors.New(r.GetError())
	}
	return r.GetResultJson(), nil
}

// DescribeProvider asks the host for the CACHED capability metadata of another provider by
// (class, word) — no live Describe round-trip, no Invoke (K5-A item 2). found=false means no
// CONNECTED provider resolves (class, word); a plugin routing on this can distinguish that from a
// transport failure, which the returned error alone would not. contract is nil unless the
// resolved provider is class="step" and declares one (F3) — nil for every other class, exactly
// mirroring ProvidedCapability.step_contract's own optionality.
func (e *Executor) DescribeProvider(ctx context.Context, class, word string) (found bool, contract *StepContract, err error) {
	r, err := e.client.DescribeProvider(ctx, &pb.DescribeProviderRequest{Class: class, Word: word})
	if err != nil {
		return false, nil, err
	}
	if !r.GetFound() {
		return false, nil, nil
	}
	if sc := r.GetStepContract(); sc != nil {
		contract = &StepContract{Scope: sc.GetScope(), Venue: int(sc.GetVenue()), Gate: sc.GetGate(), Emits: sc.GetEmits()}
	}
	return true, contract, nil
}
