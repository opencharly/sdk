package deploykit

// compiler_deps.go — helper types/vars/aliases the deploy-plan compiler needs, moved
// from charly with install_build.go in P4.

import (
	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/vmshared"
	"github.com/opencharly/spec/spec"
)

var (
	ExpandPath         = kit.ExpandPath
	shellQuote         = spec.ShellQuote
	KwRun              = kit.KwRun
	extractStringSlice = ExtractStringSlice
	toMapSlice         = buildkit.ToMapSlice
)

type ShellSpec = vmshared.ShellSpec

// BuilderPreresolved is the pre-resolved builder-context payload — the TYPE now lives in
// spec (#55 value-type consolidation; a plain in-process value carrier, not a wire type:
// candy/plugin-fleet's preresolveBuilderContexts builds it plugin-side over
// exec.InvokeProvider and the pure compiler only reads it, so it never crosses the process
// boundary — HostContext.BuilderContext is populated in-proc AFTER the HostContextJSON
// decode). This forwarder keeps deploykit's callers + candy/plugin-fleet compiling
// unchanged. The externalized-builder WORD SET itself needs no new sharing mechanism — it
// rides the wire as spec.ResolvedProject.ExternalizedBuilders.
type BuilderPreresolved = spec.BuilderPreresolved

// HostContext is the deploy-compile host-context value carrier — the TYPE now lives in spec
// (spec.HostContext, #55 K4 import-purity; a hand-written spec value type, NOT a CUE wire type:
// it crosses ONLY as the opaque `host_context: bytes` RawBody, and its BuilderContext/ActiveInit/
// ActiveInitName are json:"-" in-process fields the plugin populates after decode — the gengotypes
// spike cannot express that json:"-" intent, so hand-written is SPIKE-JUSTIFIED). This forwarder
// keeps the compile mechanism (BuildDeployPlan + the compile helpers) + candy/plugin-fleet
// compiling unchanged; charly core constructs spec.HostContext directly.
type HostContext = spec.HostContext

// ShellAllowlist enumerates valid per-shell sub-block keys inside `shell:`.
var ShellAllowlist = map[string]bool{"bash": true, "zsh": true, "fish": true, "sh": true}

// OpInContext (the op-context DI seam charly injects at init) now lives in spec
// (spec.OpInContext, #55 import-purity cone-render) so charly injects + the fabric
// libraries read ONE canonical var without a deploykit import. Consumers reference
// spec.OpInContext directly.

// BuilderCtxKey keys the per-(candy,builder) pre-resolved builder context.
func BuilderCtxKey(candy, builder string) string { return candy + "\x00" + builder }
