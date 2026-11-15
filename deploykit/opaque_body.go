package deploykit

import (
	"encoding/json"
	"fmt"

	"github.com/opencharly/spec/spec"
)

// decodeOpaque turns an IR opaque-body SLICE into typed entries, and decodeOpaqueOne does the same
// for a body that holds a single object.
//
// The candy IR carries authored shapes as BYTES rather than importing them (DESIGN.md §3.1,
// I1/D-SCH-7), so the adapter is where the build engine turns them back into typed values. Two
// cases, one crossing: EMPTY bytes mean "the candy declared nothing" and stay nil, and bytes that do
// not decode are an invariant violation — the producer marshals a typed value, so anything else
// means the body came from somewhere else. Returning nil there would silently drop a candy's hooks
// or its file extractions, which is silent absence the operator cannot diagnose.
//
// Both live here because two hand-written copies of this crossing immediately drifted: one guarded
// on `!= nil` and one on `len() > 0`, and the first answered a different question than the
// predicate it fed.
func decodeOpaque[T any](body spec.RawBody, what string) []T {
	if len(body) == 0 {
		return nil
	}
	var out []T
	if err := json.Unmarshal(body, &out); err != nil {
		panic(fmt.Sprintf("deploykit: the IR %s body does not decode: %v", what, err))
	}
	return out
}

func decodeOpaqueOne[T any](body spec.RawBody, what string) *T {
	if len(body) == 0 {
		return nil
	}
	var out T
	if err := json.Unmarshal(body, &out); err != nil {
		panic(fmt.Sprintf("deploykit: the IR %s body does not decode: %v", what, err))
	}
	return &out
}
