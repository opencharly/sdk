package deploykit

import "github.com/opencharly/spec/spec"

// tunnel_resolve.go — re-export forwarders for the tunnel-config RESOLUTION transforms, now
// homed in spec (#55 U5 value-type consolidation). ResolveTunnelConfig / TunnelConfigFromMetadata
// (and their pure parseHostPorts/buildPortMapping/resolveProto helpers) are pure resolve-to-envelope
// value-transforms over spec value types + spec.ParsePortMapping (delivered by U3) — no live
// *Config/*Candy graph, no host I/O beyond a stderr diagnostic — so they moved to the spec contract
// module. deploykit's callers (charly host_build_pod_config_seams, candy/plugin-box,
// candy/plugin-deploy-pod) reference deploykit.X unchanged. The dead FUNCTIONAL duplicate in
// sdk/kit/tunnel_metadata.go — a stale copy of these helpers (lacking the live ResolveTunnelConfig;
// zero production callers) — was retired in the same move (R3 / dead-code).
var (
	// ResolveTunnelConfig resolves a TunnelYAML into a TunnelConfig with defaults applied.
	ResolveTunnelConfig = spec.ResolveTunnelConfig
	// TunnelConfigFromMetadata creates a TunnelConfig from image-label metadata.
	TunnelConfigFromMetadata = spec.TunnelConfigFromMetadata
)
