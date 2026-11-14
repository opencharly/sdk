package deploykit

import (
	"os"
	"strings"
	"testing"
)

// TestUnsatisfiedInitDependsMessageIsTheDefaultReportersText pins sdk#521: a consumer that
// must put this warning into its OWN diagnostics (plugin-box's `box validate` summary) needs
// the text, and duplicating the prose in the consumer would be the drift sdk#519 exists to
// stop. The formatter is the ONE source; the default reporter must print exactly it.
func TestUnsatisfiedInitDependsMessageIsTheDefaultReportersText(t *testing.T) {
	msg := UnsatisfiedInitDependsMessage("demo-x", "supervisord", "layer-supervisord")
	for _, want := range []string{`box "demo-x"`, `init "supervisord"`, `"layer-supervisord"`, "fail at start"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("formatter must name %q; got %q", want, msg)
		}
	}
	if strings.HasSuffix(msg, "\n") {
		t.Fatalf("the formatter must NOT carry the trailing newline (the printer owns the line break); got %q", msg)
	}

	f, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatalf("temp: %v", err)
	}
	orig := os.Stderr
	os.Stderr = f
	defaultUnsatisfiedInitDepends("demo-x", "supervisord", "layer-supervisord")
	os.Stderr = orig
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	printed, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := "warning: " + msg + "\n"
	if string(printed) != want {
		t.Fatalf("the default reporter must print exactly the formatter's text.\n want: %q\n  got: %q", want, string(printed))
	}
}
