package deploykit

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/opencharly/spec/spec"
)

func candyWithFiles(t *testing.T, name string, files ...string) CandyModel {
	t.Helper()
	dir := t.TempDir()
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return NewSpecCandyModel(
		spec.CandyModel{Name: name, SourceDir: dir},
		spec.CandyView{},
	)
}

// `candy_file:` was consulted for init DETECTION only. The candy scan is init-agnostic,
// so it collected a hardcoded `*.service` glob into CandyModel.ServiceFiles — which
// meant OpenRC's declared `candy_file: ['*.initd']` had never matched a file, and an
// init could not widen its set at all: adding `*.socket` to the vocabulary would have
// been a no-op because the scan still only found `*.service`.
func TestInitUnitFilesHonoursCandyFilePatterns(t *testing.T) {
	layer := candyWithFiles(t, "svc", "a.service", "b.socket", "c.target", "d.initd", "notes.md")

	t.Run("openrc claims its declared .initd", func(t *testing.T) {
		def := &spec.ResolvedInit{Model: "file_copy", CandyFiles: []string{"*.initd"}}
		got := base(initUnitFiles(def, layer))
		want := []string{"d.initd"}
		if !equal(got, want) {
			t.Errorf("got %v, want %v — the init's declared candy_file: is still ignored", got, want)
		}
	})

	t.Run("an init may claim several patterns", func(t *testing.T) {
		def := &spec.ResolvedInit{
			Model:      "file_copy",
			CandyFiles: []string{"*.service", "*.socket", "*.target"},
		}
		got := base(initUnitFiles(def, layer))
		want := []string{"a.service", "b.socket", "c.target"}
		if !equal(got, want) {
			t.Errorf("got %v, want %v — a widened vocabulary must actually collect the new units", got, want)
		}
	})

	t.Run("no candy_file keeps the historical *.service glob", func(t *testing.T) {
		def := &spec.ResolvedInit{Model: "file_copy"}
		got := base(initUnitFiles(def, layer))
		want := []string{"a.service"}
		if !equal(got, want) {
			t.Errorf("got %v, want %v — an init declaring no patterns must render exactly as before", got, want)
		}
	})

	t.Run("unrelated files are never claimed", func(t *testing.T) {
		def := &spec.ResolvedInit{Model: "file_copy", CandyFiles: []string{"*.service"}}
		for _, f := range base(initUnitFiles(def, layer)) {
			if f == "notes.md" {
				t.Error("claimed a file no pattern matches")
			}
		}
	})
}

// An init whose depends_candy names a candy the project cannot see produces an image
// that declares that init with no binary to run it. Declining to inject is correct;
// doing it silently is the defect — the failure then surfaces at `charly start` as a
// bare "executable file not found in $PATH", naming neither the init nor the candy.
func TestUnsatisfiedInitDependsIsReported(t *testing.T) {
	var mu sync.Mutex
	var calls [][3]string
	orig := unsatisfiedInitDepends
	unsatisfiedInitDepends = func(box, init, candy string) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, [3]string{box, init, candy})
	}
	t.Cleanup(func() { unsatisfiedInitDepends = orig })
	unsatisfiedInitDependsSeen = sync.Map{} // package state; keep the case independent

	cfg := &spec.Config{}
	cfg.SetBox("lonely", spec.Box{Candy: []string{"app"}})
	// The candy TRIGGERS supervisord (so the init resolves) but the `supervisord`
	// candy that init depends on is NOT in the scanned set — the exact shape a
	// standalone candy repo or spike project has.
	layers := map[string]CandyModel{"app": serviceCandy("app", "supervisord")}
	vocab := initVocabFixture()

	InjectInitDependsCandy(cfg, layers, vocab)

	if len(calls) == 0 {
		t.Fatal("an unsatisfiable depends_candy was passed over in silence: the box will build " +
			"an image whose declared init has no binary, and nothing said so")
	}
	if calls[0][0] != "lonely" {
		t.Errorf("reported box = %q, want lonely", calls[0][0])
	}
	if calls[0][2] == "" {
		t.Error("the report does not name the candy that would have satisfied the init")
	}

	// The injector runs at every box-resolution chokepoint; the report must not
	// triple up and read like three separate problems.
	before := len(calls)
	InjectInitDependsCandy(cfg, layers, vocab)
	InjectInitDependsCandy(cfg, layers, vocab)
	if len(calls) != before {
		t.Errorf("report repeated across chokepoints: %d calls after two more passes, want %d",
			len(calls), before)
	}
}

func base(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, filepath.Base(p))
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
