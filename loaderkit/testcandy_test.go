package loaderkit

import (
	"github.com/opencharly/sdk/deploykit"
	"github.com/opencharly/spec/spec"
)

// newLoaderTestCandy wraps a CandyModel+CandyView into a spec.CandyReader fixture,
// stamping name onto both. Shared by the loaderkit tests that need a bare CandyReader.
func newLoaderTestCandy(name string, m spec.CandyModel, v spec.CandyView) spec.CandyReader {
	m.Name = name
	v.Name = name
	return deploykit.NewSpecCandyModel(m, v)
}

func candyRequires(l spec.CandyReader, bare string) bool { return countCandyRequire(l, bare) > 0 }

func countCandyRequire(l spec.CandyReader, bare string) int {
	n := 0
	for _, r := range l.GetRequire() {
		if r.Bare() == bare {
			n++
		}
	}
	return n
}
