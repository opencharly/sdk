// parse_doc_stream_test.go — the ONE doc-stream composer (parser consolidation F2.5): the
// per-document classify → #NodeDoc gate → registered DocParser → directive serialization →
// import/discover collection pipeline, driven identically by the file walk and the
// binary-embedded default-vocabulary stream. These tests drive ParseDocStream directly with a
// stub WalkSeams (the same seams the walk threads), locking the composer's contract: docs in
// stream order, empty-doc skip, deterministic directive bytes, and the import/discover
// collections the file walk consumes.
package loaderkit

import (
	"errors"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// streamSeams is a stub spec.WalkSeams for the composer tests: the default parser + a Threaded
// snapshot + a gate that records every gated label.
func streamSeams(t *testing.T) (spec.WalkSeams, *[]string) {
	t.Helper()
	var gated []string
	return spec.WalkSeams{
		Parser:   DocParser{},
		Threaded: func() spec.Threaded { return candyThreaded },
		GateDoc: func(label string, raw []byte) error {
			gated = append(gated, label)
			return nil
		},
	}, &gated
}

// TestParseDocStream_MultiDocStream: one stream with a node-form doc, an empty doc (skipped)
// and a second node-form doc — the composer returns exactly the two parsed docs in stream
// order, gates each node-form doc, and skips the empty one.
func TestParseDocStream_MultiDocStream(t *testing.T) {
	seams, gated := streamSeams(t)
	data := []byte("redis:\n  candy:\n    version: \"2026.150.0000\"\n---\n---\nweb:\n  pod:\n    from: img\n")
	docs, imports, specs, err := ParseDocStream(data, "stream-test", "srcdir", seams)
	if err != nil {
		t.Fatalf("ParseDocStream: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("docs = %d, want 2 (the empty doc is skipped)", len(docs))
	}
	if docs[0].Project.Nodes[0].Name != "redis" || docs[1].Project.Nodes[0].Name != "web" {
		t.Fatalf("docs out of order or wrong parse: %+v", docs)
	}
	if len(*gated) != 2 || (*gated)[0] != "stream-test:doc0" {
		t.Fatalf("gate labels = %v, want stream-test:doc0 + stream-test:doc2", *gated)
	}
	if len(imports) != 0 || len(specs) != 0 {
		t.Fatalf("unexpected imports/specs for a directive-less stream: %v %v", imports, specs)
	}
}

// TestParseDocStream_DirectivesCollected: a doc carrying version/discover/import directives —
// the composer serializes the directives deterministically (sorted keys) into the LoadedDoc and
// collects the flat import queue + anchored discover scan-specs for the walk.
func TestParseDocStream_DirectivesCollected(t *testing.T) {
	seams, _ := streamSeams(t)
	data := []byte("version: \"2026.150.0000\"\nimport:\n  - base.yml\ndiscover:\n  - path: candy\n    recursive: true\nfoo:\n  pod:\n    image: x\n")
	docs, imports, specs, err := ParseDocStream(data, "dir-test", "/proj", seams)
	if err != nil {
		t.Fatalf("ParseDocStream: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("docs = %d, want 1", len(docs))
	}
	d := docs[0]
	if !strings.Contains(string(d.Directives), "version") || !strings.Contains(string(d.Directives), "discover") {
		t.Fatalf("directives body incomplete: %s", d.Directives)
	}
	// Sorted-key determinism: version < discover < import alphabetically.
	if !strings.HasPrefix(string(d.Directives), "discover:") {
		t.Fatalf("directives body must start with the alphabetically-first key 'discover': %s", d.Directives)
	}
	if len(imports) != 1 || imports[0].Ref != "base.yml" {
		t.Fatalf("import queue = %v, want [base.yml]", imports)
	}
	if len(specs) != 1 || specs[0].Path != "/proj/candy" {
		t.Fatalf("scan specs = %v, want one candy scan anchored under /proj", specs)
	}
}

// TestParseDocStream_GateFailPropagates: a GateDoc failure aborts the whole stream, exactly as
// the walk's validate-before-execute contract requires.
func TestParseDocStream_GateFailPropagates(t *testing.T) {
	seams := spec.WalkSeams{
		Parser:   DocParser{},
		Threaded: func() spec.Threaded { return candyThreaded },
		GateDoc:  func(label string, raw []byte) error { return errors.New("gate rejected the doc") },
	}
	_, _, _, err := ParseDocStream([]byte("a:\n  candy: {version: \"2026.150.0000\"}\n"), "gate-test", "", seams)
	if err == nil {
		t.Fatal("expected the gate failure to propagate")
	}
}
