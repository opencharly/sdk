package deploykit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// preempt_effective_test.go (relocated from charly/preempt_schema_test.go's
// TestPreemptibleConfig_UnmarshalYAML, #55 K3 Cone 4): proves PreemptEffectiveStop /
// PreemptEffectiveRestore resolve their defaults + explicit overrides against a literal
// *spec.PreemptibleConfig fixture — no charly loader/CUE-decode needed, since the functions
// under test only read the already-decoded struct's fields.

func TestPreemptEffectiveStop_Default(t *testing.T) {
	p := &spec.PreemptibleConfig{Holds: []string{"gpu", "tpu"}}
	if got := PreemptEffectiveStop(p); got != PreemptStopShutdown {
		t.Errorf("default stop = %q, want %q", got, PreemptStopShutdown)
	}
}

func TestPreemptEffectiveRestore_Default(t *testing.T) {
	p := &spec.PreemptibleConfig{Holds: []string{"gpu", "tpu"}}
	if got := PreemptEffectiveRestore(p); got != PreemptRestoreAlways {
		t.Errorf("default restore = %q, want %q", got, PreemptRestoreAlways)
	}
}

func TestPreemptEffectiveRestore_ExplicitOverride(t *testing.T) {
	p := &spec.PreemptibleConfig{Holds: []string{"gpu"}, Stop: "shutdown", Restore: "on-success"}
	if got := PreemptEffectiveRestore(p); got != spec.PreemptRestoreSuccess {
		t.Errorf("restore = %q, want %q", got, spec.PreemptRestoreSuccess)
	}
}

func TestPreemptEffectiveStop_NilConfig(t *testing.T) {
	if got := PreemptEffectiveStop(nil); got != PreemptStopShutdown {
		t.Errorf("nil config stop = %q, want %q", got, PreemptStopShutdown)
	}
	if got := PreemptEffectiveRestore(nil); got != PreemptRestoreAlways {
		t.Errorf("nil config restore = %q, want %q", got, PreemptRestoreAlways)
	}
}
