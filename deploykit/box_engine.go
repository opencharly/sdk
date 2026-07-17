package deploykit

import "github.com/opencharly/sdk/spec"

// box_engine.go — the two per-deploy engine-resolution functions with NO project-loader
// dependency (K4: relocated from charly/engine.go). Homed here (not kit) because
// ResolveBoxEngineForDeploy needs LoadDeployConfigForRead, a deploykit-only mechanism (kit cannot
// import deploykit — that would cycle). ResolveBoxEngine/ResolveBoxEngineFromDir/ImageRuntime
// (which DO need *Config/*Candy/LoadConfig) STAY in charly core. Shared between charly core's
// remaining callers (preempt.go, resolved_project_host.go, status_collector.go — direct
// deploykit.ResolveBoxEngineForDeploy calls, K3 ZERO-ALIASES) and candy/plugin-deploy-pod
// (the pod-lifecycle resolvers).

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
