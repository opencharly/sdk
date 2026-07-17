package kit

import (
	"fmt"
	"os"
	"strconv"

	"github.com/opencharly/sdk/spec"
)

// tunnel_metadata.go — the label-only tunnel-config resolution (K4 lane B: relocated from
// charly/tunnel.go — genuinely pure, no candy/project dependency). ResolveTunnelConfig (which
// DOES need candy access) STAYS in charly core; parseHostPorts/buildPortMapping/resolveProto are
// shared by both, so they move here too and charly/tunnel.go's ResolveTunnelConfig calls the
// aliased copies (kit_aliases.go). Consumed directly by candy/plugin-deploy-pod
// (pod_lifecycle_resolve.go's quadlet-mode move, which reads a tunnel config off the image label
// exactly like this).

// ParseHostPorts extracts host-side ports from image port mappings via the
// canonical ParsePortMapping. Unparseable entries are reported on stderr —
// silent skipping was the root cause of an unrelated bug where tunnel rules
// vanished without a diagnostic.
func ParseHostPorts(boxPorts []string) []int {
	var result []int
	for _, mapping := range boxPorts {
		p, ok := ParsePortMapping(mapping)
		if !ok {
			fmt.Fprintf(os.Stderr,
				"Warning: ignoring unparseable port mapping %q (expected forms: \"P\", \"H:C\", \"IP:H:C\")\n",
				mapping)
			continue
		}
		result = append(result, p.Host)
	}
	return result
}

// BuildPortMapping builds a host→container port map from image port mappings.
// Same loud-failure policy as ParseHostPorts — see comment above.
func BuildPortMapping(boxPorts []string) map[int]int {
	m := make(map[int]int, len(boxPorts))
	for _, mapping := range boxPorts {
		p, ok := ParsePortMapping(mapping)
		if !ok {
			fmt.Fprintf(os.Stderr,
				"Warning: ignoring unparseable port mapping %q (expected forms: \"P\", \"H:C\", \"IP:H:C\")\n",
				mapping)
			continue
		}
		m[p.Host] = p.Container
	}
	return m
}

// ResolveProto returns the backend scheme for a container port, defaulting to "http".
// portProtos is string-keyed (the OCI-label wire form, P2B reshape) — index by the port as a string.
func ResolveProto(containerPort int, portProtos map[string]string) string {
	if portProtos != nil {
		if pp, ok := portProtos[strconv.Itoa(containerPort)]; ok {
			return pp
		}
	}
	return "http"
}

// TunnelConfigFromMetadata creates a TunnelConfig from image label metadata.
// Unlike ResolveTunnelConfig, this doesn't need candy access since the tunnel
// configuration is already stored in the label.
func TunnelConfigFromMetadata(meta *spec.BoxMetadata) *spec.TunnelConfig {
	if meta == nil || meta.Tunnel == nil {
		return nil
	}

	t := meta.Tunnel
	cfg := &spec.TunnelConfig{
		Provider: t.Provider,
		BoxName:  meta.Box,
	}

	hostPorts := ParseHostPorts(meta.Port)
	hostToContainer := BuildPortMapping(meta.Port)

	// Determine public set
	publicSet := make(map[int]bool)
	publicHostnames := make(map[int]string)
	if t.Public.All {
		for _, p := range hostPorts {
			publicSet[p] = true
		}
	}
	for _, p := range t.Public.Ports {
		publicSet[p] = true
	}
	for p, h := range t.Public.PortMap {
		publicSet[p] = true
		publicHostnames[p] = h
	}

	// Determine private set
	privateSet := make(map[int]bool)
	if t.Private.All {
		for _, p := range hostPorts {
			if !publicSet[p] {
				privateSet[p] = true
			}
		}
	}
	for _, p := range t.Private.Ports {
		privateSet[p] = true
	}

	// Build TunnelPort slice
	for _, hp := range hostPorts {
		if !publicSet[hp] && !privateSet[hp] {
			continue
		}
		cp := hp
		if c, ok := hostToContainer[hp]; ok {
			cp = c
		}
		proto := ResolveProto(cp, meta.PortProto)
		cfg.Ports = append(cfg.Ports, spec.TunnelPort{
			Port:        hp,
			BackendPort: hp,
			Protocol:    proto,
			Public:      publicSet[hp],
			Hostname:    publicHostnames[hp],
		})
	}

	// Cloudflare defaults
	if cfg.Provider == "cloudflare" {
		cfg.TunnelName = t.Tunnel
		if cfg.TunnelName == "" {
			cfg.TunnelName = "charly-" + meta.Box
		}
		cfg.Hostname = meta.DNS
	}

	return cfg
}
