package deploykit

import "testing"

// TestSetUnsatisfiedInitDependsReporterObservesAndRestores pins sdk#519: the "init
// depends_candy missing" detection prints to stderr through an injectable hook, but the
// hook was unexported, so a validator could not count the warning it caused — the
// summary then reported 0 warnings while printing one.
func TestSetUnsatisfiedInitDependsReporterObservesAndRestores(t *testing.T) {
	type call struct{ box, initName, candy string }
	var got []call

	prev := SetUnsatisfiedInitDependsReporter(func(box, initName, candy string) {
		got = append(got, call{box, initName, candy})
	})
	defer SetUnsatisfiedInitDependsReporter(prev)

	reportUnsatisfiedInitDepends("demo-a", "supervisord", "layer-supervisord")
	if len(got) != 1 {
		t.Fatalf("a swapped reporter must observe the warning; got %d calls: %v", len(got), got)
	}
	if got[0] != (call{"demo-a", "supervisord", "layer-supervisord"}) {
		t.Fatalf("reporter arguments must carry box/init/candy verbatim; got %+v", got[0])
	}

	// The dedupe is the CALLER's policy and must still hold through the seam: a second
	// identical report is suppressed, so a validator cannot double-count one warning.
	reportUnsatisfiedInitDepends("demo-a", "supervisord", "layer-supervisord")
	if len(got) != 1 {
		t.Fatalf("duplicate report must stay deduped through the seam; got %d calls", len(got))
	}

	// A distinct box is a distinct warning.
	reportUnsatisfiedInitDepends("demo-b", "supervisord", "layer-supervisord")
	if len(got) != 2 {
		t.Fatalf("a distinct box must report again; got %d calls", len(got))
	}

	// Restoring the previous reporter must stop delivery to the swapped one.
	SetUnsatisfiedInitDependsReporter(prev)
	reportUnsatisfiedInitDepends("demo-c", "supervisord", "layer-supervisord")
	if len(got) != 2 {
		t.Fatalf("after restore the old reporter must receive nothing; got %d calls", len(got))
	}
}
