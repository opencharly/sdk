package buildkit

import (
	"path/filepath"
	"slices"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// InitConfig represents the `init:` section of the embedded vocabulary (charly/charly.yml).
// Each init system defines how to detect, build, assemble, and manage services.
type InitConfig struct {
	Init map[string]*ResolvedInit `yaml:"init" json:"init"`
}

// DetectCandyInit returns which init system names a candy triggers,
// based on its candy manifest fields and file patterns.
func (ic *InitConfig) DetectCandyInit(ly *spec.CandyYAML, candyPath string) []string {
	if ic == nil {
		return nil
	}
	var result []string
	for initName, def := range ic.Init {
		if detectsInit(def, ly, candyPath) {
			result = append(result, initName)
		}
	}
	kit.SortStrings(result)
	return result
}

// detectsInit checks if a candy matches an init system's detection criteria.
// Schema-driven: iterates the unified service: list + per-entry init routing
// (IsPackaged → ServiceSchema.SupportsPackaged; custom exec → ServiceSchema.ServiceTemplate).
func detectsInit(def *ResolvedInit, ly *spec.CandyYAML, candyPath string) bool {
	if ly == nil {
		return false
	}
	// candy_field: [service] gates schema-driven detection.
	participatesInSchema := slices.Contains(def.CandyFields, "service")
	if participatesInSchema {
		for i := range ly.Service {
			entry := &ly.Service[i]
			if entry.IsPackaged() {
				if def.ServiceSchema != nil && def.ServiceSchema.SupportsPackaged {
					return true
				}
			} else {
				if def.ServiceSchema != nil && def.ServiceSchema.ServiceTemplate != "" {
					return true
				}
			}
		}
	}

	// candy_file: glob the candy dir (file_copy model — systemd *.service units).
	for _, pattern := range def.CandyFiles {
		matches, _ := filepath.Glob(filepath.Join(candyPath, pattern))
		if len(matches) > 0 {
			return true
		}
	}

	return false
}

// ResolveInitSystem determines the active init system for an image.
// Priority: explicit override → auto-detect from candies.
// Returns ("", nil) if no init system is needed.
//
// Candy capability requirements (RequiresCapabilities) are checked
// against the aggregated candy caps for the composition; init systems
// whose requirements aren't met are filtered out. The aggregated caps
// are also consulted for the bootc-prefer-systemd heuristic via
// PreserveUser (the canonical signal that this is a bootc-flavored
// composition).
func (ic *InitConfig) ResolveInitSystem(layers map[string]spec.CandyReader, candyOrder []string, explicit string) (string, *ResolvedInit) {
	if ic == nil {
		return "", nil
	}

	// Explicit override
	if explicit != "" {
		if def, ok := ic.Init[explicit]; ok {
			return explicit, def
		}
	}

	caps, _ := AggregateCandyCapabilities(layers, candyOrder)
	if caps == nil {
		caps = &AggregatedCandyCaps{Provided: map[string]bool{}}
	}

	// Auto-detect: find the init system that candies trigger
	initHits := make(map[string]bool)
	for _, candyName := range candyOrder {
		layer, ok := layers[candyName]
		if !ok {
			continue
		}
		for initName := range ic.Init {
			if layer.HasInit(initName) {
				initHits[initName] = true
			}
		}
		// port_relay triggers the init system with a relay_template
		if len(layer.RelayPorts()) > 0 {
			for initName, def := range ic.Init {
				if def.RelayTemplate != "" {
					initHits[initName] = true
				}
			}
		}
	}

	// Filter by capability requirements
	for initName := range initHits {
		def := ic.Init[initName]
		if !initDefRequirementsMet(def, caps) {
			delete(initHits, initName)
		}
	}

	// For bootc-flavored compositions (preserve_user) prefer systemd over supervisord
	if caps.PreserveUser && initHits["systemd"] {
		return "systemd", ic.Init["systemd"]
	}

	// For container images, prefer supervisord
	if initHits["supervisord"] {
		return "supervisord", ic.Init["supervisord"]
	}

	// Return first remaining init system
	for initName := range initHits {
		return initName, ic.Init[initName]
	}

	return "", nil
}

// ActiveInit returns all init systems that are active for the given image.
// An image can have multiple active inits (e.g., supervisord + systemd on
// bootc-flavored compositions).
func (ic *InitConfig) ActiveInit(layers map[string]spec.CandyReader, candyOrder []string) map[string]*ResolvedInit {
	if ic == nil {
		return nil
	}

	caps, _ := AggregateCandyCapabilities(layers, candyOrder)
	if caps == nil {
		caps = &AggregatedCandyCaps{Provided: map[string]bool{}}
	}

	result := make(map[string]*ResolvedInit)
	for _, candyName := range candyOrder {
		layer, ok := layers[candyName]
		if !ok {
			continue
		}
		for initName, def := range ic.Init {
			if !layer.HasInit(initName) {
				continue
			}
			if !initDefRequirementsMet(def, caps) {
				continue
			}
			result[initName] = def
		}
		// port_relay triggers init systems with relay_template
		if len(layer.RelayPorts()) > 0 {
			for initName, def := range ic.Init {
				if def.RelayTemplate != "" && initDefRequirementsMet(def, caps) {
					result[initName] = def
				}
			}
		}
	}

	return result
}

// initDefRequirementsMet reports whether the init definition's
// RequiresCapabilities are all present in the aggregated caps.
func initDefRequirementsMet(def *ResolvedInit, caps *AggregatedCandyCaps) bool {
	if def == nil || len(def.RequiresCapability) == 0 {
		return true
	}
	if caps == nil || caps.Provided == nil {
		return false
	}
	for _, req := range def.RequiresCapability {
		if !caps.Provided[req] {
			return false
		}
	}
	return true
}

// InitNames returns a sorted list of all init system names.
func (ic *InitConfig) InitNames() []string {
	if ic == nil {
		return nil
	}
	names := make([]string, 0, len(ic.Init))
	for name := range ic.Init {
		names = append(names, name)
	}
	kit.SortStrings(names)
	return names
}
