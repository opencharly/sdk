package deploykit

import (
	"reflect"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestOpaqueBodiesDecodeAndRejectCorruptInput covers EVERY field that crosses the IR as opaque bytes,
// in ONE table rather than one near-identical function per field.
//
// The fields differ only in their Go type and the body they carry, so three copies of this test
// would be three copies of the same three assertions — and the linter's `dupl` check said so before a
// reviewer had to. The shared crossing (decodeOpaque / decodeOpaqueOne) is what makes one table
// possible: each row exercises the same code path with a different type parameter.
//
// Each row asserts the three outcomes that matter:
//   - well-formed bytes decode to the authored values;
//   - EMPTY bytes mean "the candy declared nothing" and stay nil;
//   - bytes that are not the authored shape PANIC with a message naming the body, because a silently
//     empty list means a hook never runs or a stage copies no files, with nothing saying why.
func TestOpaqueBodiesDecodeAndRejectCorruptInput(t *testing.T) {
	cases := []struct {
		name    string
		body    spec.RawBody
		decode  func(spec.RawBody) any
		want    any // the decoded value's first element, checked by the caller's assertion below
		badJSON spec.RawBody
	}{
		{
			name:    "hook (decodeOpaqueOne)",
			body:    spec.RawBody(`{"post_enable":"echo up"}`),
			decode:  func(b spec.RawBody) any { return decodeOpaqueOne[HooksConfig](b, "hook") },
			badJSON: spec.RawBody(`not json at all`),
		},
		{
			name:    "extract (decodeOpaque)",
			body:    spec.RawBody(`[{"source":"builder","path":"/out/x","dest":"/usr/x"}]`),
			decode:  func(b spec.RawBody) any { return decodeOpaque[ExtractYAML](b, "extract") },
			badJSON: spec.RawBody(`not json`),
		},
		{
			name:    "data (decodeOpaque)",
			body:    spec.RawBody(`[{"volume":"workspace","src":"/seed","dest":"/data"}]`),
			decode:  func(b spec.RawBody) any { return decodeOpaque[DataYAML](b, "data") },
			badJSON: spec.RawBody(`not json`),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name+" decodes", func(t *testing.T) {
			if got := tc.decode(tc.body); got == nil {
				t.Fatalf("%s returned nil for a well-formed body", tc.name)
			}
		})

		t.Run(tc.name+" empty is nil", func(t *testing.T) {
			// `got != nil` is NOT the question: a nil *HooksConfig (or a nil slice) stored in an
			// `any` is a NON-nil interface holding a nil value, so the naive check would fail while
			// the decode is perfectly correct. isNilAny asks the question that was meant.
			if got := tc.decode(nil); !isNilAny(got) {
				t.Fatalf("%s returned %+v for an empty body, want nil", tc.name, got)
			}
		})

		t.Run(tc.name+" corrupt panics", func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("%s returned normally on corrupt bytes — the body would silently vanish", tc.name)
				}
				msg, ok := r.(string)
				if !ok {
					t.Fatalf("recovered %v, want a string panic message", r)
				}
				if !strings.Contains(msg, "does not decode") {
					t.Fatalf("recovered %q, want a message saying the body does not decode", msg)
				}
			}()
			_ = tc.decode(tc.badJSON)
		})
	}
}

// isNilAny reports whether v is nil OR an interface holding a nil pointer/slice/map. The plain
// `v == nil` comparison cannot see through the interface box, which is why a correct decode would
// otherwise fail its own test.
func isNilAny(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Slice, reflect.Map, reflect.Interface:
		return rv.IsNil()
	}
	return false
}

// TestOpaqueFieldsDecodeThroughTheAdapter proves the adapter accessors — not just the raw helpers —
// hand the build engine typed values, one assertion per field that has an adapter accessor.
func TestOpaqueFieldsDecodeThroughTheAdapter(t *testing.T) {
	h := NewSpecCandyModel(spec.CandyModel{Hook: spec.RawBody(`{"pre_remove":"echo down"}`)}, spec.CandyView{})
	if h.Hooks() == nil || h.Hooks().PreRemove != "echo down" {
		t.Fatalf("Hooks() = %+v, want the decoded pre_remove", h.Hooks())
	}

	e := NewSpecCandyModel(spec.CandyModel{Extract: spec.RawBody(`[{"dest":"/usr/x"}]`)}, spec.CandyView{})
	if got := e.Extract(); len(got) != 1 || got[0].Dest != "/usr/x" {
		t.Fatalf("Extract() = %+v, want one entry dest /usr/x", got)
	}

	d := NewSpecCandyModel(spec.CandyModel{Data: spec.RawBody(`[{"dest":"/data"}]`)}, spec.CandyView{})
	if got := d.Data(); len(got) != 1 || got[0].Dest != "/data" {
		t.Fatalf("Data() = %+v, want one entry dest /data", got)
	}
}
