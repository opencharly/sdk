package sdk

// climodel.go — re-export of BuildCLIModel (relocated to
// github.com/opencharly/spec/clireflect, #55 import-purity). The Kong→CLIModel
// reflection mechanism is a shared fabric primitive used by charly core (the host
// CLI) and every command plugin; it lives in the kong-bearing spec/clireflect slice
// — NOT the cuelang-bearing spec/climodel — so that consumers needing only CLI
// reflection never pull cuelang, and consumers needing only CUE validation never
// pull kong (Rule 2: each heavy third-party dep lives only in the slice that needs
// it). Re-exported here so candy call sites (candy/plugin-agent's 3 BuildCLIModel
// call sites) compile UNCHANGED; charly core re-points to
// spec/clireflect.BuildCLIModel directly (the step-1 import-purity gate). The
// unexported leaf/arg helpers that lived here had no external callers (verified
// across the whole tree) and are not re-exported — spec/clireflect owns the single
// implementation.
import "github.com/opencharly/spec/clireflect"

// BuildCLIModel reflects a Kong command tree into the generated #CLIModel.
// Prefix is a dotted command path prepended to every leaf (for example "agent" when
// a command plugin reflects only its owned subtree).
// Relocated to spec/clireflect; re-exported here so candy call sites compile UNCHANGED.
var BuildCLIModel = clireflect.BuildCLIModel
