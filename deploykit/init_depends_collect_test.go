package deploykit

import (
	"slices"
	"testing"

	"github.com/opencharly/spec/spec"
)

// init_depends_collect_test.go — the SEAM between the init `depends_candy:` injection and the
// cross-candy chain COLLECTORS.
//
// The first cut of the injection taught the box's composition to gain the active init's candy, but
// it wrote the DERIVED view (ResolvedBox.Candy) instead of the source. The five chain collectors
// (CollectDescriptions / CollectBoxPorts / CollectBoxVolume / CollectHooks / CollectShell) re-derive
// the composition from cfg via BoxCandyChain, so an injected candy contributed NOTHING to any of
// them: the shipped image ran supervisord as PID 1 while `charly box pull` + `charly check box` no
// longer tested it, and the emitted ai.opencharly.description label carried only the hand-authored
// candies.
//
// These tests pin the general property — an injected candy is a FULL member of the composition,
// contributing its plan, ports and volumes exactly as a hand-authored one does — so the seam cannot
// silently reopen for the next composition-time transform. Nothing here names "supervisord" or
// "init" as a special case: the fixture's init candy carries an ordinary plan step, port and volume,
// and is asserted through the ordinary collectors.

// collectSeamFixture builds the exact shape the check-tutorial-shell bed exercises: a box composing a
// tool candy and a service candy WITHOUT naming an init, plus an init candy carrying real
// contributions of its own (a check step, a port, a volume) so every collector has something to miss.
func collectSeamFixture() (*spec.Config, map[string]CandyModel) {
	cfg := boxCfg(map[string][]string{"tutorial-shell": {"ripgrep", "sshd"}})
	layers := map[string]CandyModel{
		"ripgrep": plainCandy("ripgrep"),
		"sshd":    serviceCandy("sshd", "supervisord"),
		"supervisord": NewSpecCandyModel(
			spec.CandyModel{
				Name: "supervisord",
				Plan: []spec.Step{{Check: "supervisord runs as container init and its control socket answers supervisorctl pid"}},
				Port: []spec.PortSpec{{Port: 9001}},
			},
			spec.CandyView{
				Name:        "supervisord",
				Description: "process control system that runs as container init",
				Volumes:     []spec.CandyVolume{{Name: "supervisord-run", Path: "/run/supervisord"}},
			},
		),
	}
	return cfg, layers
}

// TestInjectedCandyReachesCollectedDescription is the ADE-critical direction and the one the bed
// measured: the injected init candy's own acceptance plan must reach the baked
// ai.opencharly.description label. Without it the image ships an init that `charly check box` never
// probes — the shipped-but-untested hole.
func TestInjectedCandyReachesCollectedDescription(t *testing.T) {
	cfg, layers := collectSeamFixture()

	InjectInitDependsCandy(cfg, layers, initVocabFixture())

	set := CollectDescriptions(cfg, layers, "tutorial-shell")
	if set == nil {
		t.Fatal("CollectDescriptions returned nil")
	}
	var origins []string
	for _, ld := range set.Candy {
		origins = append(origins, ld.Origin)
	}
	if !slices.Contains(origins, "candy:supervisord") {
		t.Fatalf("injected candy must contribute its plan to the description label, got origins %v", origins)
	}
}

// TestInjectedCandyReachesCollectedPorts proves the fix is GENERAL, not a description special case:
// a port an injected candy declares is published like any other candy's.
func TestInjectedCandyReachesCollectedPorts(t *testing.T) {
	cfg, layers := collectSeamFixture()

	InjectInitDependsCandy(cfg, layers, initVocabFixture())

	ports, err := CollectBoxPorts(cfg, layers, "tutorial-shell")
	if err != nil {
		t.Fatalf("CollectBoxPorts() error = %v", err)
	}
	if !slices.Contains(ports, "9001") {
		t.Fatalf("injected candy must contribute its ports, got %v", ports)
	}
}

// TestInjectedCandyReachesCollectedVolumes is the third contribution class — same property, third
// collector, so a future transform that reopens the seam fails here too.
func TestInjectedCandyReachesCollectedVolumes(t *testing.T) {
	cfg, layers := collectSeamFixture()

	InjectInitDependsCandy(cfg, layers, initVocabFixture())

	mounts, err := CollectBoxVolume(cfg, layers, "tutorial-shell", "/home/user", nil)
	if err != nil {
		t.Fatalf("CollectBoxVolume() error = %v", err)
	}
	var names []string
	for _, m := range mounts {
		names = append(names, m.VolumeName)
	}
	if !slices.Contains(names, "charly-tutorial-shell-supervisord-run") {
		t.Fatalf("injected candy must contribute its volumes, got %v", names)
	}
}

// TestInjectedCandyReachesBaseChainCollectors pins the INHERITANCE half: a child box built on a base
// whose composition triggers the init must see the injected candy's contributions through the shared
// base-chain walk, exactly as it sees a hand-authored base candy's. This is the shape every distro
// box actually has — the services live on the base, the leaf just extends it.
func TestInjectedCandyReachesBaseChainCollectors(t *testing.T) {
	_, layers := collectSeamFixture()
	cfg := &spec.Config{Box: spec.BoxMap{}}
	cfg.SetBox("base-shell", spec.BoxConfig{Base: "quay.io/fedora/fedora:43", Build: []string{"rpm"}, Candy: []string{"sshd"}})
	cfg.SetBox("leaf", spec.BoxConfig{Base: "base-shell", Build: []string{"rpm"}, Candy: []string{"ripgrep"}})

	InjectInitDependsCandy(cfg, layers, initVocabFixture())

	chain, err := BoxCandyChain(cfg, layers, "leaf")
	if err != nil {
		t.Fatalf("BoxCandyChain() error = %v", err)
	}
	if !slices.Contains(chain, "supervisord") {
		t.Fatalf("leaf must inherit the base's injected init candy, got %v", chain)
	}
	// De-duplicated: the leaf's own injection and the base's must not double-count the candy, or
	// every collector would emit its contributions twice.
	if n := slices.Index(chain, "supervisord"); slices.Contains(chain[n+1:], "supervisord") {
		t.Errorf("injected candy must appear once across the chain, got %v", chain)
	}
}

// TestInjectionIsIdempotentAcrossChokepoints is the shared-state guard, and it is load-bearing in a
// way it was not when the pass wrote a per-call resolved-box map: the pass now mutates a *spec.Config
// that OUTLIVES one call, and all three composition chokepoints — candy/plugin-build's
// resolveBuildEngine, loaderkit.ProjectResolvedProject and FillNamespaceBoxViews — can fire against
// the SAME config in one process (the build path runs the first, then hands its cfg to the second).
//
// A double-injection would not merely lengthen a list: BoxCandyChain de-duplicates, but the candy
// would be counted twice by anything reading the box's own order, so this asserts through both the
// authored list and the collector-facing chain.
func TestInjectionIsIdempotentAcrossChokepoints(t *testing.T) {
	cfg, layers := collectSeamFixture()
	vocab := initVocabFixture()

	InjectInitDependsCandy(cfg, layers, vocab) // chokepoint 1 — the build path
	after1 := append([]string(nil), boxCandy(t, cfg, "tutorial-shell")...)
	InjectInitDependsCandy(cfg, layers, vocab) // chokepoint 2 — the envelope assembler
	InjectInitDependsCandy(cfg, layers, vocab) // chokepoint 3 — the namespaced set

	if got := boxCandy(t, cfg, "tutorial-shell"); !slices.Equal(got, after1) {
		t.Fatalf("three passes over one config must equal one: %v -> %v", after1, got)
	}
	if n := slices.Index(after1, "supervisord"); n < 0 || slices.Contains(after1[n+1:], "supervisord") {
		t.Fatalf("injected candy must appear exactly once, got %v", after1)
	}
	chain, err := BoxCandyChain(cfg, layers, "tutorial-shell")
	if err != nil {
		t.Fatalf("BoxCandyChain() error = %v", err)
	}
	n := slices.Index(chain, "supervisord")
	if n < 0 || slices.Contains(chain[n+1:], "supervisord") {
		t.Fatalf("collector chain must carry the injected candy exactly once, got %v", chain)
	}
}

// TestInjectionIsIdempotentAcrossChokepointsRemoteKeyed is the same guard on the shape that actually
// ships. The local-keyed case above is protected by the FIRST presence check (key == name, so the
// bare-name comparison already matches); only a REMOTE-keyed init candy — every distro submodule,
// including the box the check-tutorial-shell bed builds — depends on the key-comparison guard. A
// mutation that deletes that guard leaves the local case green and double-injects here, so without
// this case the chokepoint idempotence proof is vacuous for the shape in production.
func TestInjectionIsIdempotentAcrossChokepointsRemoteKeyed(t *testing.T) {
	const remoteKey = "github.com/opencharly/charly/candy/supervisord"
	layers := map[string]CandyModel{
		"github.com/opencharly/charly/candy/sshd": serviceCandy("sshd", "supervisord"),
		remoteKey: plainCandy("supervisord"),
	}
	cfg := boxCfg(map[string][]string{
		"tutorial-shell": {"@github.com/opencharly/charly/candy/sshd:2026.200.1200"},
	})
	vocab := initVocabFixture()

	InjectInitDependsCandy(cfg, layers, vocab)
	after1 := append([]string(nil), boxCandy(t, cfg, "tutorial-shell")...)
	InjectInitDependsCandy(cfg, layers, vocab)
	InjectInitDependsCandy(cfg, layers, vocab)

	if got := boxCandy(t, cfg, "tutorial-shell"); !slices.Equal(got, after1) {
		t.Fatalf("three passes over one config must equal one: %v -> %v", after1, got)
	}
	if n := slices.Index(after1, remoteKey); n < 0 || slices.Contains(after1[n+1:], remoteKey) {
		t.Fatalf("remote-keyed init candy must appear exactly once, got %v", after1)
	}
}

// TestInjectionLeavesMachineVenueCollectorsAlone is the negative direction at the COLLECTOR level:
// a machine-venue composition resolves to systemd, whose init def declares no depends_candy, so
// nothing may appear in any collector's output. This is the half a target-blind fix gets wrong.
func TestInjectionLeavesMachineVenueCollectorsAlone(t *testing.T) {
	cfg := boxCfg(map[string][]string{"os": {"bootc-base", "sshd"}})
	layers := map[string]CandyModel{
		"bootc-base":  bootcCandy("bootc-base"),
		"sshd":        serviceCandy("sshd", "supervisord", "systemd"),
		"supervisord": plainCandy("supervisord"),
	}

	InjectInitDependsCandy(cfg, layers, initVocabFixture())

	chain, err := BoxCandyChain(cfg, layers, "os")
	if err != nil {
		t.Fatalf("BoxCandyChain() error = %v", err)
	}
	if slices.Contains(chain, "supervisord") {
		t.Fatalf("machine-venue composition must not gain the init candy, got %v", chain)
	}
}
