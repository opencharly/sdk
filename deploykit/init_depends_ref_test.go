package deploykit

import (
	"slices"
	"testing"
)

// init_depends_ref_test.go — the REF-SHAPED `depends_candy:` direction.
//
// The init vocabulary may name its runtime candy by a pinned remote ref
// (`@github.com/opencharly/layer-supervisord:v2026.271.1817`) rather than a bare name. Every
// presence check in InjectInitDependsCandy compares against the BARE ref (the scanned map's key,
// and what a box's resolved order carries), so an un-normalized ref-shaped depends_candy matched
// NOTHING and every box reported itself unsatisfied even with the candy composed.

// TestInjectInitDependsCandy_RemoteRefDependsCandyIsInjected: the vocabulary names the runtime as a
// pinned remote ref, the scanned map keys it bare, and the box composes only the triggering service
// candy — the injection must still fire, by the map KEY.
func TestInjectInitDependsCandy_RemoteRefDependsCandyIsInjected(t *testing.T) {
	const remoteKey = "github.com/opencharly/layer-supervisord"
	layers := map[string]CandyModel{
		"github.com/opencharly/charly/candy/sshd": serviceCandy("sshd", "supervisord"),
		remoteKey: plainCandy("supervisord"),
	}
	cfg := boxCfg(map[string][]string{
		"tutorial-shell": {"@github.com/opencharly/charly/candy/sshd:2026.200.1200"},
	})
	vocab := initVocabFixture()
	vocab.Init["supervisord"].DependsCandy = "@github.com/opencharly/layer-supervisord:v2026.271.1817"

	InjectInitDependsCandy(cfg, layers, vocab)

	got := boxCandy(t, cfg, "tutorial-shell")
	if len(got) == 0 || got[0] != remoteKey {
		t.Fatalf("ref-shaped depends_candy must inject the candy's map KEY %q, got %v", remoteKey, got)
	}
	// Idempotence: a second pass over the now-satisfied box must change nothing.
	InjectInitDependsCandy(cfg, layers, vocab)
	if got2 := boxCandy(t, cfg, "tutorial-shell"); !slices.Equal(got2, got) {
		t.Fatalf("second pass must be a no-op: %v -> %v", got, got2)
	}
}

// TestInjectInitDependsCandy_BareDependsCandyStillInjects is the regression guard for the bare
// form: a `depends_candy: supervisord` against a candy keyed by its repo path must keep injecting
// the map key exactly as before.
func TestInjectInitDependsCandy_BareDependsCandyStillInjects(t *testing.T) {
	const remoteKey = "github.com/opencharly/layer-supervisord"
	layers := map[string]CandyModel{
		"github.com/opencharly/charly/candy/sshd": serviceCandy("sshd", "supervisord"),
		remoteKey: plainCandy("supervisord"),
	}
	cfg := boxCfg(map[string][]string{
		"tutorial-shell": {"@github.com/opencharly/charly/candy/sshd:2026.200.1200"},
	})

	InjectInitDependsCandy(cfg, layers, initVocabFixture())

	got := boxCandy(t, cfg, "tutorial-shell")
	if len(got) == 0 || got[0] != remoteKey {
		t.Fatalf("bare depends_candy must still inject the candy's map KEY %q, got %v", remoteKey, got)
	}
}

// TestPruneContainerInitForSystemd_RemoteKeyedSupervisord: a resolved order carrying the container
// init under its REPO-REF key must be pruned exactly as the bare name is, or supervisord reaches a
// systemd guest — the exact thing the prune exists to prevent.
func TestPruneContainerInitForSystemd_RemoteKeyedSupervisord(t *testing.T) {
	in := []string{"github.com/opencharly/layer-supervisord", "github.com/opencharly/pod-sshd"}

	got := PruneContainerInitForSystemd(in, HostContext{MachineVenue: true})
	if !slices.Equal(got, []string{"github.com/opencharly/pod-sshd"}) {
		t.Fatalf("ref-keyed supervisord must be pruned on a machine venue, got %v", got)
	}

	// A container compile (MachineVenue false) keeps the order verbatim — supervisord IS its init.
	if unchanged := PruneContainerInitForSystemd(in, HostContext{MachineVenue: false}); !slices.Equal(unchanged, in) {
		t.Fatalf("container compile must keep the order verbatim, got %v", unchanged)
	}
}

// TestPruneContainerInitForSystemd_BareSupervisordStaysPruned is a guard that the bare name is
// still pruned after the match was generalized to reuse the tolerant name rule.
func TestPruneContainerInitForSystemd_BareSupervisordStaysPruned(t *testing.T) {
	got := PruneContainerInitForSystemd([]string{"supervisord", "ripgrep"}, HostContext{MachineVenue: true})
	if !slices.Equal(got, []string{"ripgrep"}) {
		t.Fatalf("bare supervisord must stay pruned, got %v", got)
	}
}
