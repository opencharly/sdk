package deploykit

// compiler_deps.go — helper types/vars/aliases the deploy-plan compiler needs, moved
// from charly with install_build.go in P4.

import (
	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
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
// functions that BUILD it stay in charly; the compiler only reads it.
type BuilderPreresolved struct {
	Context map[string]any
	Reverse []ReverseOp
}

// ShellAllowlist enumerates valid per-shell sub-block keys inside `shell:`.
var ShellAllowlist = map[string]bool{"bash": true, "zsh": true, "fish": true, "sh": true}

// ExecContext + Ctx consts — the op-context classification enum, moved from charly with
// the compiler.
type ExecContext string

const (
	CtxBuild   ExecContext = "build"
	CtxDeploy  ExecContext = "deploy"
	CtxRuntime ExecContext = "runtime"
)

// OpInContext reports whether an op runs in the given exec context. Its fallback
// consults the kernel VerbCatalog (charly), so charly injects the impl at init.
var OpInContext func(op *Op, ctx ExecContext) bool

// BuilderCtxKey keys the per-(candy,builder) pre-resolved builder context.
func BuilderCtxKey(candy, builder string) string { return candy + "\x00" + builder }
