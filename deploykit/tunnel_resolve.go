package deploykit

import "github.com/opencharly/spec/spec"

// tunnel_resolve.go — re-export forwarder for the tunnel-config RESOLUTION transform, homed in
// spec (#55 U5 value-type consolidation). TunnelConfigFromMetadata (and its pure
// parseHostPorts/buildPortMapping/resolveProto helpers) is a pure resolve-to-envelope value
// transform over spec value types + spec.ParsePortMapping — no live *Config/*Candy graph, no host
// I/O beyond a stderr diagnostic — so it lives in the spec contract module. deploykit's callers
// reference deploykit.TunnelConfigFromMetadata unchanged. The dead FUNCTIONAL duplicate in
// sdk/kit/tunnel_metadata.go was retired in the same move (R3 / dead-code); ResolveTunnelConfig
// was deleted entirely (zero production callers — W3).
var (
	// TunnelConfigFromMetadata creates a TunnelConfig from image-label metadata.
	TunnelConfigFromMetadata = spec.TunnelConfigFromMetadata
)
