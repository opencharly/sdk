// Package charlylib is the generic, plugin-AGNOSTIC multi-call host for
// out-of-process charly plugins.
//
// A charly plugin's cmd/serve is a one-liner: sdk.Main(NewProvider(), NewMeta(),
// CliMain). Built one-per-plugin, every such binary statically links the whole
// shared SDK/spec transport closure — and because Go has no runtime shared
// library for this (buildmode=shared needs cgo + a writable GOROOT and fails
// across module roots; buildmode=plugin needs cgo and in-process loading), N
// plugin binaries store that closure N times.
//
// charlylib removes the duplication by hosting MANY plugins in ONE binary: it
// links the shared closure once and dispatches to the plugin named by the base
// name it was invoked as (argv[0]). A package installs `plugin-<word>` as a
// symlink to the single shared `charly-lib` binary, so the loader contract — a
// `.providers` word manifest beside an executable named `plugin-<word>` — is
// byte-for-byte unchanged (charly's bakedPluginDirs / discoverBakedPluginWords
// keep working with ZERO core changes).
//
// This package names NO plugin and imports NO plugin module: the registry is
// supplied by GENERATED code (charly-lib-gen) built from a data list, so any
// current or future plugin joins the shared host by being listed, not by editing
// this library. It depends only on the spec contract module (proto + transport).
package charlylib

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	pb "github.com/opencharly/spec/proto"
	"github.com/opencharly/spec/transport"
)

// Plugin is one welded plugin a shared host binary serves. Its three fields are
// exactly the arguments of a per-plugin cmd/serve's sdk.Main call.
type Plugin struct {
	// Provider constructs the plugin's pb.ProviderServer.
	Provider func() pb.ProviderServer
	// Meta constructs the plugin's pb.PluginMetaServer.
	Meta func() pb.PluginMetaServer
	// CLI runs the plugin's command-mode work with os.Args[1:]; its int return is
	// the process exit code.
	CLI func(args []string) int
}

// Registry maps a plugin's binary base name (e.g. "plugin-clean") to its Plugin.
type Registry map[string]Plugin

// pluginMain is transport.Main, indirected through a var so tests can observe
// argv[0] dispatch without starting a go-plugin server. Production never
// reassigns it.
var pluginMain = transport.Main

// Run dispatches to the plugin named by filepath.Base(os.Args[0]) and delegates
// to transport.Main, which decides serve-vs-CLI mode from the go-plugin
// handshake cookie — identical behaviour to the per-plugin binary it replaces.
//
// A base name absent from the registry is a hard error (exit 2) that lists the
// known names, so a mis-installed symlink fails loudly instead of silently doing
// nothing.
func Run(reg Registry) {
	name := filepath.Base(os.Args[0])
	p, ok := reg[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "charly-lib: invoked as %q; known plugins: %v\n", name, reg.Names())
		os.Exit(2)
	}
	pluginMain(p.Provider(), p.Meta(), p.CLI)
}

// Names returns the registered binary base names, sorted (diagnostics + tests).
func (r Registry) Names() []string {
	names := make([]string, 0, len(r))
	for n := range r {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
