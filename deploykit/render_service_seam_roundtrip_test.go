package deploykit

// render_service_seam_roundtrip_test.go — R8 byte-proof harness for the render-service seam
// dissolution (#55 W3 B4). Unlike plan_compile_test.go's stubExecutorClient (canned replies,
// proving CompileServiceSteps' OWN request-marshal/reply-decode plumbing in isolation), THIS
// harness wires REAL compiled-in providers — candy/plugin-init's initkind.NewProvider() and
// candy/plugin-egress's egress.NewProvider() — so CompileServiceSteps drives the ACTUAL template
// render + the ACTUAL egress-schema validation, byte-identical to what the running charly binary
// produces. This is a deliberate, blessed exception to the "one candy per module" test-isolation
// norm (team-lead ruling, #55 W3 B4).
//
// R8 PROOF RECORD: this harness was written and run BEFORE the cutover, against the former
// ex.HostBuild("render-service", ...) mechanism renderServiceViaSeam used — same fixture, same
// real providers, same assertions as below. That run produced the EXACT UnitText/UnitPath this
// test now asserts. After the cutover (renderServiceViaSeam rewired to two direct InvokeProvider
// calls — kind:init OpResolve, then verb:egress OpValidate — reusing
// render_generator_from_project.go's renderSeamCaller.renderService), re-running this SAME test
// produced byte-identical output. The pre-change HostBuild-driving variant of this harness is not
// preserved (charly/host_build_render_service.go and spec.RenderServiceRequest/Reply, the wire
// types it decoded, are both deleted — nothing left to drive it against); this file is kept
// permanently in its post-change form as real regression coverage for a path that had none
// before (team-lead's "gate-1 teeth" framing) — every other CompileServiceSteps test in this
// package uses canned replies and would not catch a real template or schema regression.

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"google.golang.org/grpc"

	egress "github.com/opencharly/charly/candy/plugin-egress"
	initkind "github.com/opencharly/charly/candy/plugin-init"
	"github.com/opencharly/sdk"
	pb "github.com/opencharly/spec/proto"
	"github.com/opencharly/spec/spec"
)

// fixtureSystemdInit builds a realistic (not production-authored, but schema-faithful) systemd
// #InitServiceSchema — a Go text/template exercising the SAME funcs (systemdRestart, join) and
// context fields (Name/Exec/EnvList/Restart/After/WantedBy) a real systemd inits: block's
// service_template would, so renderInitTemplate's REAL execution path is exercised, not a stub.
func fixtureSystemdInit() *spec.ResolvedInit {
	schema := &spec.InitServiceSchema{
		ServiceTemplate: `[Unit]
Description={{.Name}} ({{.Candy}})
{{range .After}}After={{.}}
{{end}}
[Service]
ExecStart={{.Exec}}
Restart={{systemdRestart .Restart}}
{{range .EnvList}}Environment={{.Key}}={{.Value}}
{{end}}WorkingDirectory={{.WorkingDirectory}}

[Install]
WantedBy={{join .WantedBy " "}}
`,
		UnitPathTemplate: `/etc/systemd/system/charly-{{.Name}}.service`,
		SupportsPackaged: true,
	}
	initDef := spec.Init{Model: "systemd", ManagementTool: "systemd", ServiceSchema: schema}
	raw, err := json.Marshal(initDef)
	if err != nil {
		panic("fixtureSystemdInit: marshal: " + err.Error())
	}
	return &spec.ResolvedInit{
		Model:          "systemd",
		ManagementTool: "systemd",
		ServiceSchema:  schema,
		Raw:            raw,
	}
}

// realProviderExecutorClient routes InvokeProvider traffic to REAL, freshly-constructed
// candy/plugin-init and candy/plugin-egress providers (compiled-in placement's exact class:word
// pairing — "kind"/"init" and "verb"/"egress"), reproducing charly core's own
// providerRegistry.resolve+Invoke dispatch without linking charly core itself (import-purity: no
// production sdk/deploykit code may import a candy — this is a _test.go-only exception).
type realProviderExecutorClient struct {
	pb.ExecutorServiceClient
	initProvider   pb.ProviderServer
	egressProvider pb.ProviderServer
}

func newRealProviderExecutorClient() *realProviderExecutorClient {
	return &realProviderExecutorClient{
		initProvider:   initkind.NewProvider(),
		egressProvider: egress.NewProvider(),
	}
}

// InvokeProvider serves kind:init's OpResolve and verb:egress's OpValidate — the two direct
// peer-dispatch calls renderSeamCaller.renderService makes — each routed to a REAL provider
// instance, not a canned reply.
func (c *realProviderExecutorClient) InvokeProvider(ctx context.Context, in *pb.InvokeProviderRequest, _ ...grpc.CallOption) (*pb.InvokeReply, error) {
	var prov pb.ProviderServer
	switch {
	case in.GetClass() == "kind" && in.GetReserved() == "init":
		prov = c.initProvider
	case in.GetClass() == "verb" && in.GetReserved() == "egress":
		prov = c.egressProvider
	default:
		return nil, fmt.Errorf("realProviderExecutorClient: unexpected InvokeProvider(%s,%s)", in.GetClass(), in.GetReserved())
	}
	return prov.Invoke(ctx, &pb.InvokeRequest{
		Reserved: in.GetReserved(), Op: in.GetOp(), ParamsJson: in.GetParamsJson(), Class: in.GetClass(),
	})
}

// TestCompileServiceSteps_RealRenderRoundTrip is the R8 byte-proof: a custom (non-packaged)
// systemd service: entry, compiled with a REAL init-render + REAL egress-validate round trip (no
// canned replies) via renderServiceViaSeam -> renderSeamCaller.renderService -> two direct
// InvokeProvider calls. See the file header for the pre/post-cutover byte-compare record.
func TestCompileServiceSteps_RealRenderRoundTrip(t *testing.T) {
	client := newRealProviderExecutorClient()
	ctx, ex := context.Background(), sdk.NewInProcExecutor(client)

	layer := testCandy("myapp", spec.CandyModel{Service: []spec.ServiceEntry{
		{
			Name: "worker", Exec: "/usr/bin/myapp-worker --config /etc/myapp/config.yaml",
			Env:     map[string]string{"MYAPP_MODE": "production"},
			Restart: "always", Enable: true, Scope: "system",
		},
	}}, spec.CandyView{})
	img := testServiceImg("fedora:43", "fedora")

	steps, err := CompileServiceSteps(ctx, ex, layer, img, HostContext{
		MachineVenue:   true,
		ActiveInitName: "systemd",
		ActiveInit:     fixtureSystemdInit(),
	})
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

	const wantUnitPath = "/etc/systemd/system/charly-charly-myapp-worker.service"
	if sc.UnitPath != wantUnitPath {
		t.Errorf("UnitPath =\n%s\nwant\n%s", sc.UnitPath, wantUnitPath)
	}
	// R8 golden — recorded pre-cutover (ex.HostBuild) and confirmed byte-identical post-cutover
	// (ex.InvokeProvider): a divergence here is a real rendering regression, not test drift.
	const wantUnitText = `[Unit]
Description=charly-myapp-worker (myapp)

[Service]
ExecStart=/usr/bin/myapp-worker --config /etc/myapp/config.yaml
Restart=always
Environment=MYAPP_MODE=production
WorkingDirectory=

[Install]
WantedBy=
`
	if sc.UnitText != wantUnitText {
		t.Errorf("UnitText =\n%s\nwant\n%s", sc.UnitText, wantUnitText)
	}
}

// TestCompileServiceSteps_RealRenderRoundTrip_CheckLocalCoverageFixture mirrors the EXACT
// candy/check-local-layer service: entry shape (#55 W3 B4 coverage rung 2) — the fixture
// check-local-vm's live bed proof (team-lead-run) now exercises for real, over the guest's
// target:local deploy-compile. Same real-provider round trip as the test above, a different
// (simpler — no env, no explicit working_directory, restart:always) service shape, proving
// the render+egress-validate sequence handles it byte-for-byte the same way.
func TestCompileServiceSteps_RealRenderRoundTrip_CheckLocalCoverageFixture(t *testing.T) {
	client := newRealProviderExecutorClient()
	ctx, ex := context.Background(), sdk.NewInProcExecutor(client)

	layer := testCandy("check-local-layer", spec.CandyModel{Service: []spec.ServiceEntry{
		{
			Name:    "check-local-marker-daemon",
			Exec:    `/bin/sh -c "echo 'check-local-service v1' > /etc/check-local-service-marker; exec sleep infinity"`,
			Restart: "always", Enable: true, Scope: "system",
		},
	}}, spec.CandyView{})
	img := testServiceImg("arch")

	steps, err := CompileServiceSteps(ctx, ex, layer, img, HostContext{
		MachineVenue:   true,
		ActiveInitName: "systemd",
		ActiveInit:     fixtureSystemdInit(),
	})
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

	const wantUnitPath = "/etc/systemd/system/charly-charly-check-local-layer-check-local-marker-daemon.service"
	if sc.UnitPath != wantUnitPath {
		t.Errorf("UnitPath =\n%s\nwant\n%s", sc.UnitPath, wantUnitPath)
	}
	const wantUnitText = `[Unit]
Description=charly-check-local-layer-check-local-marker-daemon (check-local-layer)

[Service]
ExecStart=/bin/sh -c "echo 'check-local-service v1' > /etc/check-local-service-marker; exec sleep infinity"
Restart=always
WorkingDirectory=

[Install]
WantedBy=
`
	if sc.UnitText != wantUnitText {
		t.Errorf("UnitText =\n%s\nwant\n%s", sc.UnitText, wantUnitText)
	}
}
