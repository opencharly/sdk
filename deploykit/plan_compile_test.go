package deploykit

// plan_compile_test.go — direct unit coverage for CompileOpSteps / CompileServiceSteps (the
// InstallPlan compiler's two sub-compilers), using literal spec fixtures — no charly loader,
// mirroring sdk/buildkit/build_helpers_test.go's hand-built-fixture precedent (#55 K3 cone 1).
//
// BuildDeployPlan-level INTEGRATION coverage (real candy/ fixtures for ripgrep/dev-tools/
// pre-commit, ComputeDeployID, MergePlan, DescribePlan, the builder-purity/no-plugin-RPC gate,
// the SystemPackagesStep repo-key regression guard) already lives in
// candy/plugin-fleet/install_build_test.go (#55 decoupling Batch A, 42d97495) — this file does
// NOT duplicate that. What it covers instead is genuinely uncovered ground (repo-wide grep
// confirmed before writing this file): CompileOpSteps was previously exercised ONLY
// transitively through BuildDeployPlan, or via charly's own verb-routing-specific tests
// (install_act_test.go / plan_unify_test.go, which assert compileActOp's typed-step routing for
// ONE verb — not the compiler's own iteration/filtering/dispatch mechanics); CompileServiceSteps
// had exactly one direct caller anywhere (candy/plugin-fleet/service_distro_filter_test.go),
// narrowly scoped to distro filtering alone. Confirming this required wiring spec.OpInContext
// (a package-level DI hook CompileOpSteps calls — production wires it from charly/layers.go's
// init(), which this sdk-only test binary never links) ourselves below; no other sdk/deploykit
// test file did so before this one, which is independent proof CompileOpSteps was never
// exercised from this package (a nil-func-call panic would have surfaced immediately).

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"google.golang.org/grpc"

	"github.com/opencharly/sdk"
	pb "github.com/opencharly/spec/proto"
	"github.com/opencharly/spec/spec"
)

// spec.OpInContext is a package-level DI hook CompileOpSteps calls (spec/spec/injection_seams.go):
// in production charly core's own init() wires it (charly/layers.go: spec.OpInContext =
// opInContext), safe there because charly always shares that process with the compile call. This
// package's own test binary links no charly core, so the hook stays nil unless wired here. The
// implementation is pure (spec.VerbCatalog is static data + the op's own declared Context — no
// registry consult), so it is ported verbatim from charly/planrun_adapter.go's opInContext/
// opEffectiveContexts (the SAME port candy/plugin-fleet's fleet_test_helpers_test.go already
// carries for its own out-of-module test binary — R3 would collapse these into one shared sdk
// helper if a THIRD package ever needed it; two independent test-binary ports of a ~15-line pure
// function is not yet worth a shared-package indirection).
func init() {
	spec.OpInContext = compileTestOpInContext
}

func compileTestOpEffectiveContexts(c *spec.Op) []spec.ExecContext {
	if len(c.Context) > 0 {
		out := make([]spec.ExecContext, 0, len(c.Context))
		for _, s := range c.Context {
			out = append(out, spec.ExecContext(s))
		}
		return out
	}
	if verb, err := c.Kind(); err == nil {
		if vs, ok := spec.VerbCatalog[verb]; ok {
			return vs.Contexts
		}
	}
	return nil
}

func compileTestOpInContext(c *spec.Op, ctx spec.ExecContext) bool {
	return slices.Contains(compileTestOpEffectiveContexts(c), ctx)
}

// testCandy (the literal-fixture spec.CandyReader constructor) and testResolvedBox (a hand-built
// fedora/rpm ResolvedBox) are already package-level test helpers — graph_shim_relocated_test.go
// and tasks_emit_test.go respectively — reused here as-is (R3: no second copy).

// stubExecutorClient is a minimal, per-test-configurable pb.ExecutorServiceClient double. Every
// method other than HostBuild/InvokeProvider is left on the nil embedded interface, so calling
// one panics loudly — none of the tests below drive Venue/RunSystem/RunUser/etc., only the
// host-reaching seams each compiler function has (construct-step / render-service). This proves
// the compiler's OWN request-marshal / reply-decode plumbing in isolation from whatever the real
// provider registry would decide (that decision is charly-core territory, covered by charly's own
// install_act_test.go against the REAL construct-step host builder, and — for render-service
// specifically, #55 W3 B4 — render_service_seam_roundtrip_test.go's REAL-provider round trip).
type stubExecutorClient struct {
	pb.ExecutorServiceClient
	hostBuild      func(*pb.HostBuildRequest) (*pb.HostBuildReply, error)
	invokeProvider func(*pb.InvokeProviderRequest) (*pb.InvokeReply, error)
}

func (s stubExecutorClient) HostBuild(_ context.Context, in *pb.HostBuildRequest, _ ...grpc.CallOption) (*pb.HostBuildReply, error) {
	if s.hostBuild == nil {
		return nil, fmt.Errorf("stubExecutorClient: unexpected HostBuild(%q) — no hostBuild func configured", in.GetKind())
	}
	return s.hostBuild(in)
}

func (s stubExecutorClient) InvokeProvider(_ context.Context, in *pb.InvokeProviderRequest, _ ...grpc.CallOption) (*pb.InvokeReply, error) {
	if s.invokeProvider == nil {
		return nil, fmt.Errorf("stubExecutorClient: unexpected InvokeProvider(%s,%s) — no invokeProvider func configured", in.GetClass(), in.GetReserved())
	}
	return s.invokeProvider(in)
}

// testExecutor returns a background context + an *sdk.Executor backed by stubExecutorClient. A
// nil hostBuild means "this test expects the compiler to NEVER dial HostBuild" (proving purity on
// the non-plugin-verb / non-render path) — the stub errors loudly if it's ever called.
func testExecutor(hostBuild func(*pb.HostBuildRequest) (*pb.HostBuildReply, error)) (context.Context, *sdk.Executor) {
	return context.Background(), sdk.NewInProcExecutor(stubExecutorClient{hostBuild: hostBuild})
}

// testExecutorInvoke returns a background context + an *sdk.Executor backed by stubExecutorClient,
// wired for InvokeProvider instead of HostBuild (#55 W3 B4 — the render-service seam moved off
// HostBuild onto direct peer dispatch).
func testExecutorInvoke(invokeProvider func(*pb.InvokeProviderRequest) (*pb.InvokeReply, error)) (context.Context, *sdk.Executor) {
	return context.Background(), sdk.NewInProcExecutor(stubExecutorClient{invokeProvider: invokeProvider})
}

// ---------------------------------------------------------------------------
// CompileOpSteps
// ---------------------------------------------------------------------------

// TestCompileOpSteps_FiltersToRunStepsOnly proves the loop skips every non-`run:` step (check /
// agent-run / agent-check / include) — only KwRun steps ever reach constructOpStep. A single
// run: step in the middle of a plan carrying one of each other keyword must yield EXACTLY that
// one step, in a plan slot ambiguous enough (3 non-run neighbors) that an off-by-one filter bug
// would surface as a wrong step count.
func TestCompileOpSteps_FiltersToRunStepsOnly(t *testing.T) {
	layer := testCandy("x", spec.CandyModel{Plan: []spec.Step{
		{Check: "probe", Op: spec.Op{Command: "true"}},
		{AgentCheck: "looks right"},
		{Include: "other:kind"},
		{Run: "make the dir", Op: spec.Op{Mkdir: "/opt/x"}},
	}}, spec.CandyView{})

	ctx, ex := testExecutor(nil) // must never dial HostBuild — mkdir is a non-plugin verb
	steps, err := CompileOpSteps(ctx, ex, layer, testResolvedBox())
	if err != nil {
		t.Fatalf("CompileOpSteps: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("len(steps) = %d, want 1 (only the run: step should lower); got %#v", len(steps), steps)
	}
	op, ok := steps[0].(*OpStep)
	if !ok {
		t.Fatalf("steps[0] = %#v, want *OpStep", steps[0])
	}
	if op.Op.Mkdir != "/opt/x" {
		t.Errorf("op.Op.Mkdir = %q, want /opt/x", op.Op.Mkdir)
	}
}

// TestCompileOpSteps_SkipsRuntimeOnlyOps proves a run: step scoped EXCLUSIVELY to runtime
// context (context: [runtime], no build/deploy) is skipped — it belongs to the check Runner's
// live execution, never the install timeline; lowering it would double-execute it (once at
// install, once live). A sibling run: step with an EXPLICIT context: [build, deploy] (no
// runtime) in the SAME plan must still lower, proving the skip is context-scoped, not a
// blanket effect of one op appearing.
func TestCompileOpSteps_SkipsRuntimeOnlyOps(t *testing.T) {
	layer := testCandy("x", spec.CandyModel{Plan: []spec.Step{
		{Run: "runtime probe", Op: spec.Op{Mkdir: "/should/not/appear", Context: []string{"runtime"}}},
		{Run: "install dir", Op: spec.Op{Mkdir: "/should/appear", Context: []string{"build", "deploy"}}},
	}}, spec.CandyView{})

	ctx, ex := testExecutor(nil)
	steps, err := CompileOpSteps(ctx, ex, layer, testResolvedBox())
	if err != nil {
		t.Fatalf("CompileOpSteps: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("len(steps) = %d, want 1 (the runtime-only op must be skipped); got %#v", len(steps), steps)
	}
	op, ok := steps[0].(*OpStep)
	if !ok || op.Op.Mkdir != "/should/appear" {
		t.Fatalf("steps[0] = %#v, want the build/deploy mkdir op", steps[0])
	}
}

// TestCompileOpSteps_NonPluginVerbNeverDialsHostBuild is the CompileOpSteps-level externalization
// purity gate (the constructOpStep-scoped sibling of
// candy/plugin-fleet/install_build_test.go's TestBuildDeployPlan_BuilderPurity_NoPluginRPC,
// which proves BUILDER-step purity — this proves the SAME invariant one layer down, for the
// install-verb dispatch that decides whether ANY op needs the wire at all): every install verb
// (mkdir/copy/write/link/download/setcap/build/command — anything whose Kind() != "plugin") must
// lower via the fully-portable buildGenericOpStep with ZERO HostBuild calls. testExecutor(nil)
// errors loudly on any HostBuild — so a passing run proves purity, not merely a plausible result.
func TestCompileOpSteps_NonPluginVerbNeverDialsHostBuild(t *testing.T) {
	layer := testCandy("x", spec.CandyModel{Plan: []spec.Step{
		{Run: "mkdir", Op: spec.Op{Mkdir: "/a"}},
		{Run: "download", Op: spec.Op{Download: "https://example/x", To: "/b"}},
		{Run: "setcap", Op: spec.Op{Setcap: "/usr/bin/x"}},
	}}, spec.CandyView{})

	ctx, ex := testExecutor(nil)
	steps, err := CompileOpSteps(ctx, ex, layer, testResolvedBox())
	if err != nil {
		t.Fatalf("CompileOpSteps: %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("len(steps) = %d, want 3; got %#v", len(steps), steps)
	}
	for i, s := range steps {
		if _, ok := s.(*OpStep); !ok {
			t.Errorf("steps[%d] = %#v, want *OpStep (generic lowering)", i, s)
		}
	}
}

// TestCompileOpSteps_PluginVerbFallsBackOnEmptyReply proves the "construct-step" HostBuild round
// trip: a `plugin:` verb op reaches HostBuild(construct-step, ...) exactly once, and an empty
// reply (Step == nil — "not a typed step, build the generic OpStep") falls back to the SAME
// generic OpStep lowering non-plugin verbs get, carrying the plugin/plugin_input pair intact.
func TestCompileOpSteps_PluginVerbFallsBackOnEmptyReply(t *testing.T) {
	var calls int
	layer := testCandy("x", spec.CandyModel{Plan: []spec.Step{
		{Run: "run a script", Op: spec.Op{Plugin: "command", PluginInput: map[string]any{"command": "echo hi"}}},
	}}, spec.CandyView{})

	ctx, ex := testExecutor(func(in *pb.HostBuildRequest) (*pb.HostBuildReply, error) {
		if in.GetKind() != "construct-step" {
			return nil, fmt.Errorf("unexpected HostBuild kind %q", in.GetKind())
		}
		calls++
		reply, err := json.Marshal(spec.ConstructStepReply{})
		if err != nil {
			return nil, err
		}
		return &pb.HostBuildReply{ResultJson: reply}, nil
	})
	steps, err := CompileOpSteps(ctx, ex, layer, testResolvedBox())
	if err != nil {
		t.Fatalf("CompileOpSteps: %v", err)
	}
	if calls != 1 {
		t.Fatalf("HostBuild(construct-step) called %d times, want exactly 1", calls)
	}
	if len(steps) != 1 {
		t.Fatalf("len(steps) = %d, want 1", len(steps))
	}
	op, ok := steps[0].(*OpStep)
	if !ok {
		t.Fatalf("steps[0] = %#v, want *OpStep (empty reply must fall back to generic lowering)", steps[0])
	}
	if op.Op.Plugin != "command" || op.Op.PluginInput["command"] != "echo hi" {
		t.Errorf("plugin/plugin_input not preserved on generic fallback: %+v", op.Op)
	}
}

// TestCompileOpSteps_PluginVerbLowersToTypedStepFromReply proves the OTHER half of the
// construct-step contract: a non-empty reply carrying a typed step VIEW (here a
// SystemPackagesStep, mirroring the real "plugin: package" TypedStepProvider path
// charly/install_act_test.go exercises against the real host) is decoded via StepFromView and
// returned VERBATIM — CompileOpSteps must never re-derive or discard fields from the reply.
func TestCompileOpSteps_PluginVerbLowersToTypedStepFromReply(t *testing.T) {
	layer := testCandy("x", spec.CandyModel{Plan: []spec.Step{
		{Run: "install redis", Op: spec.Op{Plugin: "package", PluginInput: map[string]any{"package": "redis"}}},
	}}, spec.CandyView{})

	want := &spec.SystemPackagesStep{Format: "rpm", Phase: spec.PhaseInstall, Packages: []string{"redis"}}
	view := spec.StepToView(want)

	ctx, ex := testExecutor(func(in *pb.HostBuildRequest) (*pb.HostBuildReply, error) {
		if in.GetKind() != "construct-step" {
			return nil, fmt.Errorf("unexpected HostBuild kind %q", in.GetKind())
		}
		reply, err := json.Marshal(spec.ConstructStepReply{Step: &view})
		if err != nil {
			return nil, err
		}
		return &pb.HostBuildReply{ResultJson: reply}, nil
	})
	steps, err := CompileOpSteps(ctx, ex, layer, testResolvedBox())
	if err != nil {
		t.Fatalf("CompileOpSteps: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("len(steps) = %d, want 1", len(steps))
	}
	sp, ok := steps[0].(*spec.SystemPackagesStep)
	if !ok {
		t.Fatalf("steps[0] = %#v, want *spec.SystemPackagesStep (the typed reply must NOT fall back to a generic OpStep)", steps[0])
	}
	if sp.Format != "rpm" || len(sp.Packages) != 1 || sp.Packages[0] != "redis" {
		t.Errorf("SystemPackagesStep = %+v, want Format=rpm Packages=[redis]", sp)
	}
	rev := sp.Reverse()
	if len(rev) != 1 || rev[0].Kind != spec.ReverseOpPackageRemove {
		t.Errorf("Reverse() = %+v, want [package-remove] (the load-bearing reversal must survive the wire round-trip)", rev)
	}
}

// TestCompileOpSteps_HostBuildErrorPropagates proves a construct-step failure surfaces as a
// CompileOpSteps error rather than being swallowed or silently treated as "no typed step."
func TestCompileOpSteps_HostBuildErrorPropagates(t *testing.T) {
	layer := testCandy("x", spec.CandyModel{Plan: []spec.Step{
		{Run: "run a script", Op: spec.Op{Plugin: "command", PluginInput: map[string]any{"command": "echo hi"}}},
	}}, spec.CandyView{})

	ctx, ex := testExecutor(func(*pb.HostBuildRequest) (*pb.HostBuildReply, error) {
		return nil, fmt.Errorf("boom: host reverse channel unavailable")
	})
	_, err := CompileOpSteps(ctx, ex, layer, testResolvedBox())
	if err == nil {
		t.Fatal("CompileOpSteps: expected an error when HostBuild(construct-step) fails, got nil")
	}
}

// ---------------------------------------------------------------------------
// CompileServiceSteps
// ---------------------------------------------------------------------------

func testServiceImg(distro ...string) *ResolvedBox {
	return &ResolvedBox{ResolvedBox: spec.ResolvedBox{Name: "svc", Home: "/home/u", Distro: distro}}
}

// TestCompileServiceSteps_PackagedOnMachineVenue proves a use_packaged: entry lowers to a
// ServicePackagedStep, with Unit/.service-suffixed, TargetScope, and Enable carried through, ONLY
// on a MachineVenue compile (a host/vm deploy with a real systemd init) — the container-image
// compile case is covered by the sibling test below.
func TestCompileServiceSteps_PackagedOnMachineVenue(t *testing.T) {
	layer := testCandy("x", spec.CandyModel{Service: []spec.ServiceEntry{
		{Name: "redis", UsePackaged: "redis", Scope: "system", Enable: true},
	}}, spec.CandyView{})
	img := testServiceImg("fedora:43", "fedora")

	ctx, ex := testExecutor(nil) // packaged units never touch render-service
	steps, err := CompileServiceSteps(ctx, ex, layer, img, HostContext{MachineVenue: true})
	if err != nil {
		t.Fatalf("CompileServiceSteps: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("len(steps) = %d, want 1; got %#v", len(steps), steps)
	}
	sp, ok := steps[0].(*ServicePackagedStep)
	if !ok {
		t.Fatalf("steps[0] = %#v, want *ServicePackagedStep", steps[0])
	}
	if sp.Unit != "redis.service" {
		t.Errorf("Unit = %q, want redis.service (EnsureServiceSuffix must apply)", sp.Unit)
	}
	if sp.TargetScope != spec.ScopeSystem || !sp.Enable {
		t.Errorf("TargetScope/Enable = %v/%v, want system/true", sp.TargetScope, sp.Enable)
	}
}

// TestCompileServiceSteps_PackagedSkippedOnContainerCompile proves the same packaged: entry
// yields NO step when compiling for a container image build (HostContext{} zero value,
// MachineVenue=false) — a supervisord-driven pod image cannot consume a systemd packaged unit
// (compileServiceSteps's own documented invariant).
func TestCompileServiceSteps_PackagedSkippedOnContainerCompile(t *testing.T) {
	layer := testCandy("x", spec.CandyModel{Service: []spec.ServiceEntry{
		{Name: "redis", UsePackaged: "redis", Enable: true},
	}}, spec.CandyView{})
	img := testServiceImg("fedora:43", "fedora")

	ctx, ex := testExecutor(nil)
	steps, err := CompileServiceSteps(ctx, ex, layer, img, HostContext{})
	if err != nil {
		t.Fatalf("CompileServiceSteps: %v", err)
	}
	if len(steps) != 0 {
		t.Fatalf("len(steps) = %d, want 0 (packaged units must not render on a container compile); got %#v", len(steps), steps)
	}
}

// TestCompileServiceSteps_CustomWithoutPackagedSibling proves a plain exec: entry (no matching
// use_packaged: sibling) lowers to a ServiceCustomStep named "charly-<candy>-<entry>", with
// TargetScope/Enable carried through — and, absent a resolved systemd ActiveInit, UnitText stays
// empty rather than dialing render-service speculatively.
func TestCompileServiceSteps_CustomWithoutPackagedSibling(t *testing.T) {
	layer := testCandy("myapp", spec.CandyModel{Service: []spec.ServiceEntry{
		{Name: "worker", Exec: "/usr/bin/myapp-worker", Scope: "user", Enable: true},
	}}, spec.CandyView{})
	img := testServiceImg("fedora:43", "fedora")

	ctx, ex := testExecutor(nil) // no ActiveInit resolved → renderServiceViaSeam must not be dialed
	steps, err := CompileServiceSteps(ctx, ex, layer, img, HostContext{MachineVenue: true})
	if err != nil {
		t.Fatalf("CompileServiceSteps: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("len(steps) = %d, want 1; got %#v", len(steps), steps)
	}
	sc, ok := steps[0].(*ServiceCustomStep)
	if !ok {
		t.Fatalf("steps[0] = %#v, want *ServiceCustomStep", steps[0])
	}
	if sc.Name != "charly-myapp-worker" {
		t.Errorf("Name = %q, want charly-myapp-worker", sc.Name)
	}
	if sc.TargetScope != spec.ScopeUser || !sc.Enable {
		t.Errorf("TargetScope/Enable = %v/%v, want user/true", sc.TargetScope, sc.Enable)
	}
	if sc.UnitText != "" {
		t.Errorf("UnitText = %q, want empty (no ActiveInit resolved)", sc.UnitText)
	}
}

// TestCompileServiceSteps_MixedPairPackagedWinsOnSystemd proves the mixed-entry polymorphism
// (CLAUDE.md "Init-system polymorphism"): when the SAME name carries both a use_packaged: form
// and an exec: form, the packaged form wins on a systemd machine venue — the custom entry is
// skipped entirely, never emitted alongside it.
func TestCompileServiceSteps_MixedPairPackagedWinsOnSystemd(t *testing.T) {
	layer := testCandy("x", spec.CandyModel{Service: []spec.ServiceEntry{
		{Name: "libvirtd", UsePackaged: "libvirtd", Enable: true},
		{Name: "libvirtd", Exec: "/usr/sbin/libvirtd", Enable: true},
	}}, spec.CandyView{})
	img := testServiceImg("fedora:43", "fedora")

	ctx, ex := testExecutor(nil)
	steps, err := CompileServiceSteps(ctx, ex, layer, img, HostContext{MachineVenue: true})
	if err != nil {
		t.Fatalf("CompileServiceSteps: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("len(steps) = %d, want 1 (packaged must win, custom must be suppressed); got %#v", len(steps), steps)
	}
	if _, ok := steps[0].(*ServicePackagedStep); !ok {
		t.Fatalf("steps[0] = %#v, want *ServicePackagedStep (the packaged sibling)", steps[0])
	}
}

// TestCompileServiceSteps_PerDistroFilter proves ServiceEntryAppliesToDistro gates rendering: an
// entry restricted to "debian" is skipped when the image's distro chain is fedora-only, and
// renders when the chain matches (either the bare name or a versioned tag).
func TestCompileServiceSteps_PerDistroFilter(t *testing.T) {
	layer := testCandy("x", spec.CandyModel{Service: []spec.ServiceEntry{
		{Name: "apt-only", UsePackaged: "apt-only", Distro: []string{"debian"}},
	}}, spec.CandyView{})

	ctx, ex := testExecutor(nil)

	fedoraImg := testServiceImg("fedora:43", "fedora")
	steps, err := CompileServiceSteps(ctx, ex, layer, fedoraImg, HostContext{MachineVenue: true})
	if err != nil {
		t.Fatalf("CompileServiceSteps (fedora): %v", err)
	}
	if len(steps) != 0 {
		t.Fatalf("fedora: len(steps) = %d, want 0 (debian-only entry must not apply)", len(steps))
	}

	debianImg := testServiceImg("debian:13", "debian")
	steps, err = CompileServiceSteps(ctx, ex, layer, debianImg, HostContext{MachineVenue: true})
	if err != nil {
		t.Fatalf("CompileServiceSteps (debian): %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("debian: len(steps) = %d, want 1 (debian-only entry must apply on a debian chain)", len(steps))
	}
}

// ---------------------------------------------------------------------------
// CompileExtractSteps
// ---------------------------------------------------------------------------

// TestCompileExtractSteps_MachineVenueEmitsOneStepPerEntry proves a candy's `extract:` entries
// lower to one ExtractStep each on a MACHINE venue compile (target:local / target:vm — where the
// built image filesystem is never used and the content must be materialized onto the venue's own
// filesystem), carrying Source/Path/Dest/CandyName verbatim. This is the step that FAILS without
// the change: before CompileExtractSteps existed, a machine-venue deploy silently dropped every
// `extract:` entry (the Containerfile COPY --from stages only ever ran at image build).
func TestCompileExtractSteps_MachineVenueEmitsOneStepPerEntry(t *testing.T) {
	layer := testCandy("agentteams-higress", spec.CandyModel{Extract: []spec.CandyExtract{
		{Source: "higress-registry.cn-hangzhou.cr.aliyuncs.com/higress/all-in-one:2.2.1", Path: "/usr/local/bin/envoy", Dest: "/usr/local/bin/envoy"},
		{Source: "higress-registry.cn-hangzhou.cr.aliyuncs.com/higress/all-in-one:2.2.1", Path: "/etc/istio", Dest: "/etc/istio"},
	}}, spec.CandyView{})

	steps := CompileExtractSteps(layer, HostContext{MachineVenue: true})
	if len(steps) != 2 {
		t.Fatalf("len(steps) = %d, want 2 (one ExtractStep per extract: entry); got %#v", len(steps), steps)
	}
	for i, want := range []struct{ src, path, dest string }{
		{"higress-registry.cn-hangzhou.cr.aliyuncs.com/higress/all-in-one:2.2.1", "/usr/local/bin/envoy", "/usr/local/bin/envoy"},
		{"higress-registry.cn-hangzhou.cr.aliyuncs.com/higress/all-in-one:2.2.1", "/etc/istio", "/etc/istio"},
	} {
		es, ok := steps[i].(*ExtractStep)
		if !ok {
			t.Fatalf("steps[%d] = %#v, want *ExtractStep", i, steps[i])
		}
		if es.Source != want.src || es.Path != want.path || es.Dest != want.dest {
			t.Errorf("steps[%d] = %+v, want Source=%q Path=%q Dest=%q", i, es, want.src, want.path, want.dest)
		}
		if es.CandyName != "agentteams-higress" {
			t.Errorf("steps[%d].CandyName = %q, want agentteams-higress (provenance for the ledger)", i, es.CandyName)
		}
	}
}

// TestCompileExtractSteps_ContainerCompileEmitsNothing proves the SAME candy yields NO extract
// steps on a container-image compile (HostContext{} zero value, MachineVenue=false) — the OCI/pod
// targets emit the Containerfile extract stages directly (EmitExtractStages / EmitExtractedFiles)
// and must never see a machine-venue ExtractStep in their install plan.
func TestCompileExtractSteps_ContainerCompileEmitsNothing(t *testing.T) {
	layer := testCandy("agentteams-higress", spec.CandyModel{Extract: []spec.CandyExtract{
		{Source: "higress-registry.cn-hangzhou.cr.aliyuncs.com/higress/all-in-one:2.2.1", Path: "/usr/local/bin/envoy", Dest: "/usr/local/bin/envoy"},
	}}, spec.CandyView{})

	if steps := CompileExtractSteps(layer, HostContext{}); len(steps) != 0 {
		t.Fatalf("len(steps) = %d, want 0 (container compile must not lower extract steps); got %#v", len(steps), steps)
	}
}

// TestCompileExtractSteps_NoEntriesEmitsNothing proves the no-op path: a candy with no `extract:`
// entries yields zero steps even on a machine venue (the common case — most candies never extract).
func TestCompileExtractSteps_NoEntriesEmitsNothing(t *testing.T) {
	layer := testCandy("plain", spec.CandyModel{}, spec.CandyView{})
	if steps := CompileExtractSteps(layer, HostContext{MachineVenue: true}); len(steps) != 0 {
		t.Fatalf("len(steps) = %d, want 0 (no extract: entries → no steps); got %#v", len(steps), steps)
	}
}

// TestCompileServiceSteps_RendersCustomUnitViaSeamOnSystemd proves the render-service peer-dispatch
// round trip (#55 W3 B4 — moved off HostBuild onto direct InvokeProvider): a custom exec: entry on
// a MachineVenue compile with a resolved systemd ActiveInit InvokeProviders kind:init's OpResolve
// exactly once, then verb:egress's OpValidate exactly once on the rendered unit text, and carries
// the returned UnitText/UnitPath into the ServiceCustomStep verbatim. The REAL provider round trip
// (no canned replies) is covered separately by render_service_seam_roundtrip_test.go.
func TestCompileServiceSteps_RendersCustomUnitViaSeamOnSystemd(t *testing.T) {
	var initCalls, egressCalls int
	layer := testCandy("myapp", spec.CandyModel{Service: []spec.ServiceEntry{
		{Name: "worker", Exec: "/usr/bin/myapp-worker", Enable: true},
	}}, spec.CandyView{})
	img := testServiceImg("fedora:43", "fedora")

	ctx, ex := testExecutorInvoke(func(in *pb.InvokeProviderRequest) (*pb.InvokeReply, error) {
		switch {
		case in.GetClass() == "kind" && in.GetReserved() == "init":
			initCalls++
			reply, err := json.Marshal(spec.ServiceRenderReply{Rendered: &spec.RenderedService{
				UnitText: "[Unit]\nDescription=worker\n",
				UnitPath: "/etc/systemd/system/charly-myapp-worker.service",
			}})
			if err != nil {
				return nil, err
			}
			return &pb.InvokeReply{ResultJson: reply}, nil
		case in.GetClass() == "verb" && in.GetReserved() == "egress":
			egressCalls++
			reply, err := json.Marshal(map[string]string{"error": ""})
			if err != nil {
				return nil, err
			}
			return &pb.InvokeReply{ResultJson: reply}, nil
		default:
			return nil, fmt.Errorf("unexpected InvokeProvider(%s,%s)", in.GetClass(), in.GetReserved())
		}
	})
	steps, err := CompileServiceSteps(ctx, ex, layer, img, HostContext{
		MachineVenue:   true,
		ActiveInitName: "systemd",
		ActiveInit:     &spec.ResolvedInit{Model: "systemd", ServiceSchema: &spec.InitServiceSchema{ServiceTemplate: "x"}},
	})
	if err != nil {
		t.Fatalf("CompileServiceSteps: %v", err)
	}
	if initCalls != 1 {
		t.Fatalf("InvokeProvider(kind,init) called %d times, want exactly 1", initCalls)
	}
	if egressCalls != 1 {
		t.Fatalf("InvokeProvider(verb,egress) called %d times, want exactly 1", egressCalls)
	}
	if len(steps) != 1 {
		t.Fatalf("len(steps) = %d, want 1", len(steps))
	}
	sc, ok := steps[0].(*ServiceCustomStep)
	if !ok {
		t.Fatalf("steps[0] = %#v, want *ServiceCustomStep", steps[0])
	}
	if sc.UnitText != "[Unit]\nDescription=worker\n" {
		t.Errorf("UnitText = %q, not carried through from the render-service reply", sc.UnitText)
	}
	if sc.UnitPath != "/etc/systemd/system/charly-myapp-worker.service" {
		t.Errorf("UnitPath = %q, not carried through from the render-service reply", sc.UnitPath)
	}
}
