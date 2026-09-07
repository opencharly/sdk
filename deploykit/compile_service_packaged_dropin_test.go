package deploykit

// compile_service_packaged_dropin_test.go — the packaged-unit override contract:
// a use_packaged service entry with an overrides: block MUST materialize a systemd
// drop-in (OverridesText/OverridesPath) on the compiled ServicePackagedStep — the
// executor (walkServicePackaged) writes it + daemon-reloads BEFORE enable --now, so
// the runtime ExecStart/env/ordering override applies without touching the shipped
// unit. Prior to the fix the compiler DROPPED entry.Overrides on the packaged path
// (the spec fields + the executor existed; the compiler never populated them), which
// left e.g. a --listen 0.0.0.0 override silently inert on a packaged unit.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/opencharly/sdk"
	"github.com/opencharly/spec/spec"
)

// fixtureSystemdInitWithDropin extends fixtureSystemdInit's schema with the packaged
// drop-in templates a real systemd InitServiceSchema carries (DropinTemplate /
// DropinPathTemplate), so the packaged branch of the REAL init provider renders a
// drop-in — schema-faithful, same rendering path as a production systemd init.
func fixtureSystemdInitWithDropin() *spec.ResolvedInit {
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
		DropinTemplate: `[Service]
ExecStart=
ExecStart={{.Exec}}
{{range .EnvList}}Environment={{.Key}}={{.Value}}
{{end}}`,
		DropinPathTemplate: `/etc/systemd/system/{{.PackagedUnit}}.d/charly-{{.Candy}}-{{.Name}}.conf`,
	}
	initDef := spec.Init{Model: "systemd", ManagementTool: "systemd", ServiceSchema: schema}
	raw, err := json.Marshal(initDef)
	if err != nil {
		panic("fixtureSystemdInitWithDropin: marshal: " + err.Error())
	}
	return &spec.ResolvedInit{
		Model:          "systemd",
		ManagementTool: "systemd",
		ServiceSchema:  schema,
		Raw:            raw,
	}
}

// TestCompileServiceSteps_PackagedOverridesMaterializeDropin proves the packaged
// path renders entry.Overrides into a systemd drop-in on the step (real init render
// + egress round trip, R8-style byte proof).
func TestCompileServiceSteps_PackagedOverridesMaterializeDropin(t *testing.T) {
	client := newRealProviderExecutorClient()
	ctx, ex := context.Background(), sdk.NewInProcExecutor(client)

	layer := testCandy("charly-mcp", spec.CandyModel{Service: []spec.ServiceEntry{
		{
			Name:        "mcp",
			UsePackaged: "charly-mcp.service",
			Enable:      true,
			Scope:       "system",
			Overrides: &spec.CandyServiceOverrides{
				Exec: "/usr/bin/charly mcp serve --listen 0.0.0.0:18765",
				Env:  map[string]string{"CHARLY_MCP_BIND": "all"},
			},
		},
	}}, spec.CandyView{})
	img := testServiceImg("cachyos", "arch")

	steps, err := CompileServiceSteps(ctx, ex, layer, img, HostContext{
		MachineVenue:   true,
		ActiveInitName: "systemd",
		ActiveInit:     fixtureSystemdInitWithDropin(),
	})
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
	if sp.Unit != "charly-mcp.service" {
		t.Errorf("Unit = %q, want charly-mcp.service", sp.Unit)
	}
	if sp.OverridesPath != "/etc/systemd/system/charly-mcp.service.d/charly-charly-mcp-mcp.conf" {
		t.Errorf("OverridesPath = %q, want the packaged drop-in dir", sp.OverridesPath)
	}
	if !strings.Contains(sp.OverridesText, "ExecStart=/usr/bin/charly mcp serve --listen 0.0.0.0:18765") {
		t.Errorf("OverridesText lacks the ExecStart override: %q", sp.OverridesText)
	}
	if !strings.Contains(sp.OverridesText, "ExecStart=\n") {
		t.Errorf("OverridesText lacks the ExecStart reset:\n%s", sp.OverridesText)
	}
	if !strings.Contains(sp.OverridesText, "Environment=CHARLY_MCP_BIND=all") {
		t.Errorf("OverridesText lacks the Environment override:\n%s", sp.OverridesText)
	}

	// A packaged entry WITHOUT overrides must carry no drop-in (the executor skips
	// the write; the shipped unit runs unmodified).
	layerPlain := testCandy("charly-mcp", spec.CandyModel{Service: []spec.ServiceEntry{
		{Name: "mcp", UsePackaged: "charly-mcp.service", Enable: true, Scope: "system"},
	}}, spec.CandyView{})
	steps, err = CompileServiceSteps(ctx, ex, layerPlain, img, HostContext{
		MachineVenue:   true,
		ActiveInitName: "systemd",
		ActiveInit:     fixtureSystemdInitWithDropin(),
	})
	if err != nil {
		t.Fatalf("CompileServiceSteps (plain): %v", err)
	}
	if p := steps[0].(*ServicePackagedStep); p.OverridesText != "" || p.OverridesPath != "" {
		t.Errorf("plain packaged step must carry no drop-in, got text=%q path=%q", p.OverridesText, p.OverridesPath)
	}
}
