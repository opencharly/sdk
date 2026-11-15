package deploykit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestHooksDecodesTheOpaqueBodyAndRejectsACorruptOne pins both halves of the byte crossing, and
// fails without either of them.
//
// The IR carries a candy's hook as OPAQUE BYTES (DESIGN.md 3.1, I1/D-SCH-7), so the adapter is the
// one place the build engine turns those bytes back into a typed value. Three cases matter:
// well-formed bytes decode; EMPTY bytes mean "no hook" and must stay nil; and bytes that are not a
// #CandyHook at all must FAIL rather than silently vanish, because a dropped hook means a
// pre_remove never runs and nothing says why.
func TestHooksDecodesTheOpaqueBodyAndRejectsACorruptOne(t *testing.T) {
	t.Run("well-formed bytes decode", func(t *testing.T) {
		got := NewSpecCandyModel(spec.CandyModel{Hook: spec.RawBody(`{"post_enable":"echo up"}`)}, spec.CandyView{}).Hooks()
		if got == nil {
			t.Fatal("Hooks() returned nil for well-formed bytes")
		}
		if got.PostEnable != "echo up" {
			t.Fatalf("PostEnable = %q, want %q", got.PostEnable, "echo up")
		}
	})

	t.Run("empty bytes are no hook", func(t *testing.T) {
		if got := NewSpecCandyModel(spec.CandyModel{}, spec.CandyView{}).Hooks(); got != nil {
			t.Fatalf("Hooks() = %+v for empty bytes, want nil", got)
		}
	})

	t.Run("corrupt bytes fail loud", func(t *testing.T) {
		// A body that is not JSON must not be read as "no hook": the bytes come from marshalling a
		// typed #CandyHook, so anything else is an invariant violation.
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("Hooks() returned normally on corrupt bytes — a candy's hooks would vanish silently")
			}
			if msg, ok := r.(string); !ok || !strings.Contains(msg, "hook") && strings.Contains(msg, "does not decode") {
				t.Fatalf("recovered %v, want a message naming the corrupt hook body", r)
			}
		}()
		_ = NewSpecCandyModel(spec.CandyModel{Hook: spec.RawBody(`not json at all`)}, spec.CandyView{}).Hooks()
	})
}
