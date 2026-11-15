package loaderkit

import (
	"github.com/opencharly/sdk/rawbody"
	"github.com/opencharly/spec/spec"
)

// The crossing lives in sdk/rawbody so loaderkit and deploykit share ONE implementation.
func marshalOpaque[T any](v T, what string) spec.RawBody { return rawbody.Encode(v, what) }
func hasOpaqueSlice[T any](v []T) bool                   { return rawbody.HasSlice(v) }
func hasOpaquePtr[T any](v *T) bool                      { return rawbody.HasPtr(v) }
