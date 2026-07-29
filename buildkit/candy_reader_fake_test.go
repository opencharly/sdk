package buildkit

import "github.com/opencharly/spec/spec"

// candy_reader_fake_test.go — a minimal spec.CandyReader test double shared by every
// buildkit test that needs to hand a candy to status.go / capabilities.go / init_config.go.
//
// Every spec.CandyReader double already in this repo (deploykit's specCandyAdapter behind
// NewSpecCandyModel, and the deploykit/loaderkit test-file helpers built on top of it —
// deploy_host_helpers_test.go's testArtifactCandy, graph_shim_relocated_test.go's testCandy,
// spec_candy_adapter_test.go, loaderkit/scan_candy_test.go) lives in a package buildkit
// cannot import: deploykit already imports buildkit (buildkit_aliases.go), so
// buildkit -> deploykit would be a cycle, and loaderkit sits on the same side of that line.
// buildkit therefore needs its own double.
//
// Rather than hand-duplicate all ~65 CandyReader accessors those adapters carry (R3), this
// EMBEDS the spec.CandyReader interface itself (nil by default), which makes *fakeCandyReader
// satisfy the full interface for free, and overrides ONLY the handful of accessors buildkit's
// own functions under test actually read (Capabilities/RequiresCapabilities/GetStatus/HasInit/
// RelayPorts/GetName). Any other accessor would panic on the nil embedded interface if a test
// ever called it through this double — which is the correct failure: it means the double needs
// a new override, not a silent zero value.
type fakeCandyReader struct {
	spec.CandyReader

	name         string
	status       string
	caps         *spec.CandyCapability
	requiresCaps []string
	hasInit      map[string]bool
	relayPorts   []int
}

func (f *fakeCandyReader) GetName() string                     { return f.name }
func (f *fakeCandyReader) GetStatus() string                   { return f.status }
func (f *fakeCandyReader) Capabilities() *spec.CandyCapability { return f.caps }
func (f *fakeCandyReader) RequiresCapabilities() []string      { return f.requiresCaps }
func (f *fakeCandyReader) HasInit(initName string) bool        { return f.hasInit[initName] }
func (f *fakeCandyReader) RelayPorts() []int                   { return f.relayPorts }

var _ spec.CandyReader = (*fakeCandyReader)(nil)
