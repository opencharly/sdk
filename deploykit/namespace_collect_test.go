package deploykit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// namespace_collect_test.go — the namespace-qualified entry coverage for the whole box-collector
// family. Every collector here is fed a box reached under an IMPORT NAMESPACE (`distro.app`),
// which is how EVERY box the superproject builds is reached after the box inversion (boxes live
// in the box/<distro> submodules, mounted as import namespaces).
//
// Before BoxOwner these collectors read the qualified name through the flat, root-internal
// cfg.BoxConfig / cfg.WalkBaseChain lookups, found nothing, and returned an EMPTY result — so an
// imported box baked no ai.opencharly.description (ADE's plan: label, leaving `charly check box`
// answering "No plan steps defined for this image" and exiting GREEN), no port (hence no EXPOSE),
// no volume, security, shell or alias. Each test below fails on the pre-fix code.

// nsFixture builds a root Config importing one namespace `distro` that owns a two-level base
// chain (`app` on `base`), plus the flat candy map shared across namespaces (its keys are bare
// local names or full remote refs — never namespace-qualified, which is why a namespace-relative
// candy ref resolves against the SAME map).
func nsFixture() (*spec.Config, map[string]CandyModel) {
	sub := &spec.Config{Box: spec.BoxMap{
		"base": spec.EncodeBox(spec.BoxConfig{
			Candy: []string{"sshd"},
		}),
		"app": spec.EncodeBox(spec.BoxConfig{
			Base:        "base",
			Candy:       []string{"redis"},
			Description: "the app box",
			Plan: []spec.Step{{Check: "the app answers", Op: spec.Op{
				Plugin: "port", PluginInput: map[string]any{"port": 6379},
			}}},
			Alias:    []spec.BoxAlias{{Name: "app", Command: "/usr/bin/app"}},
			Security: &spec.Security{CapAdd: []string{"SYS_PTRACE"}},
			Shell:    &spec.Shell{Init: "export APP=1"},
		}),
	}}
	root := &spec.Config{
		Box:        spec.BoxMap{"root-only": spec.EncodeBox(spec.BoxConfig{})},
		Namespaces: map[string]*spec.Config{"distro": sub},
	}
	layers := map[string]CandyModel{
		"sshd": NewSpecCandyModel(
			spec.CandyModel{
				Port: []spec.PortSpec{{Port: 2222}},
				// A data candy on the BASE box, so the data entries must be reached through the
				// base-chain walk, not just the leaf's own candies.
				Data: []spec.CandyData{{Src: "etc/sshd", Volume: "sshconf", Dest: "keys"}},
				Plan: []spec.Step{{Check: "sshd listens", Op: spec.Op{
					Plugin: "port", PluginInput: map[string]any{"port": 2222},
				}}},
			},
			spec.CandyView{Name: "sshd", Description: "openssh server"},
		),
		"redis": NewSpecCandyModel(
			spec.CandyModel{
				Port:     []spec.PortSpec{{Port: 6379}},
				Security: &spec.Security{CapAdd: []string{"IPC_LOCK"}},
				Shell:    &spec.Shell{Init: "export REDIS=1"},
				Plan: []spec.Step{{Check: "the redis binary exists", Op: spec.Op{
					Plugin: "file", PluginInput: map[string]any{"file": "/usr/bin/redis-server", "exists": true},
				}}},
			},
			// Volumes/Aliases are read off the VIEW (specCandyAdapter.Volume/Alias), not the model.
			spec.CandyView{
				Name:        "redis",
				Description: "redis store",
				Volumes:     []spec.CandyVolume{{Name: "data", Path: "/var/lib/redis"}},
				Aliases:     []spec.CandyAlias{{Name: "r", Command: "redis-cli"}},
			},
		),
	}
	return root, layers
}

func TestBoxOwner_DescendsIntoImportNamespace(t *testing.T) {
	root, _ := nsFixture()
	owner, leaf, img, ok := BoxOwner(root, "distro.app")
	if !ok {
		t.Fatal("BoxOwner(distro.app) not found — the qualified entry must descend into the namespace")
	}
	if leaf != "app" {
		t.Errorf("leaf = %q, want %q", leaf, "app")
	}
	if img.Description != "the app box" {
		t.Errorf("img.Description = %q, want the imported box's own authored description", img.Description)
	}
	if _, subOK := owner.BoxConfig("base"); !subOK {
		t.Error("owner Config must be the namespace that OWNS the box (its base refs are relative there)")
	}
	// A bare name still resolves in the root, unchanged.
	if _, _, _, rootOK := BoxOwner(root, "root-only"); !rootOK {
		t.Error("BoxOwner(root-only) must still resolve a bare root-local name")
	}
	if _, _, _, missOK := BoxOwner(root, "distro.absent"); missOK {
		t.Error("BoxOwner(distro.absent) must report not-found, not a zero-value hit")
	}
}

func TestBoxCandyChain_ImportedBoxWalksItsBaseChain(t *testing.T) {
	root, layers := nsFixture()
	got, err := BoxCandyChain(root, layers, "distro.app")
	if err != nil {
		t.Fatalf("BoxCandyChain() error = %v", err)
	}
	want := []string{"redis", "sshd"}
	if len(got) != len(want) {
		t.Fatalf("BoxCandyChain(distro.app) = %v, want %v (own candies then the base's)", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("BoxCandyChain(distro.app)[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestBoxDirectCandies_ImportedBoxResolvesOwnCandies(t *testing.T) {
	root, layers := nsFixture()
	got, err := BoxDirectCandies(root, layers, "distro.app")
	if err != nil {
		t.Fatalf("BoxDirectCandies() error = %v", err)
	}
	if len(got) != 1 || got[0] != "redis" {
		t.Errorf("BoxDirectCandies(distro.app) = %v, want [redis] (own candies, no base inheritance)", got)
	}
}

// TestCollectDescriptions_ImportedBoxBakesNonEmptyLabel is the discriminating guard for the
// defect: an imported box MUST bake a non-empty ai.opencharly.description, carrying both its
// candy-chain plans and its own box-level plan. Pre-fix the set was nil and the label was omitted
// entirely, so `charly check box` found no plan and passed vacuously.
func TestCollectDescriptions_ImportedBoxBakesNonEmptyLabel(t *testing.T) {
	root, layers := nsFixture()
	readers := make(map[string]spec.CandyReader, len(layers))
	for k, v := range layers {
		readers[k] = v
	}

	set := CollectDescriptions(root, readers, "distro.app")
	if set == nil || set.IsEmpty() {
		t.Fatal("CollectDescriptions(distro.app) = empty — an imported box must bake its plan into ai.opencharly.description")
	}
	if len(set.Candy) != 2 {
		t.Fatalf("candy sections = %d, want 2 (redis + the base's sshd)", len(set.Candy))
	}
	steps := 0
	for _, ld := range set.Candy {
		steps += len(ld.Plan)
	}
	if steps != 2 {
		t.Errorf("baked candy-chain check steps = %d, want 2", steps)
	}
	if len(set.Box) != 1 || set.Box[0].Description != "the app box" {
		t.Fatalf("box section = %+v, want the imported box's own description + plan", set.Box)
	}
	if len(set.Box[0].Plan) != 1 {
		t.Errorf("baked box-level plan = %d steps, want 1", len(set.Box[0].Plan))
	}
}

func TestCollectBoxPorts_ImportedBoxInheritsChainPorts(t *testing.T) {
	root, layers := nsFixture()
	got, err := CollectBoxPorts(root, layers, "distro.app")
	if err != nil {
		t.Fatalf("CollectBoxPorts() error = %v", err)
	}
	want := []string{"2222", "6379"}
	if len(got) != len(want) {
		t.Fatalf("CollectBoxPorts(distro.app) = %v, want %v — no ports means no ai.opencharly.port AND no EXPOSE", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("CollectBoxPorts(distro.app)[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestCollectBoxVolume_ImportedBoxCollectsCandyVolumes(t *testing.T) {
	root, layers := nsFixture()
	got, err := CollectBoxVolume(root, layers, "distro.app", "/home/user", nil)
	if err != nil {
		t.Fatalf("CollectBoxVolume() error = %v", err)
	}
	if len(got) != 1 || got[0].ContainerPath != "/var/lib/redis" {
		t.Errorf("CollectBoxVolume(distro.app) = %+v, want the redis candy's /var/lib/redis volume", got)
	}
}

func TestCollectSecurity_ImportedBoxMergesCandyAndBox(t *testing.T) {
	root, layers := nsFixture()
	got := CollectSecurity(root, layers, "distro.app")
	caps := map[string]bool{}
	for _, c := range got.CapAdd {
		caps[c] = true
	}
	if !caps["IPC_LOCK"] {
		t.Errorf("CapAdd = %v, want the redis candy's IPC_LOCK", got.CapAdd)
	}
	if !caps["SYS_PTRACE"] {
		t.Errorf("CapAdd = %v, want the imported box's own SYS_PTRACE override", got.CapAdd)
	}
}

func TestCollectShell_ImportedBoxCollectsCandyAndBoxSections(t *testing.T) {
	root, layers := nsFixture()
	got := CollectShell(root, layers, "distro.app")
	if got == nil {
		t.Fatal("CollectShell(distro.app) = nil — an imported box must bake ai.opencharly.shell")
	}
	if len(got.Candy) != 1 || got.Candy[0].ID != "redis" {
		t.Errorf("shell candy section = %+v, want the redis candy entry", got.Candy)
	}
	if len(got.Box) != 1 {
		t.Errorf("shell box section = %+v, want the imported box's own shell entry", got.Box)
	}
}

func TestCollectBoxAlias_ImportedBoxCollectsCandyAndBoxAliases(t *testing.T) {
	root, layers := nsFixture()
	readers := make(map[string]spec.CandyReader, len(layers))
	for k, v := range layers {
		readers[k] = v
	}
	got, err := CollectBoxAlias(root, readers, "distro.app")
	if err != nil {
		t.Fatalf("CollectBoxAlias() error = %v", err)
	}
	names := map[string]string{}
	for _, a := range got {
		names[a.Name] = a.Command
	}
	if names["r"] != "redis-cli" {
		t.Errorf("aliases = %+v, want the redis candy's `r` alias", got)
	}
	if names["app"] != "/usr/bin/app" {
		t.Errorf("aliases = %+v, want the imported box's own `app` alias", got)
	}
}

// TestCollectChainDataEntries_ImportedBoxCollectsChainData covers the ELEVENTH site — the one a
// token-keyed grep for `BoxConfig(boxName)` missed, because it spelled the same lookup
// `BoxConfig(current)` inside a hand-rolled copy of the base-chain walk. Pre-fix an imported box
// emitted NO ai.opencharly.data entries at all.
//
// The assertions are on the entry's CONTENT (volume / staging / candy / dest), not on mere
// non-emptiness: a length check alone would pass on a wrongly-derived staging path, and the whole
// point of this label is that a deploy reads the staging path back.
func TestCollectChainDataEntries_ImportedBoxCollectsChainData(t *testing.T) {
	root, layers := nsFixture()
	g := NewRenderGenerator()
	g.Config = root
	g.Candies = layers

	got := g.collectChainDataEntries("distro.app")
	if len(got) != 1 {
		t.Fatalf("collectChainDataEntries(distro.app) = %+v, want the base chain's one sshd data entry", got)
	}
	e := got[0]
	if e.Volume != "sshconf" {
		t.Errorf("Volume = %q, want %q", e.Volume, "sshconf")
	}
	if e.Candy != "sshd" {
		t.Errorf("Candy = %q, want %q (the entry must be attributed to the BASE box's candy)", e.Candy, "sshd")
	}
	if e.Dest != "keys" {
		t.Errorf("Dest = %q, want %q", e.Dest, "keys")
	}
	// The staging path is what the deploy reads back to find the staged tree; a trailing slash is
	// load-bearing for the COPY that consumes it.
	if e.Staging != "/data/sshconf/keys/" {
		t.Errorf("Staging = %q, want %q", e.Staging, "/data/sshconf/keys/")
	}
}

func TestCollectHooks_ImportedBoxWalksChain(t *testing.T) {
	root, layers := nsFixture()
	// The fixture declares no hooks; the guard is that the walk RESOLVES the qualified box at all
	// rather than short-circuiting on a flat-lookup miss — proven via the shared chain walk.
	names, err := BoxCandyChain(root, layers, "distro.app")
	if err != nil || len(names) == 0 {
		t.Fatalf("BoxCandyChain(distro.app) = %v, %v — CollectHooks/CollectSkills read the same walk", names, err)
	}
	if got := CollectHooks(root, layers, "distro.app"); got != nil {
		t.Errorf("CollectHooks = %+v, want nil for a hook-less chain", got)
	}
}
