package deploykit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestDataDecodesTheOpaqueBodyAndRejectsACorruptOne is the data consumer test, the third on this
// pattern: well-formed bytes decode; EMPTY bytes mean "no data seeding" and stay nil; bytes that are
// not a []CandyData MUST fail, because a silently empty list means a stage copies no data files and
// nothing says why.
func TestDataDecodesTheOpaqueBodyAndRejectsACorruptOne(t *testing.T) {
	t.Run("well-formed bytes decode", func(t *testing.T) {
		got := NewSpecCandyModel(spec.CandyModel{
			Data: spec.RawBody(`[{"volume":"workspace","src":"/seed","dest":"/data"}]`),
		}, spec.CandyView{}).Data()
		if len(got) != 1 {
			t.Fatalf("Data() returned %d entries, want 1", len(got))
		}
		if got[0].Volume != "workspace" || got[0].Dest != "/data" {
			t.Fatalf("entry = %+v, want the authored values", got[0])
		}
	})

	t.Run("empty bytes are no data", func(t *testing.T) {
		if got := NewSpecCandyModel(spec.CandyModel{}, spec.CandyView{}).Data(); got != nil {
			t.Fatalf("Data() = %+v for empty bytes, want nil", got)
		}
	})

	t.Run("corrupt bytes fail loud", func(t *testing.T) {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("Data() returned normally on corrupt bytes — a stage would silently copy nothing")
			}
			msg, ok := r.(string)
			if !ok {
				t.Fatalf("recovered %v, want a string panic message", r)
			}
			if !strings.Contains(msg, "data") || !strings.Contains(msg, "does not decode") {
				t.Fatalf("recovered %q, want a message naming the corrupt data body", msg)
			}
		}()
		_ = NewSpecCandyModel(spec.CandyModel{Data: spec.RawBody(`not json`)}, spec.CandyView{}).Data()
	})
}
