package sdk

// ops.go — re-exports of the operation selectors (the op.Op / InvokeRequest.Op
// wire value), the EphemeralPanicMarker, the ResultJSON reply builder, and
// InvokeProviderOpts relocated to github.com/opencharly/spec/ops (#55
// import-purity). The definitions + their long rationale live once in spec/ops
// (the single source, R3); the sdk root re-exports them so candy call sites
// compile UNCHANGED — a kind candy checks sdk.OpLoad, a step/deploy candy
// sdk.OpEmit/sdk.OpExecute, a builder candy sdk.OpResolve.

import (
	"github.com/opencharly/spec/ops"
)

const (
	// verb/kind/deploy/step/builder operation selectors.
	OpRun      = ops.OpRun      // verb: run a check / live-container probe → CheckResult
	OpLoad     = ops.OpLoad     // kind: decode a node into its typed entity
	OpValidate = ops.OpValidate // kind: closed/concrete CUE validation → Diagnostics
	OpEmit     = ops.OpEmit     // deploy/step: emit an InstallPlan / Containerfile fragment
	OpExecute  = ops.OpExecute  // deploy/step: execute against a venue (streamed)
	OpResolve  = ops.OpResolve  // builder: resolve a builder image + steps (build-time multi-stage)
	OpBuild    = ops.OpBuild    // build: dispatch the image-build / generate engine host-side (F10 HostBuild seam)

	// OpCompile is the K4-B deploy-COMPILE selector (command:bundle).
	OpCompile = ops.OpCompile

	// OpCollectContext + OpReverse are the DEPLOY-TIME builder-IR legs of an externalized detection-builder.
	OpCollectContext = ops.OpCollectContext // builder: per-candy stage-context keys → BuilderCollectReply
	OpReverse        = ops.OpReverse        // builder: teardown ops for a resolved stage context → BuilderReverseReply

	// F6 — the SUBSTRATE LIFECYCLE selectors (host→plugin on Provider.Invoke).
	OpPrepareVenue     = ops.OpPrepareVenue     // lifecycle: build the venue → VenueDescriptor (re-materialized host-side)
	OpArtifactKey      = ops.OpArtifactKey      // lifecycle: the per-deploy artifact ledger key
	OpPostApply        = ops.OpPostApply        // lifecycle: post-walk finalize on the venue
	OpTeardownExecutor = ops.OpTeardownExecutor // lifecycle: the executor for Del → VenueDescriptor
	OpPostTeardown     = ops.OpPostTeardown     // lifecycle: drop venue artifacts (image/domain)
	OpStart            = ops.OpStart            // lifecycle: start the venue
	OpStop             = ops.OpStop             // lifecycle: stop the venue
	OpStatus           = ops.OpStatus           // lifecycle: venue status → spec.DeployTargetStatus
	OpLogs             = ops.OpLogs             // lifecycle: stream venue logs
	OpShell            = ops.OpShell            // lifecycle: NON-interactive in-container exec CAPTURE; interactive shell is OpAttach
	OpAttach           = ops.OpAttach           // F12 lifecycle: LIVE-STDIO attach
	OpRebuild          = ops.OpRebuild          // lifecycle: rebuild the venue (charly update)

	// OpConfigWrite is the POD config-WRITE selector (P11, Q1=(a)).
	OpConfigWrite = ops.OpConfigWrite

	// OpConfigSetup / OpConfigRemove are the P13-KERNEL config-BODY selectors.
	OpConfigSetup  = ops.OpConfigSetup
	OpConfigRemove = ops.OpConfigRemove

	// OpStatusCollect: programmatic status collection → []spec.DeploymentStatus (distinct from lifecycle OpStatus).
	OpStatusCollect = ops.OpStatusCollect

	// OpStatusCollectAll is the K6 whole-subsystem status FAN-OUT + deploy-cone ENRICHMENT selector.
	OpStatusCollectAll = ops.OpStatusCollectAll

	// OpPreresolve is the generalized host-side deploy preresolver (F6).
	OpPreresolve = ops.OpPreresolve

	// OpBootstrap is the BOOTSTRAP-PHASE hook (F9).
	OpBootstrap = ops.OpBootstrap

	// OpEphemeralRegister / OpEphemeralTeardown are the command:bundle EPHEMERAL-LIFECYCLE selectors (FINAL/K5 unit 6a).
	OpEphemeralRegister = ops.OpEphemeralRegister
	OpEphemeralTeardown = ops.OpEphemeralTeardown

	// OpDeployDispatch is the command:bundle S3b selector (ELEVEN former methods through ONE wire pair, R3).
	OpDeployDispatch = ops.OpDeployDispatch

	// OpVerifyChecks is the command:check selector for the DEPLOY-VERIFY drive (#55 CHECK-ENGINE cone, Unit 2).
	OpVerifyChecks = ops.OpVerifyChecks

	// OpResolveEndpoint / OpResolveImageLabel / OpDrainEndpointCleanups are verb:check-resolve
	// selectors (#55 W3 B7) for the relocated CheckContext.ResolveEndpoint/ResolveImageLabel
	// resolution bodies + their shared cleanup-drain signal.
	OpResolveEndpoint       = ops.OpResolveEndpoint
	OpResolveImageLabel     = ops.OpResolveImageLabel
	OpDrainEndpointCleanups = ops.OpDrainEndpointCleanups

	// EphemeralPanicMarker prefixes an error converted from a RECOVERED PANIC inside OpEphemeralRegister/OpEphemeralTeardown (RCA #5).
	EphemeralPanicMarker = ops.EphemeralPanicMarker
)
