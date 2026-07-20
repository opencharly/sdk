package sdk

// Operation selectors (the op.Op / InvokeRequest.Op wire value). Each provider class uses
// the subset it needs. This is the SINGLE SOURCE for the selectors (R3): charly's package
// main aliases these (provider.go), and an out-of-tree / compiled-in plugin's Invoke
// dispatch compares req.GetOp() against them — so a kind candy checks sdk.OpLoad, a
// step/deploy candy sdk.OpEmit/sdk.OpExecute, a builder candy sdk.OpResolve.
const (
	OpRun      = "run"      // verb: run a check / live-container probe → CheckResult
	OpLoad     = "load"     // kind: decode a node into its typed entity
	OpValidate = "validate" // kind: closed/concrete CUE validation → Diagnostics
	OpEmit     = "emit"     // deploy/step: emit an InstallPlan / Containerfile fragment
	OpExecute  = "execute"  // deploy/step: execute against a venue (streamed)
	OpResolve  = "resolve"  // builder: resolve a builder image + steps (build-time multi-stage)
	OpBuild    = "build"    // build: dispatch the image-build / generate engine host-side (F10 HostBuild seam)

	// OpCompile is the K4-B deploy-COMPILE selector (command:bundle): the host's
	// deployAddCmd.compileNodePlans computes the per-node selection and Invokes the
	// command:bundle plugin's OpCompile with a spec.DeployCompileRequest; the plugin
	// re-hydrates the resolved-project envelope (HostBuild("resolved-project")) +
	// loops deploykit.BuildDeployPlan, returning []spec.InstallPlanView the host
	// re-materializes. A generic action selector (never a provider word — F11).
	OpCompile = "compile"

	// OpDispatch is the K4-C deploy-DISPATCH selector (command:bundle, P13-KERNEL spike #1):
	// deployAddCmd.dispatchNode, for the root-level (non-nested) Add path, Invokes the
	// command:bundle plugin's OpDispatch with a spec.DeployDispatchRequest instead of calling
	// ResolveTarget(...).Add(...) directly in-process. The plugin relays the request VERBATIM
	// to HostBuild("deploy-dispatch"), where the host reconstructs the config + DeployContext
	// and runs the actual ResolveTarget → UnifiedDeployTarget.Add/Del/Test/Update — proving the
	// reverse-channel broker nests correctly when the OUTER call originates from a plugin
	// (rather than the host, as OpCompile's HostBuild("resolved-project") call does today).
	OpDispatch = "dispatch"

	// OpCollectContext + OpReverse are the DEPLOY-TIME builder-IR legs of an externalized
	// detection-builder plugin (cargo/npm/pixi/aur). A builder's build-time multi-stage is
	// resolved by its OpResolve leg (C10); these two carry the per-builder deploy-time IR
	// shim — the stage-context the compiler records on a BuilderStep + that step's teardown
	// ops — out-of-process. BOTH are invoked HOST-SIDE in the build PRE-PASS (BEFORE the pure
	// BuildDeployPlan compile reads the result), never inside the pure compiler.
	OpCollectContext = "collect-context" // builder: per-candy stage-context keys → BuilderCollectReply
	OpReverse        = "reverse"         // builder: teardown ops for a resolved stage context → BuilderReverseReply

	// F6 — the SUBSTRATE LIFECYCLE selectors (host→plugin on Provider.Invoke): a deploy
	// substrate plugin brings its OWN host-side venue lifecycle. PrepareVenue/VenueExecutor
	// return a VenueDescriptor the HOST re-materializes into a real DeployExecutor (the live
	// executor never crosses the wire); the rest carry name/node/opts in, error/StatusInfo out.
	OpPrepareVenue     = "prepare-venue"     // lifecycle: build the venue → VenueDescriptor (re-materialized host-side)
	OpArtifactKey      = "artifact-key"      // lifecycle: the per-deploy artifact ledger key
	OpPostApply        = "post-apply"        // lifecycle: post-walk finalize on the venue
	OpTeardownExecutor = "teardown-executor" // lifecycle: the executor for Del → VenueDescriptor
	OpPostTeardown     = "post-teardown"     // lifecycle: drop venue artifacts (image/domain)
	OpStart            = "start"             // lifecycle: start the venue
	OpStop             = "stop"              // lifecycle: stop the venue
	OpStatus           = "status"            // lifecycle: venue status → StatusInfo
	OpLogs             = "logs"              // lifecycle: stream venue logs
	OpShell            = "shell"             // lifecycle: NON-interactive in-container exec CAPTURE (charly service — output-in-reply); interactive shell is OpAttach
	OpAttach           = "attach"            // F12 lifecycle: LIVE-STDIO attach — charly shell (-it TTY) + charly cmd (-i, stdin piped). The plugin exec.RunInteractive's a host-resolved #PodLiveStdioPlan.script; the host reverse-server holds the operator's terminal (stdio never crosses the wire)
	OpRebuild          = "rebuild"           // lifecycle: rebuild the venue (charly update)

	// OpConfigWrite is the POD config-WRITE selector (P11, Q1=(a)): the HOST `charly config`
	// command resolves the full QuadletConfig + the host-side target paths and Invokes the
	// deploy:pod plugin's OpConfigWrite with a spec.PodConfigWriteRequest; the plugin renders the
	// .container/.pod/sidecar/tunnel file CONTENTS (deploykit.GenerateQuadlet + the pod/sidecar/
	// tunnel generators) and os.WriteFiles them at the exact modes (same-host, compiled-in),
	// returning the written paths. RESOLVE + host side-effects (secret provisioning, saveDeployState,
	// enc-mount, data-seed, systemctl) stay in the host command — the plugin owns only the
	// config-WRITE (Ruling C). Distinct from the venue-lifecycle Ops: host-initiated, not a deploy.
	OpConfigWrite = "config-write"

	// OpConfigSetup / OpConfigRemove are the P13-KERNEL config-BODY selectors: the deploy:pod
	// plugin's Invoke handles these carrying #PodConfigSetupRequest / #PodConfigRemoveRequest
	// VERBATIM as Params — the direction-flip counterpart of OpConfigWrite (which stayed
	// host-initiated/plugin-rendered): host_build_pod_config.go's hostBuildPodConfigSetup/
	// hostBuildPodConfigRemove now FORWARD onward to the plugin (resolve the deploy:pod provider +
	// InvokeWithExecutor, the SAME primitive InvokeProvider/grpcSubstrateLifecycle use) instead of
	// running the ported BoxConfigSetupCmd/BoxConfigRemoveCmd orchestration in-core. The plugin
	// calls back the narrow "pod-config-*" HostBuild seams (sdk/schema/seam.cue) for the
	// genuinely host/loader/registry-coupled sub-steps.
	OpConfigSetup  = "config-setup"
	OpConfigRemove = "config-remove"

	OpStatusCollect = "status-collect" // command:status: programmatic status collection → []spec.DeploymentStatus (distinct from lifecycle OpStatus)

	// OpStatusCollectAll is the K6 whole-subsystem status FAN-OUT + deploy-cone ENRICHMENT
	// selector: verb:status-fanout (candy/plugin-substrate) serves it, taking a
	// spec.StatusSubstrateRequest and returning a spec.StatusSubstrateReply — the SAME wire
	// shape the "status-substrate" HostBuild seam already carries. The host's thin forward
	// (charly/status_substrate_host.go) is pure dispatch (no status business logic); the
	// plugin owns the fan-out (calling its own per-word OpStatusCollect handlers directly, an
	// in-package call — no registry needed for that leg), the pod/vm deploy-cone enrichment
	// (kit.ExtractMetadata/kit.ResolveBoxName/deploykit.QuadletDir/deploykit.
	// ResolveBoxEngineForDeploy — all sdk-portable), and the sort. Distinct from
	// OpStatusCollect (the single-word per-substrate collector op the SAME provider ALSO
	// serves on kind:pod/vm/k8s/local/android).
	OpStatusCollectAll = "status-collect-all"

	// OpPreresolve is the generalized host-side deploy preresolver (F6): a substrate plugin
	// declares a preresolve step the host runs BEFORE apply, returning the opaque JSON the host
	// ships in DeployVenue.Substrate (the wire-backed generalization of the in-core k8s/android
	// preresolvers).
	OpPreresolve = "preresolve"

	// OpBootstrap is the BOOTSTRAP-PHASE hook (F9): the kernel invokes a Phase=="bootstrap"
	// plugin BEFORE config validation, passing the RAW project config bytes
	// (params {"config": <bytes>}) and applying any transformed bytes the plugin returns
	// (reply {"config": <bytes>}) — a generic pre-validation transform hook (a no-op bootstrap
	// plugin returns the bytes unchanged). It is NOT the migration path: config-schema migration
	// is candy/plugin-migrate's command:migrate over OpRun (a whole-project file-walk that runs
	// on the config exactly when it cannot load), never a raw-byte bootstrap transform. Bootstrap
	// plugins are COMPILED-IN (in-proc), so this hook never re-enters the validated-config load.
	OpBootstrap = "bootstrap"
)
