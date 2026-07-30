package loaderkit

import "github.com/opencharly/spec/spec"

// resolve_opts.go — ResolveOpts (the loader scan/load OPTIONS) is DEFINED in the dedicated spec
// module (spec.ResolveOpts, #55 loader cascade) so the ~14 charly-core call sites that only NAME the
// options struct reach it through spec and drop their loaderkit import. Its fields are native spec
// build-vocabulary configs (*spec.InitConfig / *spec.DistroConfig / *spec.BuilderConfig) carrying
// their own resolve methods (spec init_config_methods.go / distro_config_methods.go) — spec owns the
// types AND the mechanism methods since #72, so ResolveOpts pulls in no buildkit dependency. This
// package-local alias keeps loaderkit's own resolve callers (resolve_project.go / finalize_candy.go)
// terse. DISTINCT from buildkit.ResolveOpts (the build-resolve options): this is the SCAN/LOAD
// options; the buildkit resolvers never read ExtraCandyRefs/InitCfg/RequestedBoxes.
type ResolveOpts = spec.ResolveOpts
