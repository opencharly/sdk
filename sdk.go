// Package sdk is the importable surface an out-of-tree charly plugin builds
// against. An external plugin implements the proto Provider + PluginMeta services
// (github.com/opencharly/spec/proto) and calls sdk.Serve from its main; charly
// connects to it through the SAME handshake + dispense key. The handshake/glue
// live in spec/transport (NOT in charly's package main) so both charly and an
// external plugin share ONE definition — no drift, no duplication (R3).
//
// #55 import-purity: the go-plugin serve/dispense surface (Serve/PluginMap/Conn,
// the handshake, the channel streaming types, the parent-death backstop) was
// relocated ADDITIVELY to github.com/opencharly/spec/transport by the spec leg.
// The sdk root now re-exports those symbols as thin shims so every candy call
// site compiles UNCHANGED; the definitions live once in spec/transport (the
// single source). The sdk root keeps its own SDK-facing authoring types
// (ProvidedCapability, the string-scope StepContract, SchemaValidator) that an
// out-of-tree plugin constructs.
package sdk

import (
	"github.com/opencharly/spec/transport"
)

// ProtocolVersion is the go-plugin/proto contract version — a thin secondary gate.
// CalVer (charly's version.go) is the authority; matching CalVer ⇒ matching proto.
const ProtocolVersion = transport.ProtocolVersion

// DispenseKey is the single go-plugin plugin name; charly serves/dispenses ONE
// gRPC plugin exposing the uniform Provider + PluginMeta services.
const DispenseKey = transport.DispenseKey

// Handshake is the magic-cookie handshake charly and every plugin MUST share. A
// plugin server refuses to serve unless launched with CHARLY_PLUGIN set, so a
// plugin binary run by hand prints the "not meant to be executed directly" notice
// instead of hanging.
var Handshake = transport.Handshake

// IsServeMode reports whether this process was launched by charly as a go-plugin gRPC
// SERVER (the handshake magic-cookie env is present) rather than invoked directly as a
// CLI. The single switch a dual-mode plugin's main() pivots on.
var IsServeMode = transport.IsServeMode

// Main is the dual-mode entry point a plugin's main() delegates to. In SERVE mode
// (charly launched it over go-plugin gRPC) it serves the plugin's Provider + PluginMeta
// (its verb/kind/deploy/step/builder capabilities). Otherwise the plugin was fork/exec'd
// by charly's COMMAND dispatch (or run by hand) and owns real terminal stdio/TTY: cli
// runs the command's work with os.Args[1:], its int return becoming the process exit code.
//
//	func main() { sdk.Main(&provider{}, &meta{}, cliMain) }
var Main = transport.Main

// Serve exposes a plugin's Provider + PluginMeta services over go-plugin gRPC and
// blocks serving. The serve half of Main (a verb/kind/deploy/step/builder plugin with
// no CLI mode may call it directly):
//
//	func main() { sdk.Serve(&myProvider{}, &myMeta{}) }
var Serve = transport.Serve

// PluginMap builds the go-plugin PluginSet for the dispense key. Server side passes
// the two service impls; the client side (charly connecting) passes nil,nil and
// receives a *Conn from the dispense.
var PluginMap = transport.PluginMap

// Conn is the dispensed client handle — charly's side of a connected plugin.
type Conn = transport.Conn
