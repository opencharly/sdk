package sdk

import (
	"context"
	"io/fs"

	pb "github.com/opencharly/sdk/proto"
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
	schemaFS fs.FS
}

func (m *fixedMeta) Describe(context.Context, *pb.Empty) (*pb.Capabilities, error) {
	return BuildCapabilities(m.calver, m.caps, m.schemaFS, "schema")
}

// NewMeta returns the shared PluginMetaServer for a plugin: its Describe reply carries
// caps + the CUE schema embedded at schemaFS's "schema" dir. It replaces the ~58
// hand-rolled meta types + Describe bodies (R3) — a plugin's NewMeta() is now just
// `return sdk.NewMeta(calver, caps, schemaFS)`.
func NewMeta(calver string, caps []ProvidedCapability, schemaFS fs.FS) pb.PluginMetaServer {
	return &fixedMeta{calver: calver, caps: caps, schemaFS: schemaFS}
}
