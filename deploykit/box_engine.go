package deploykit

import "github.com/opencharly/spec/spec"

// box_engine.go — the two per-deploy engine-resolution functions with NO project-loader
// dependency (K4: relocated from the DELETED charly/engine.go). Homed here (not kit) because
// ResolveBoxEngineForDeploy needs LoadDeployConfigForRead, a deploykit-only mechanism (kit cannot
// import deploykit — that would cycle). ResolveBoxEngine (the *Config/*Candy/LoadConfig-bound
// variant) lives in sdk/deploykit/box_build_resolve.go; ResolveBoxEngineFromDir/ImageRuntime have
// no live definition. The consumers of ResolveBoxEngineForDeploy are all candies:
// candy/plugin-deploy-pod/resolve.go, candy/plugin-pod/{pod_cmd,service_resolve,
// remove_orchestration}.go, candy/plugin-preempt/holder_dispatch.go, and
// candy/plugin-substrate/{status_flat,status_android_collect}.go (CHECK-wave container-resolve
// dedup; corrected 2026-08-06 — the former charly-core caller list named deleted files).

// ResolveBoxEngineForDeploy resolves the run engine from the per-host deploy config,
// falling back to globalEngine. No charly.yml (project) dependency.
func ResolveBoxEngineForDeploy(boxName, instance, globalEngine string) string {
	if entry, ok := LoadDeployConfigForRead("ResolveBoxEngineForDeploy").Lookup(boxName, instance); ok && entry.Engine != "" {
		return entry.Engine
	}
	return globalEngine
}

// ResolveBoxEngineFromMeta returns the engine from image metadata labels,
// falling back to globalEngine if not set.
func ResolveBoxEngineFromMeta(meta *spec.BoxMetadata, globalEngine string) string {
	if meta != nil && meta.Engine != "" {
		return meta.Engine
	}
	return globalEngine
}
