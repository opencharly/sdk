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
// candy/plugin-bundle's preresolveBuilderContexts builds it plugin-side over
// exec.InvokeProvider and the pure compiler only reads it, so it never crosses the process
// boundary — HostContext.BuilderContext is populated in-proc AFTER the HostContextJSON
// decode). This forwarder keeps deploykit's callers + candy/plugin-bundle compiling
// unchanged. The externalized-builder WORD SET itself needs no new sharing mechanism — it
// rides the wire as spec.ResolvedProject.ExternalizedBuilders.
type BuilderPreresolved = spec.BuilderPreresolved

// ShellAllowlist enumerates valid per-shell sub-block keys inside `shell:`.
var ShellAllowlist = map[string]bool{"bash": true, "zsh": true, "fish": true, "sh": true}

// OpInContext reports whether an op runs in the given exec context. Its fallback
// consults the kernel VerbCatalog (charly), so charly injects the impl at init.
// ExecContext (+ Ctx consts) is spec.ExecContext — a plain shared vocabulary type (K3, #39).
var OpInContext func(op *Op, ctx spec.ExecContext) bool

// BuilderCtxKey keys the per-(candy,builder) pre-resolved builder context.
func BuilderCtxKey(candy, builder string) string { return candy + "\x00" + builder }
