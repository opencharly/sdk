package deploykit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestRenderDnfConfWrite covers the dnf.conf bootstrap fragment (relocated from
// charly/generate_speedup_test.go, K3 cone2 test closure): RenderDnfConfWrite is
// a pure function of *spec.Dnf with no charly-core dependency.
func TestRenderDnfConfWrite(t *testing.T) {
	if got := RenderDnfConfWrite(nil); got != "" {
		t.Errorf("nil Dnf should render empty, got %q", got)
	}
	if got := RenderDnfConfWrite(&spec.Dnf{}); got != "" {
		t.Errorf("zero Dnf should render empty, got %q", got)
	}
	got := RenderDnfConfWrite(&spec.Dnf{MaxParallelDownloads: 10, Fastestmirror: true})
	for _, want := range []string{"max_parallel_downloads=10", "fastestmirror=True", ">> /etc/dnf/dnf.conf", "&& \\"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered dnf.conf fragment missing %q, got: %q", want, got)
		}
	}
	// Only one knob set → only that line.
	onlyParallel := RenderDnfConfWrite(&spec.Dnf{MaxParallelDownloads: 5})
	if strings.Contains(onlyParallel, "fastestmirror") {
		t.Errorf("fastestmirror should be absent when unset, got %q", onlyParallel)
	}
}
