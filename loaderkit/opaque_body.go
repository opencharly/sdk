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

// hasOpaque reports whether an authored field should produce a body at all. It exists so no caller
// has to re-derive the question: for a SLICE the answer is `len() > 0` (an empty non-nil slice
// marshals to "[]", two non-empty bytes, which would make a `len(body) > 0` predicate answer true
// for a value that declares nothing), and for a POINTER it is `!= nil`.
func hasOpaque[T any](v []T) bool { return len(v) > 0 }
