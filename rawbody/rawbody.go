// Package rawbody is the ONE crossing between authored shapes and the IR's opaque bodies.
//
// DESIGN.md §3.1 makes each plugin own its candy body, and I1/D-SCH-7 says the contract carries
// bytes rather than importing those shapes. That leaves exactly two operations at every seam — write
// the bytes, and read them back — and they live HERE rather than once per consumer because sdk's two
// consumers (`loaderkit` and `deploykit`) do not import each other: without a shared package each
// would carry its own copy, and the copies drift. That is not hypothetical: the first two copies of
// this crossing disagreed within one PR — one guarded on `!= nil` and one on `len() > 0`, and the
// `!= nil` one answered the wrong question for a slice.
package rawbody

import (
	"encoding/json"
	"fmt"

	"github.com/opencharly/spec/spec"
)

// Encode marshals an authored value into an IR body.
//
// A typed value always marshals, so a failure is an invariant violation rather than a recoverable
// condition: silently returning an empty body would reach the consumer as "the candy declared
// nothing" — a hook that never runs, a stage that copies no files — with nothing saying why. It
// panics at the point the invariant breaks.
func Encode[T any](v T, what string) spec.RawBody {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("rawbody: marshal %s: %v", what, err))
	}
	return b
}

// HasSlice reports whether a SLICE field should produce a body at all.
//
// `len() > 0`, NEVER `!= nil`: an empty non-nil slice marshals to "[]" — two non-empty bytes — so a
// `len(body) > 0` predicate downstream would answer true for a value that declares nothing. The
// guard has to ask the same question the predicate asks.
func HasSlice[T any](v []T) bool { return len(v) > 0 }

// HasPtr reports whether a POINTER field should produce a body. A pointer's only "absent" is nil;
// `len()` does not apply. Two functions rather than one because `[]T` does not accept a `*T`.
func HasPtr[T any](v *T) bool { return v != nil }

// Decode reads a body that holds a LIST into typed entries. Empty bytes mean the candy declared
// nothing and yield nil; bytes that do not decode panic, because the producer marshals a typed
// value and anything else came from somewhere else.
func Decode[T any](body spec.RawBody, what string) []T {
	if len(body) == 0 {
		return nil
	}
	var out []T
	if err := json.Unmarshal(body, &out); err != nil {
		panic(fmt.Sprintf("rawbody: the IR %s body does not decode: %v", what, err))
	}
	return out
}

// DecodeOne reads a body that holds a SINGLE object.
func DecodeOne[T any](body spec.RawBody, what string) *T {
	if len(body) == 0 {
		return nil
	}
	var out T
	if err := json.Unmarshal(body, &out); err != nil {
		panic(fmt.Sprintf("rawbody: the IR %s body does not decode: %v", what, err))
	}
	return &out
}
