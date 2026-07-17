package deploykit

import (
	"os"
	"strings"
	"testing"

	"github.com/opencharly/sdk/spec"
)

// TestGenerateInitFragments / TestGenerateRelayInitFragments were relocated
// here from charly/generate_test.go (K3, Bucket-1 dissolution): the thin
// charly-core wrapper (Generator.generateInitFragments) that these tests
// exercised had zero non-test callers and was deleted, but the real logic
// they cover — GenerateInitFragments' fragment-assembly + port-relay naming —
// lives in this package and stays real production code (candy/plugin-deploy-pod's
// overlay build reaches it directly). RenderService is stubbed with a minimal
// fragment renderer so the test isolates GenerateInitFragments' own orchestration
// (candy ordering, per-candy fragment files, port-relay fragments) from
// candy/plugin-init's template engine, which charly's own service_render_test.go
// already covers in depth.

func fakeRenderService(entry *spec.ServiceEntry, def *spec.ResolvedInit, ctx spec.ServiceRenderContext) (*spec.RenderedService, error) {
	return &spec.RenderedService{UnitText: "[program:" + ctx.Name + "]\ncommand=" + ctx.Exec + "\n"}, nil
}

func TestGenerateInitFragments(t *testing.T) {
	tmpDir := t.TempDir()

	// Schema-driven: each candy's service: list contains structured entries.
	// GenerateInitFragments iterates them and calls RenderService per entry.
	g := &Generator{
		BuildDir: tmpDir,
		Candies: map[string]CandyModel{
			"python": NewSpecCandyModel(spec.CandyModel{Name: "python"}, spec.CandyView{}),
			"svc": NewSpecCandyModel(spec.CandyModel{
				Name:    "svc",
				Service: []spec.ServiceEntry{{Name: "svc", Exec: "svc serve"}},
			}, spec.CandyView{}),
			"other": NewSpecCandyModel(spec.CandyModel{
				Name:    "other",
				Service: []spec.ServiceEntry{{Name: "other", Exec: "other run"}},
			}, spec.CandyView{}),
		},
		RenderService: fakeRenderService,
	}

	supervisordDef := &spec.ResolvedInit{
		Model:       "fragment_assembly",
		FragmentDir: "supervisor",
		ServiceSchema: &spec.InitServiceSchema{
			SupportsPackaged: false,
			ServiceTemplate:  "[program:{{.Name}}]\ncommand={{.Exec}}\n",
		},
	}

	err := g.GenerateInitFragments("test-image", "supervisord", supervisordDef, []string{"python", "svc", "other"})
	if err != nil {
		t.Fatalf("GenerateInitFragments() error = %v", err)
	}

	// Candy ordering: python=1, svc=2, other=3. Each candy with service entries
	// gets ONE fragment file named <NN>-<candy>.conf containing all its entries.
	data, err := os.ReadFile(tmpDir + "/test-image/supervisor/02-svc.conf")
	if err != nil {
		t.Fatalf("reading svc supervisor fragment: %v", err)
	}
	if !strings.Contains(string(data), "[program:svc]") {
		t.Errorf("svc fragment missing [program:svc]; got: %q", string(data))
	}
	if !strings.Contains(string(data), "command=svc serve") {
		t.Errorf("svc fragment missing exec command; got: %q", string(data))
	}

	data, err = os.ReadFile(tmpDir + "/test-image/supervisor/03-other.conf")
	if err != nil {
		t.Fatalf("reading other supervisor fragment: %v", err)
	}
	if !strings.Contains(string(data), "[program:other]") {
		t.Errorf("other fragment missing [program:other]; got: %q", string(data))
	}

	// python has no service: entry → no fragment file.
	if _, err := os.Stat(tmpDir + "/test-image/supervisor/01-python.conf"); err == nil {
		t.Error("python should not produce a fragment")
	}
}

func TestGenerateRelayInitFragments(t *testing.T) {
	tmpDir := t.TempDir()

	relayTmpl := "[program:relay-{{.Port}}]\ncommand=/usr/local/bin/relay-wrapper {{.Port}}\nautostart=true\nautorestart=true\npriority=1\nstartsecs=0\nstdout_logfile=/dev/fd/1\nstdout_logfile_maxbytes=0\nredirect_stderr=true\n"

	g := &Generator{
		BuildDir: tmpDir,
		Candies: map[string]CandyModel{
			"socat": NewSpecCandyModel(spec.CandyModel{Name: "socat"}, spec.CandyView{}),
			"chrome": NewSpecCandyModel(spec.CandyModel{
				Name:           "chrome",
				PortRelayPorts: []int{9222},
				Service:        []spec.ServiceEntry{{Name: "chrome", Exec: "chrome"}},
			}, spec.CandyView{}),
		},
		RenderService: fakeRenderService,
	}

	supervisordDef := &spec.ResolvedInit{
		Model:       "fragment_assembly",
		FragmentDir: "supervisor",
		ServiceSchema: &spec.InitServiceSchema{
			SupportsPackaged: false,
			ServiceTemplate:  "[program:{{.Name}}]\ncommand={{.Exec}}\n",
		},
		RelayTemplate: relayTmpl,
	}

	err := g.GenerateInitFragments("test-image", "supervisord", supervisordDef, []string{"socat", "chrome"})
	if err != nil {
		t.Fatalf("GenerateInitFragments() error = %v", err)
	}

	// Candy ordering: socat=1, chrome=2. chrome has both a service: entry
	// and a port_relay, producing 02-chrome.conf + 02-relay-9222.conf.
	data, err := os.ReadFile(tmpDir + "/test-image/supervisor/02-chrome.conf")
	if err != nil {
		t.Fatalf("reading chrome supervisor config: %v", err)
	}
	if !strings.Contains(string(data), "[program:chrome]") {
		t.Error("chrome fragment should contain [program:chrome]")
	}

	data, err = os.ReadFile(tmpDir + "/test-image/supervisor/02-relay-9222.conf")
	if err != nil {
		t.Fatalf("reading relay supervisor config: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "[program:relay-9222]") {
		t.Error("relay fragment should contain [program:relay-9222]")
	}
	if !strings.Contains(content, "relay-wrapper 9222") {
		t.Error("relay fragment should contain relay-wrapper 9222 command")
	}
	if !strings.Contains(content, "autostart=true") {
		t.Error("relay fragment should have autostart=true")
	}
	if !strings.Contains(content, "priority=1") {
		t.Error("relay fragment should have priority=1")
	}

	// socat has no supervisord or port_relay, should not have a config
	_, err = os.ReadFile(tmpDir + "/test-image/supervisor/01-socat.conf")
	if err == nil {
		t.Error("socat should not have a supervisor config")
	}
}
