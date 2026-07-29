package loaderkit

import (
	"encoding/json"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/spec/spec"
)

// vocab.go — the SHARED build-vocabulary projections (K3 build-engine, Unit 1). A project's
// `distro:`/`builder:`/`init:` build vocabulary lands in uf.PluginKinds["distro"|"builder"|"init"]
// as OPAQUE canonical bodies (the distro/builder/init plugin kinds — candy/plugin-distro etc.). These
// three functions reconstruct the name-keyed build-vocab CONFIGS (buildkit.DistroConfig/
// BuilderConfig/InitConfig) the build engine consumes.
//
// They live in loaderkit — the SINGLE home both charly core AND a genuine out-of-module plugin
// (candy/plugin-build, which runs the build-engine RESOLVE plugin-side over the K1 reverse legs)
// call — so the projection is never duplicated (R3). The distro/init bodies must be RESOLVED via a
// plugin's OpResolve leg (they are opaque post-de-type); that resolve is registry-coupled, so it
// rides in as a CALLBACK the caller supplies: charly core passes its in-proc registry invokers
// (resolveDistroViaPlugin/resolveInitConfigViaPlugin), and plugin-build passes InvokeProvider-backed
// callbacks over its reverse channel — same wrapper, either placement. The builder bodies decode
// PURELY (spec.DecodePluginKindMap), so ProjectBuilderConfig needs no callback.
//
// The loaderkit→buildkit import edge is acyclic: buildkit imports only sdk/kit (a pure leaf) — it
// imports neither loaderkit nor charly (verified).

// ProjectDistroConfig reconstructs the *buildkit.DistroConfig (distro: section) from uf, resolving
// each opaque `distro` body via the caller-supplied resolveDistro callback (the distro plugin's
// OpResolve leg). Nil when no distros are configured.
func ProjectDistroConfig(uf *spec.UnifiedFile, resolveDistro func(json.RawMessage) (*spec.ResolvedDistro, error)) *buildkit.DistroConfig {
	distros := spec.ResolvePluginKindViaPlugin(uf, "distro", resolveDistro)
	if len(distros) == 0 {
		return nil
	}
	return &buildkit.DistroConfig{Distro: distros}
}

// ProjectBuilderConfig reconstructs the *buildkit.BuilderConfig (builder: section) from uf. The
// builder bodies decode purely (spec.DecodePluginKindMap) — no OpResolve callback needed. Nil when no
// builders are configured.
func ProjectBuilderConfig(uf *spec.UnifiedFile) *buildkit.BuilderConfig {
	builders := spec.DecodePluginKindMap[buildkit.BuilderDef](uf, "builder")
	if len(builders) == 0 {
		return nil
	}
	return &buildkit.BuilderConfig{Builder: builders}
}

// ProjectInitConfig reconstructs the *buildkit.InitConfig (init: section) from uf, resolving each
// opaque `init` body via the caller-supplied resolveInit callback (the init plugin's OpResolve
// config leg). Nil when no init systems are configured.
func ProjectInitConfig(uf *spec.UnifiedFile, resolveInit func(json.RawMessage) (*spec.ResolvedInit, error)) *buildkit.InitConfig {
	inits := spec.ResolvePluginKindViaPlugin(uf, "init", resolveInit)
	if len(inits) == 0 {
		return nil
	}
	return &buildkit.InitConfig{Init: inits}
}
