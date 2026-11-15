package rawbody

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

type pair struct {
	Name string `json:"name"`
	Dest string `json:"dest"`
}

// TestTheCrossing pins every operation the seam has, including the GUARD RULE that was the whole
// reason this package exists: a SLICE asks len() > 0 and a POINTER asks != nil, because an empty
// non-nil slice marshals to "[]" — two non-empty bytes — which would make a downstream
// `len(body) > 0` predicate answer true for a value that declares nothing.
func TestTheCrossing(t *testing.T) {
	t.Run("Encode writes the authored JSON verbatim", func(t *testing.T) {
		b := Encode([]pair{{Name: "web", Dest: "/usr/web"}}, "pair list")
		if got := string(b); got != `[{"name":"web","dest":"/usr/web"}]` {
			t.Fatalf("Encode = %s, want the authored JSON inline", got)
		}
	})

	t.Run("Decode round-trips", func(t *testing.T) {
		got := Decode[pair](Encode([]pair{{Name: "web", Dest: "/usr/web"}}, "pair list"), "pair list")
		if len(got) != 1 || got[0].Name != "web" || got[0].Dest != "/usr/web" {
			t.Fatalf("Decode = %+v, want the authored entry", got)
		}
	})

	t.Run("DecodeOne round-trips", func(t *testing.T) {
		got := DecodeOne[pair](Encode(pair{Name: "one", Dest: "/d"}, "pair"), "pair")
		if got == nil || got.Name != "one" || got.Dest != "/d" {
			t.Fatalf("DecodeOne = %+v, want the authored value", got)
		}
	})

	t.Run("empty bodies are nil", func(t *testing.T) {
		if got := Decode[pair](nil, "pair list"); got != nil {
			t.Fatalf("Decode(nil) = %+v, want nil", got)
		}
		if got := DecodeOne[pair](nil, "pair"); got != nil {
			t.Fatalf("DecodeOne(nil) = %+v, want nil", got)
		}
	})

	t.Run("corrupt bodies panic naming the field", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			fn   func()
		}{
			{"Decode", func() { Decode[pair](spec.RawBody(`nope`), "the pair list") }},
			{"DecodeOne", func() { DecodeOne[pair](spec.RawBody(`nope`), "the pair") }},
		} {
			func() {
				defer func() {
					r := recover()
					if r == nil {
						t.Fatalf("%s returned normally on corrupt bytes", tc.name)
					}
					msg, ok := r.(string)
					if !ok || !strings.Contains(msg, "does not decode") {
						t.Fatalf("%s recovered %v, want a message saying the body does not decode", tc.name, r)
					}
				}()
				tc.fn()
			}()
		}
	})

	// THE GUARD RULE — the drift this package consolidates.
	t.Run("HasSlice: an EMPTY non-nil slice is absent", func(t *testing.T) {
		if HasSlice([]pair{}) {
			t.Fatal("HasSlice([]pair{}) = true — an empty non-nil slice would marshal to \"[]\", two non-empty bytes, and a len(body)>0 predicate would then answer true")
		}
		if HasSlice([]pair{{Name: "x"}}) != true {
			t.Fatal("HasSlice with one entry = false")
		}
		if HasSlice[pair](nil) {
			t.Fatal("HasSlice(nil) = true")
		}
	})

	t.Run("HasPtr: nil is absent, a value is present", func(t *testing.T) {
		if HasPtr[pair](nil) {
			t.Fatal("HasPtr(nil) = true")
		}
		if !HasPtr(&pair{Name: "x"}) {
			t.Fatal("HasPtr(non-nil) = false")
		}
	})
}
