package loaderkit

import (
	"github.com/opencharly/sdk/deploykit"
	"github.com/opencharly/spec/spec"
)

// newLoaderTestCandy wraps an empty CandyModel+CandyView into a spec.CandyReader fixture,
// stamping name onto both. Shared by the loaderkit tests that need a bare CandyReader.
func newLoaderTestCandy(name string, v spec.CandyView) spec.CandyReader {
	v.Name = name
	return deploykit.NewSpecCandyModel(spec.CandyModel{Name: name}, v)
}
