package deploykit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestExtractDecodesTheOpaqueBodyAndRejectsACorruptOne pins the extract half of the byte crossing,
// the twin of TestHooksDecodesTheOpaqueBodyAndRejectsACorruptOne.
//
// The IR carries a candy's extractions as OPAQUE BYTES (DESIGN.md 3.1, I1/D-SCH-7), so the adapter
// is where the build engine turns them back into typed entries. Well-formed bytes decode; EMPTY
// bytes mean "no extractions" and stay nil; bytes that are not a []CandyExtract must FAIL, because a
// silently empty list means a stage produces no files and nothing says why.
func TestExtractDecodesTheOpaqueBodyAndRejectsACorruptOne(t *testing.T) {
	t.Run("well-formed bytes decode", func(t *testing.T) {
		got := NewSpecCandyModel(spec.CandyModel{
			Extract: spec.RawBody(`[{"source":"builder","path":"/out/x","dest":"/usr/x"}]`),
		}, spec.CandyView{}).Extract()
		if len(got) != 1 {
			t.Fatalf("Extract() returned %d entries, want 1", len(got))
		}
		if got[0].Source != "builder" || got[0].Dest != "/usr/x" {
			t.Fatalf("entry = %+v, want the authored values", got[0])
		}
	})

	t.Run("empty bytes are no extractions", func(t *testing.T) {
		if got := NewSpecCandyModel(spec.CandyModel{}, spec.CandyView{}).Extract(); got != nil {
			t.Fatalf("Extract() = %+v for empty bytes, want nil", got)
		}
	})

	t.Run("corrupt bytes fail loud", func(t *testing.T) {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("Extract() returned normally on corrupt bytes — a stage would silently copy nothing")
			}
			if msg, ok := r.(string); !ok || !(strings.Contains(msg, "extract") && strings.Contains(msg, "does not decode")) {
				t.Fatalf("recovered %v, want a message naming the corrupt extract body", r)
			}
		}()
		_ = NewSpecCandyModel(spec.CandyModel{Extract: spec.RawBody(`not json`)}, spec.CandyView{}).Extract()
	})
}
