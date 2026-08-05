package loaderkit

// doc_parser.go — the ONE spec.DocParser ADAPTER over the free-function ParseDoc mechanism below.
//
// spec.DocParser is a single-method interface (ParseDoc), and every CUE entry point that walks a
// node-form document (ValidateCandyManifestCUE / ValidateNodeFormSteps / Walk / RunDiscover) takes
// one. The mechanism itself is the package-level loaderkit.ParseDoc free function; the interface
// exists only so an ALTERNATIVE loader plugin can serve a different config front-end.
//
// Before this file, the only adapter binding the two was candy/plugin-loader's own
// `func (*provider) ParseDoc(...)` forward — so a SECOND consumer that needed a parser (the
// `charly box validate` CUE-conformance rules, which folded into candy/plugin-box in K-wave 2 cone
// R1) had no way to reach the default parse without hand-rolling a duplicate adapter in its own
// module. This is that adapter, exported ONCE beside the mechanism it wraps (R3): candy/plugin-loader
// EMBEDS it (its former hand-written forward is deleted) and candy/plugin-box uses it by value.
//
// Zero-size, stateless, and safe to construct per call.

import (
	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// DocParser is the default charly node-form parse as a spec.DocParser value.
type DocParser struct{}

// ParseDoc implements spec.DocParser by delegating to the ONE copy of the parse mechanism.
func (DocParser) ParseDoc(doc *yaml.Node, t spec.Threaded) (map[string]*yaml.Node, spec.ParsedProject, error) {
	return ParseDoc(doc, t)
}

// Compile-time proof the adapter satisfies the seam (both placements depend on it).
var _ spec.DocParser = DocParser{}
