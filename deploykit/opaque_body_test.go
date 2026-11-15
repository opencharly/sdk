package deploykit

import (
	"reflect"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// isNilAny reports whether v is nil OR an interface holding a nil pointer/slice/map. The plain
// `v == nil` comparison cannot see through the interface box, which is why a correct decode would
// otherwise fail its own test.
func isNilAny(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	kind := rv.Kind()
	nillable := kind == reflect.Pointer || kind == reflect.Slice || kind == reflect.Map || kind == reflect.Interface
	return nillable && rv.IsNil()
}

// TestOpaqueBodiesDecodeAndRejectCorruptInput covers EVERY field that crosses the IR as opaque bytes,
// in ONE table rather than one near-identical function per field — the same "one implementation per
// behavior" rule the change itself follows, and the reason `dupl` stopped firing.
//
// Each row asserts the three outcomes that matter, on the authored values:
//   - well-formed bytes decode to the values that were marshalled (via the row's own `check`);
//   - EMPTY bytes mean "the candy declared nothing" and are nil — not `[]`, not a zero value;
//   - bytes that are not the authored shape PANIC with a message saying the body does not decode,
//     because a silently empty list means a hook never runs or a stage copies no files, with
//     nothing saying why.
func TestOpaqueBodiesDecodeAndRejectCorruptInput(t *testing.T) {
	cases := []struct {
		name    string
		body    spec.RawBody
		decode  func(spec.RawBody) any
		check   func(*testing.T, any)
		badJSON spec.RawBody
	}{
		{
			name:    "hook (decodeOpaqueOne)",
			body:    spec.RawBody(`{"post_enable":"echo up","pre_remove":"echo down"}`),
			decode:  func(b spec.RawBody) any { return decodeOpaqueOne[HooksConfig](b, "hook") },
			badJSON: spec.RawBody(`not json at all`),
			check: func(t *testing.T, got any) {
				h, ok := got.(*HooksConfig)
				if !ok || h == nil {
					t.Fatalf("decoded %T, want *HooksConfig", got)
				}
				if h.PostEnable != "echo up" || h.PreRemove != "echo down" {
					t.Fatalf("decoded %+v, want the authored hook values", h)
				}
			},
		},
		{
			name:    "extract (decodeOpaque)",
			body:    spec.RawBody(`[{"source":"builder","path":"/out/x","dest":"/usr/x"}]`),
			decode:  func(b spec.RawBody) any { return decodeOpaque[ExtractYAML](b, "extract") },
			badJSON: spec.RawBody(`not json`),
			check: func(t *testing.T, got any) {
				e, ok := got.([]ExtractYAML)
				if !ok || len(e) != 1 {
					t.Fatalf("decoded %T of len %d, want one ExtractYAML", got, len(e))
				}
				if e[0].Source != "builder" || e[0].Path != "/out/x" || e[0].Dest != "/usr/x" {
					t.Fatalf("decoded %+v, want the authored extract values", e[0])
				}
			},
		},
		{
			name:    "service (decodeOpaque)",
			body:    spec.RawBody(`[{"name":"web","exec":"/usr/bin/web"}]`),
			decode:  func(b spec.RawBody) any { return decodeOpaque[ServiceEntry](b, "service") },
			badJSON: spec.RawBody(`not json`),
			check: func(t *testing.T, got any) {
				svc, ok := got.([]ServiceEntry)
				if !ok || len(svc) != 1 {
					t.Fatalf("decoded %T of len %d, want one ServiceEntry", got, len(svc))
				}
				if svc[0].Name != "web" || svc[0].Exec != "/usr/bin/web" {
					t.Fatalf("decoded %+v, want the authored service values", svc[0])
				}
			},
		},
		{
			name:    "artifact (decodeOpaque)",
			body:    spec.RawBody(`[{"name":"kubeconfig","path":"/etc/k3s/k3s.yaml"}]`),
			decode:  func(b spec.RawBody) any { return decodeOpaque[CandyArtifact](b, "artifact") },
			badJSON: spec.RawBody(`not json`),
			check: func(t *testing.T, got any) {
				arts, ok := got.([]CandyArtifact)
				if !ok || len(arts) != 1 {
					t.Fatalf("decoded %T of len %d, want one CandyArtifact", got, len(arts))
				}
				if arts[0].Name != "kubeconfig" || arts[0].Path != "/etc/k3s/k3s.yaml" {
					t.Fatalf("decoded %+v, want the authored artifact values", arts[0])
				}
			},
		},
		{
			name:    "data (decodeOpaque)",
			body:    spec.RawBody(`[{"volume":"workspace","src":"/seed","dest":"/data"}]`),
			decode:  func(b spec.RawBody) any { return decodeOpaque[DataYAML](b, "data") },
			badJSON: spec.RawBody(`not json`),
			check: func(t *testing.T, got any) {
				d, ok := got.([]DataYAML)
				if !ok || len(d) != 1 {
					t.Fatalf("decoded %T of len %d, want one DataYAML", got, len(d))
				}
				if d[0].Volume != "workspace" || d[0].Src != "/seed" || d[0].Dest != "/data" {
					t.Fatalf("decoded %+v, want the authored data values", d[0])
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name+" decodes to the authored values", func(t *testing.T) {
			tc.check(t, tc.decode(tc.body))
		})

		t.Run(tc.name+" empty is nil", func(t *testing.T) {
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
