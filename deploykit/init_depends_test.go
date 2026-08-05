package deploykit

import (
	"slices"
	"testing"

	"github.com/opencharly/spec/spec"
)

// init_depends_test.go — the DIRECTIONS of the init `depends_candy:` injection. The container
// direction fails without deploykit.InjectInitDependsCandy because nothing would add the supervisord
// candy; the systemd direction fails under a target-BLIND fix (e.g. a blanket `require: supervisord`
// on every service candy), which would wrongly install supervisord onto a machine venue that already
// has an init.
//
// The pass writes the AUTHORED composition on *spec.Config — the one source a box's candy list has,
// which the resolved boxes and every chain collector derive from — so these tests assert through
// cfg. init_depends_collect_test.go covers the collector-facing half of the same property.

// initVocabFixture mirrors the SHAPE of the real embedded init vocabulary (charly/charly.yml
// `init:`): supervisord declares a depends_candy (a container carries no init), systemd declares
// none (every machine venue already has one) and is gated on the preserve_user capability.
func initVocabFixture() *spec.InitConfig {
	return &spec.InitConfig{
		Init: map[string]*spec.ResolvedInit{
			"supervisord": {
				CandyFields:   []string{"service"},
				DependsCandy:  "supervisord",
				ServiceSchema: &spec.InitServiceSchema{ServiceTemplate: "[program:{{.Name}}]"},
			},
			"systemd": {
				CandyFields:        []string{"service"},
				RequiresCapability: []string{"preserve_user"},
				ServiceSchema:      &spec.InitServiceSchema{SupportsPackaged: true, ServiceTemplate: "[Service]"},
			},
		},
	}
}

// serviceCandy is a candy that triggers the named init systems — the InitSystems map is exactly what
// loaderkit.PopulateCandyInitSystem fills at scan time and what CandyReader.HasInit reads.
func serviceCandy(name string, inits ...string) CandyModel {
	triggers := map[string]bool{}
	for _, i := range inits {
		triggers[i] = true
	}
	return NewSpecCandyModel(
		spec.CandyModel{Name: name},
		spec.CandyView{Name: name, InitSystems: triggers},
	)
}

// plainCandy is a tool candy: packages and probes, no service, so it triggers no init at all.
func plainCandy(name string) CandyModel {
	return NewSpecCandyModel(spec.CandyModel{Name: name}, spec.CandyView{Name: name})
}

// bootcCandy carries the preserve_user capability — the canonical signal of a bootc-flavored
// (machine venue) composition, which is what makes ResolveInitSystem select systemd over supervisord.
// The capability rides the MODEL's Capability field: that is what CandyReader.Capabilities() (and so
// spec.AggregateCandyCapabilities) reads — the CandyView's own Capabilities field is a separate,
// view-side projection that this path never consults.
func bootcCandy(name string) CandyModel {
	return NewSpecCandyModel(
		spec.CandyModel{Name: name, Capability: &spec.CandyCapability{PreserveUser: true}},
		spec.CandyView{Name: name},
	)
}

// boxCfg builds a one-or-more-box *spec.Config from name → authored candy list.
func boxCfg(boxes map[string][]string) *spec.Config {
	cfg := &spec.Config{Box: spec.BoxMap{}}
	for name, candies := range boxes {
		cfg.SetBox(name, spec.BoxConfig{Base: "quay.io/fedora/fedora:43", Build: []string{"rpm"}, Candy: candies})
	}
	return cfg
}

// boxCandy reads a box's authored candy list back off the config.
func boxCandy(t *testing.T, cfg *spec.Config, name string) []string {
	t.Helper()
	img, ok := cfg.BoxConfig(name)
	if !ok {
		t.Fatalf("box %q missing from config", name)
	}
	return img.Candy
}

// TestInjectInitDependsCandy_ContainerInjectsSupervisord is the CONTAINER direction: a box composing
// a tool candy plus a service candy — and NOT naming an init — resolves to supervisord and must gain
// the supervisord candy. This is the exact shape that used to build clean, bake
// ai.opencharly.init="supervisord" and an assembled /etc/supervisord.conf, and then fail at deploy
// `[start]` with no supervisord binary in the image.
func TestInjectInitDependsCandy_ContainerInjectsSupervisord(t *testing.T) {
	layers := map[string]CandyModel{
		"ripgrep":     plainCandy("ripgrep"),
		"sshd":        serviceCandy("sshd", "supervisord", "systemd"),
		"supervisord": plainCandy("supervisord"),
	}
	cfg := boxCfg(map[string][]string{"tutorial-shell": {"ripgrep", "sshd"}})

	InjectInitDependsCandy(cfg, layers, initVocabFixture())

	got := boxCandy(t, cfg, "tutorial-shell")
	if !slices.Contains(got, "supervisord") {
		t.Fatalf("container composition must gain the supervisord candy, got %v", got)
	}
	// Prepended, matching how boxes that name the init explicitly already author it.
	if got[0] != "supervisord" {
		t.Errorf("injected candy must lead the list, got %v", got)
	}
}

// TestInjectInitDependsCandy_MachineVenueInjectsNothing is the SYSTEMD direction — the half a
// target-blind fix would get wrong. A preserve_user (bootc / machine venue) composition resolves to
// systemd, whose init def declares NO depends_candy, so nothing may be added: supervisord must never
// be installed onto a system that already has an init.
func TestInjectInitDependsCandy_MachineVenueInjectsNothing(t *testing.T) {
	layers := map[string]CandyModel{
		"bootc-base":  bootcCandy("bootc-base"),
		"sshd":        serviceCandy("sshd", "supervisord", "systemd"),
		"supervisord": plainCandy("supervisord"),
	}
	cfg := boxCfg(map[string][]string{"os": {"bootc-base", "sshd"}})

	InjectInitDependsCandy(cfg, layers, initVocabFixture())

	got := boxCandy(t, cfg, "os")
	if slices.Contains(got, "supervisord") {
		t.Fatalf("machine-venue composition must NOT gain the supervisord candy, got %v", got)
	}
	if len(got) != 2 {
		t.Errorf("machine-venue candy list must be untouched, got %v", got)
	}
}

// TestInjectInitDependsCandy_NoServiceCandyInjectsNothing pins the other no-op: a box with no
// service candy triggers no init, so it must not acquire one.
func TestInjectInitDependsCandy_NoServiceCandyInjectsNothing(t *testing.T) {
	layers := map[string]CandyModel{
		"ripgrep":     plainCandy("ripgrep"),
		"supervisord": plainCandy("supervisord"),
	}
	cfg := boxCfg(map[string][]string{"tools": {"ripgrep"}})

	InjectInitDependsCandy(cfg, layers, initVocabFixture())

	if got := boxCandy(t, cfg, "tools"); len(got) != 1 || got[0] != "ripgrep" {
		t.Fatalf("service-less box must be untouched, got %v", got)
	}
}

// TestInjectInitDependsCandy_Idempotent pins the property that lets the pass run at BOTH box-resolution
// chokepoints without double-injecting: a box already naming the init candy is left exactly as authored,
// and a second pass over an already-injected box adds nothing.
func TestInjectInitDependsCandy_Idempotent(t *testing.T) {
	layers := map[string]CandyModel{
		"sshd":        serviceCandy("sshd", "supervisord"),
		"supervisord": plainCandy("supervisord"),
	}
	cfg := boxCfg(map[string][]string{
		"explicit": {"supervisord", "sshd"},
		"implicit": {"sshd"},
	})
	vocab := initVocabFixture()

	InjectInitDependsCandy(cfg, layers, vocab)
	if got := boxCandy(t, cfg, "explicit"); len(got) != 2 {
		t.Fatalf("box already naming the init must be untouched, got %v", got)
	}

	first := boxCandy(t, cfg, "implicit")
	InjectInitDependsCandy(cfg, layers, vocab)
	if got := boxCandy(t, cfg, "implicit"); !slices.Equal(got, first) {
		t.Fatalf("second pass must be a no-op: %v -> %v", first, got)
	}
}

// TestInjectInitDependsCandy_TransitiveRequireSatisfies pins that the presence check reads the
// RESOLVED chain, not the authored list: a box naming only a meta-candy that already requires the
// init must not gain a duplicate entry.
func TestInjectInitDependsCandy_TransitiveRequireSatisfies(t *testing.T) {
	layers := map[string]CandyModel{
		"supervisord": plainCandy("supervisord"),
		"web-stack": NewSpecCandyModel(
			spec.CandyModel{Name: "web-stack"},
			spec.CandyView{Name: "web-stack", InitSystems: map[string]bool{"supervisord": true}, Require: []string{"supervisord"}},
		),
	}
	cfg := boxCfg(map[string][]string{"web": {"web-stack"}})

	InjectInitDependsCandy(cfg, layers, initVocabFixture())

	if got := boxCandy(t, cfg, "web"); len(got) != 1 || got[0] != "web-stack" {
		t.Fatalf("transitively-satisfied box must be untouched, got %v", got)
	}
}

// TestInjectInitDependsCandy_RemoteKeyedInitCandy is the shape EVERY distro submodule actually has,
// and the one the local-project fixtures above all miss: the init candy is a REMOTE `@github…` ref, so
// BareCandyRef keys it by its full repo path while the init vocabulary still names it bare. The
// injected entry must be the map KEY, not the bare name — injecting the name yields a dangling ref
// that resolves to nothing, which is exactly how the first cut of this pass silently no-opped on
// box/fedora while every unit test stayed green.
//
// The AUTHORED list here also carries the rich `@…:version` form the map keys never do. That side
// needs no handling in this pass: ResolveCandyOrder normalizes at its own chokepoint (ExpandCandy
// BareRef-normalizes every ref before lookup), so the raw authored list resolves correctly.
func TestInjectInitDependsCandy_RemoteKeyedInitCandy(t *testing.T) {
	const remoteKey = "github.com/opencharly/charly/candy/supervisord"
	layers := map[string]CandyModel{
		"github.com/opencharly/charly/candy/sshd": serviceCandy("sshd", "supervisord"),
		remoteKey: plainCandy("supervisord"),
	}
	cfg := boxCfg(map[string][]string{
		"tutorial-shell": {"@github.com/opencharly/charly/candy/sshd:2026.200.1200"},
	})

	InjectInitDependsCandy(cfg, layers, initVocabFixture())

	got := boxCandy(t, cfg, "tutorial-shell")
	if !slices.Contains(got, remoteKey) {
		t.Fatalf("remote init candy must be injected by its MAP KEY, got %v", got)
	}
	// A second pass must still be a no-op now that the presence check compares keys.
	InjectInitDependsCandy(cfg, layers, initVocabFixture())
	if got2 := boxCandy(t, cfg, "tutorial-shell"); !slices.Equal(got2, got) {
		t.Fatalf("second pass must be a no-op: %v -> %v", got, got2)
	}
}

// TestInjectInitDependsCandy_AbsentDependsCandyIsNoOp pins the reachability boundary: the injected
// entry is a BARE candy name, so a project whose scan contains no such candy gets nothing added.
// Injecting regardless would manufacture an "unknown candy" resolve failure that buries the project's
// real defect — exactly what it did to charly's TestValidate_PortRelayMissingSocat fixture, whose
// subject is a missing socat candy and which carries no supervisord candy at all.
func TestInjectInitDependsCandy_AbsentDependsCandyIsNoOp(t *testing.T) {
	layers := map[string]CandyModel{
		"svc": serviceCandy("svc", "supervisord"),
	}
	cfg := boxCfg(map[string][]string{"mybox": {"svc"}})

	InjectInitDependsCandy(cfg, layers, initVocabFixture())

	if got := boxCandy(t, cfg, "mybox"); len(got) != 1 || got[0] != "svc" {
		t.Fatalf("unreachable depends_candy must not be injected, got %v", got)
	}
}

// TestInjectInitDependsCandy_NilInitConfig pins the documented no-op for a project with no init
// vocabulary at all.
func TestInjectInitDependsCandy_NilInitConfig(t *testing.T) {
	cfg := boxCfg(map[string][]string{"b": {"sshd"}})
	InjectInitDependsCandy(cfg, map[string]CandyModel{"sshd": serviceCandy("sshd", "supervisord")}, nil)
	if got := boxCandy(t, cfg, "b"); len(got) != 1 {
		t.Fatalf("nil initCfg must be a no-op, got %v", got)
	}
}

// TestInjectInitDependsCandy_NilConfig pins the other guard — a nil config is a no-op, not a panic.
func TestInjectInitDependsCandy_NilConfig(t *testing.T) {
	InjectInitDependsCandy(nil, map[string]CandyModel{"sshd": serviceCandy("sshd", "supervisord")}, initVocabFixture())
}

// TestFillNamespaceBoxViews_InjectsInitDependsCandy is the THIRD box-composition path, and the one
// a reviewer is right to demand evidence for before the old validator guard is deleted: an import
// namespace's boxes are resolved from the namespace's OWN config inside FillNamespaceBoxViews, so
// they pass through NEITHER candy/plugin-build's resolveBuildEngine NOR
// loaderkit.ProjectResolvedProject's fresh-box loop. Without the injection call there, a namespaced
// box composing service candies would have its init RESOLVED (RenderPrepBox bakes
// ai.opencharly.init) while its init candy was never added — precisely the defect this pass closes.
func TestFillNamespaceBoxViews_InjectsInitDependsCandy(t *testing.T) {
	sub := &spec.Config{
		Box: spec.BoxMap{
			"nsbox": spec.EncodeBox(spec.BoxConfig{Base: "quay.io/fedora/fedora:43", Build: []string{"rpm"}, Candy: []string{"sshd"}}),
		},
	}
	nsLayers := map[string]spec.CandyReader{
		"sshd":        serviceCandy("sshd", "supervisord"),
		"supervisord": plainCandy("supervisord"),
	}
	rp := &spec.ResolvedProject{}

	opts := spec.ResolveOpts{DistroCfg: &spec.DistroConfig{}, BuilderCfg: &spec.BuilderConfig{}}
	FillNamespaceBoxViews(sub, nsLayers, initVocabFixture(), "ns", "2026.1.1", t.TempDir(), opts, rp)

	view, ok := rp.Boxes["ns.nsbox"]
	if !ok {
		t.Fatalf("namespaced box view missing; got %v", rp.Boxes)
	}
	if !slices.Contains(view.Candy, "supervisord") {
		t.Fatalf("namespaced box must gain the init candy, got %v", view.Candy)
	}
}
