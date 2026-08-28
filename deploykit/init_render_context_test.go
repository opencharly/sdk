package deploykit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// GenerateInitFragments used to hand-build a 13-field ServiceRenderContext from the
// entry. RenderService then applies BuildServiceRenderContext, which OVERWRITES every
// entry-derived field — so all of that was dead — except After/Before, which it
// APPENDS. Pre-seeding those double-listed every ordering directive.
//
// Worse, the fields it should have carried were the ones absent: without Home, the
// home-expansion pass is skipped, so `%(ENV_HOME)s` reaches a systemd unit verbatim
// (supervisord expands it natively, which is why this stayed hidden), and without the
// unit dirs a `scope: user` unit has nowhere to land.
func TestGenerateInitFragmentsRenderContextIsNotDoubleSeeded(t *testing.T) {
	var seen []spec.ServiceRenderContext
	capture := func(entry *spec.ServiceEntry, def *spec.ResolvedInit, ctx spec.ServiceRenderContext) (*spec.RenderedService, error) {
		ctx = spec.BuildServiceRenderContext(entry, ctx)
		seen = append(seen, ctx)
		return &spec.RenderedService{UnitText: "[program:" + ctx.Name + "]\n"}, nil
	}

	tmpDir := t.TempDir()
	g := &Generator{
		BuildDir: tmpDir,
		Candies: map[string]CandyModel{
			"svc": NewSpecCandyModel(spec.CandyModel{
				Name: "svc",
				Service: []spec.ServiceEntry{{
					Name:   "svc",
					Exec:   "%(ENV_HOME)s/.local/bin/svc",
					After:  []string{"network.target"},
					Before: []string{"shutdown.target"},
				}},
			}, spec.CandyView{}),
		},
		Boxes: map[string]*ResolvedBox{
			"test-image": {ResolvedBox: spec.ResolvedBox{Home: "/home/user"}},
		},
		RenderService: capture,
	}

	def := &spec.ResolvedInit{
		Model:         "fragment_assembly",
		FragmentDir:   "supervisor",
		ServiceSchema: &spec.InitServiceSchema{ServiceTemplate: "[program:{{.Name}}]\n"},
	}
	if err := g.GenerateInitFragments("test-image", "supervisord", def, []string{"svc"}); err != nil {
		t.Fatalf("GenerateInitFragments: %v", err)
	}
	if len(seen) != 1 {
		t.Fatalf("expected 1 rendered entry, got %d", len(seen))
	}
	ctx := seen[0]

	// The defect: After/Before arrive twice because the caller seeded what
	// BuildServiceRenderContext appends to.
	if got := len(ctx.After); got != 1 {
		t.Errorf("After double-listed: got %d entries %v, want exactly 1 — the caller "+
			"seeded a field BuildServiceRenderContext appends to", got, ctx.After)
	}
	if got := len(ctx.Before); got != 1 {
		t.Errorf("Before double-listed: got %d entries %v, want exactly 1", got, ctx.Before)
	}

	// The other half: the context must carry what the entry cannot supply.
	if ctx.Home != "/home/user" {
		t.Errorf("Home = %q, want the image home — without it the home-expansion pass "+
			"never runs and %%(ENV_HOME)s reaches a systemd unit verbatim", ctx.Home)
	}
	if !strings.HasPrefix(ctx.Exec, "/home/user/") {
		t.Errorf("Exec = %q, want the home token expanded", ctx.Exec)
	}
	if ctx.SystemUnitDir == "" || ctx.UserUnitDir == "" {
		t.Errorf("unit dirs unset (system=%q user=%q): a scope: user unit has nowhere to land",
			ctx.SystemUnitDir, ctx.UserUnitDir)
	}
	if ctx.Candy != "svc" {
		t.Errorf("Candy = %q, want svc — the one field BuildServiceRenderContext cannot derive", ctx.Candy)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, "test-image", "supervisor", "01-svc.conf")); err != nil {
		t.Errorf("fragment not written: %v", err)
	}
}

// `#Box.init` is declared in the schema and generated into the Box model, and every
// ResolveInitSystem call site passed a hardcoded "" — so authoring `init: systemd`
// on a box did nothing at all, silently, while the parameter it should have reached
// was fully implemented with a correct fall-through.
func TestAuthoredInitIsRead(t *testing.T) {
	cfg := &spec.Config{}
	cfg.SetBox("declared", spec.Box{Init: "systemd"})
	cfg.SetBox("undeclared", spec.Box{})

	if got := AuthoredInit(cfg, "declared"); got != "systemd" {
		t.Errorf("AuthoredInit(declared) = %q, want systemd — the box's init: is ignored", got)
	}
	if got := AuthoredInit(cfg, "undeclared"); got != "" {
		t.Errorf("AuthoredInit(undeclared) = %q, want empty so auto-detect still applies", got)
	}
	if got := AuthoredInit(nil, "declared"); got != "" {
		t.Errorf("AuthoredInit(nil cfg) = %q, want empty", got)
	}
	if got := AuthoredInit(cfg, "missing"); got != "" {
		t.Errorf("AuthoredInit(missing box) = %q, want empty", got)
	}
}

// Regression guard for the way #Box.init went dead in the first place: a call site
// that passes a hardcoded "" cannot honour an authored init, and nothing else in the
// package would notice.
func TestNoResolveInitSystemCallSitePassesHardcodedEmpty(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), `ResolveInitSystem(`) &&
			strings.Contains(string(b), `, "")`) &&
			strings.Contains(string(b), `ResolveInitSystem(g.Candies, candyOrder, "")`) {
			t.Errorf("%s calls ResolveInitSystem with a hardcoded \"\": an authored "+
				"#Box.init cannot reach it. Pass AuthoredInit(cfg, boxName).", f)
		}
	}
}
