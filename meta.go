package sdk

import (
	"context"
	"io/fs"

	pb "github.com/opencharly/spec/proto"
)

// meta.go carries the ONE shared PluginMetaServer implementation. Before it, every
// plugin hand-rolled an identical `type meta struct{ pb.UnimplementedPluginMetaServer }`
// plus a Describe body that just forwarded to BuildCapabilities — ~58 byte-identical
// copies. NewMeta collapses them into one (R3): a plugin's meta constructor becomes a
// single `sdk.NewMeta(calver, caps, schemaFS)` call carrying only the genuinely
// per-plugin data (its CalVer, its provided capabilities, its embedded schema FS).

// fixedMeta is the shared PluginMeta implementation NewMeta returns. Its Describe
// advertises the plugin's capabilities + its self-contained CUE schema via
// BuildCapabilities (schema dir fixed to "schema" by convention), compiling the
// schema standalone and failing loudly before serving if it is broken/empty.
type fixedMeta struct {
	pb.UnimplementedPluginMetaServer
	calver   string
	caps     []ProvidedCapability
	requires []Requirement
	schemaFS fs.FS
}

func (m *fixedMeta) Describe(context.Context, *pb.Empty) (*pb.Capabilities, error) {
	return BuildCapabilitiesWithRequires(m.calver, m.caps, m.requires, m.schemaFS, "schema")
}

// NewMeta returns the shared PluginMetaServer for a plugin: its Describe reply carries
// caps + the CUE schema embedded at schemaFS's "schema" dir. It replaces the ~58
// hand-rolled meta types + Describe bodies (R3) — a plugin's NewMeta() is now just
// `return sdk.NewMeta(calver, caps, schemaFS)`.
func NewMeta(calver string, caps []ProvidedCapability, schemaFS fs.FS) pb.PluginMetaServer {
	return NewMetaWithRequires(calver, caps, nil, schemaFS)
}

// NewMetaWithRequires is NewMeta plus the plugin's declared inter-plugin dependencies
// (the wire Capabilities.requires field), so a plugin that depends on peers advertises
// them over Describe for the host to resolve + connect declaratively. A plugin with no
// dependencies calls NewMeta, which forwards nil — byte-identical to before.
func NewMetaWithRequires(calver string, caps []ProvidedCapability, requires []Requirement, schemaFS fs.FS) pb.PluginMetaServer {
	return &fixedMeta{calver: calver, caps: caps, requires: requires, schemaFS: schemaFS}
}
