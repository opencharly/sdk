package deploykit

// compiler_deps.go — helper types/vars/aliases the deploy-plan compiler needs, moved
// from charly with install_build.go in P4.

import (
	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
	"github.com/opencharly/sdk/vmshared"
)

var (
	ExpandPath         = kit.ExpandPath
	shellQuote         = kit.ShellQuote
	KwRun              = kit.KwRun
	extractStringSlice = ExtractStringSlice
	toMapSlice         = buildkit.ToMapSlice
)

type ShellSpec = vmshared.ShellSpec

// BuilderPreresolved is one candy×builder's pre-resolved payload. The Candy-coupled
// functions that BUILD it (FLOOR-SLIM-proper Unit-8: candy/plugin-bundle's own
// preresolveBuilderContexts, over exec.InvokeProvider — the CONNECT step alone stays
// charly-core, which owns loadProjectPlugins/ScanAllCandyWithConfigOpts) build it; the
// compiler only reads it. The externalized-builder WORD SET itself needs no new sharing
// mechanism — it already rides the wire as spec.ResolvedProject.ExternalizedBuilders (the
// resolved-project envelope every "resolved-project" HostBuild caller, incl.
// candy/plugin-bundle's compile.go, already re-hydrates).
type BuilderPreresolved struct {
	Context map[string]any
	Reverse []ReverseOp
}

// ShellAllowlist enumerates valid per-shell sub-block keys inside `shell:`.
var ShellAllowlist = map[string]bool{"bash": true, "zsh": true, "fish": true, "sh": true}

// OpInContext reports whether an op runs in the given exec context. Its fallback
// consults the kernel VerbCatalog (charly), so charly injects the impl at init.
// ExecContext (+ Ctx consts) is spec.ExecContext — a plain shared vocabulary type (K3, #39).
var OpInContext func(op *Op, ctx spec.ExecContext) bool

// BuilderCtxKey keys the per-(candy,builder) pre-resolved builder context.
func BuilderCtxKey(candy, builder string) string { return candy + "\x00" + builder }
