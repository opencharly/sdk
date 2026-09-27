package deploykit

import "github.com/opencharly/spec/spec"

// newTestCandy wraps a CandyModel into a spec.CandyReader fixture, stamping name onto both
// views (mirrors charly's own candy_test_helpers_test.go:testCandy). Shared by the deploykit
// tests that need a bare CandyReader (shell-dropin compile, apk format, loud-failure steps).
func newTestCandy(name string, m spec.CandyModel) spec.CandyReader {
	m.Name = name
	return NewSpecCandyModel(m, spec.CandyView{Name: name})
}
