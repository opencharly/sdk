package loaderkit

import (
	"encoding/json"
	"fmt"

	"github.com/opencharly/spec/spec"
)

// marshalOpaque turns an authored shape into the IR's opaque body.
//
// The candy IR carries an authored shape as BYTES rather than importing it (DESIGN.md §3.1,
// I1/D-SCH-7), so every field that crosses the seam does the same thing: marshal a typed value and
// PANIC if it cannot. A typed value always marshals, so a failure is an invariant violation rather
// than a recoverable condition — and a silently dropped body would reach the consumer as an
// unexplained decode failure, or not at all (a candy's hook that never runs, a stage that copies no
// files), with nothing saying why.
//
// It lives in ONE place because the alternative is what this PR was blocked on: two copies of the
// same three-line crossing, which immediately drifted — one guarded on `!= nil` (a slice is never
// "nil enough" for that question) and one on `len() > 0`.
func marshalOpaque[T any](v T, what string) spec.RawBody {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("loaderkit: marshal %s: %v", what, err))
	}
	return b
}

// hasOpaqueSlice and hasOpaquePtr report whether an authored field should produce a body at all.
//
// They exist so no caller has to re-derive the question, and they are TWO functions because the
// question genuinely differs by shape: a SLICE asks `len() > 0` (an empty non-nil slice marshals to
// "[]", two non-empty bytes, which would make a `len(body) > 0` predicate answer true for a value
// declaring nothing), while a POINTER asks `!= nil` (`len()` does not apply, and a nil pointer is
// the only "absent" a pointer has). One generic signature cannot cover both — `[]T` does not accept
// a `*T` — which is exactly why the hand-written copies drifted.
func hasOpaqueSlice[T any](v []T) bool { return len(v) > 0 }

func hasOpaquePtr[T any](v *T) bool { return v != nil }
