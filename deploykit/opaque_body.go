package deploykit

import (
	"github.com/opencharly/sdk/rawbody"
	"github.com/opencharly/spec/spec"
)

// The crossing lives in sdk/rawbody so loaderkit and deploykit share ONE implementation.
func decodeOpaque[T any](body spec.RawBody, what string) []T { return rawbody.Decode[T](body, what) }
func decodeOpaqueOne[T any](body spec.RawBody, what string) *T {
	return rawbody.DecodeOne[T](body, what)
}
