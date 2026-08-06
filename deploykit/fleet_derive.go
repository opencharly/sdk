package deploykit

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// fleet_derive.go — FleetConfig-derived facts (container names, occupied ports).
// SPIKE (value-type relocation, #55 cluster 2): these were FleetConfig METHODS
// before FleetConfig relocated to spec.FleetConfig — demoted to free functions
// here (receiver → first parameter) because each reaches sdk/kit
// (ContainerNameInstance/IsAutoPort/ParseHostPort), which spec can never import
// (the method-set cycle the spike flagged). Call sites change from
// `dc.DeployedContainerNames()` to `deploykit.DeployedContainerNames(dc)`.

// DeployedContainerNames returns the sorted, de-duplicated container names for
// every fleet entry.
func DeployedContainerNames(dc *FleetConfig) []string {
	if dc == nil {
		return nil
	}
	var names []string
	seen := map[string]bool{}
	for key := range dc.Fleet {
		img, inst := ParseDeployKey(key)
		name := kit.ContainerNameInstance(img, inst)
		if !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	sort.Strings(names)
	return names
}

// OccupiedHostPorts returns the set of host ports already claimed by fleet
// entries other than excludeKey (resolved pins preferred over authored).
func OccupiedHostPorts(dc *FleetConfig, excludeKey string) map[int]bool {
	out := map[int]bool{}
	if dc == nil {
		return out
	}
	for key, entry := range dc.Fleet {
		if key == excludeKey {
			continue
		}
		mappings := entry.ResolvedPort
		if mappings == nil {
			mappings = entry.Port
		}
		for _, m := range mappings {
			if kit.IsAutoPort(m) {
				continue
			}
			if h, err := kit.ParseHostPort(m); err == nil {
				out[h] = true
			}
		}
	}
	return out
}

// PodAwareEnvProvides rewrites same-deploy env values to localhost and dedups
// cross-deploy entries by name (local wins).
func PodAwareEnvProvides(entries []spec.EnvProvideEntry, consumerKey, ctrName string) []spec.EnvProvideEntry {
	var result []spec.EnvProvideEntry
	seen := map[string]bool{}
	for _, e := range entries {
		if e.Source == consumerKey {
			local := e
			local.Value = strings.ReplaceAll(e.Value, ctrName, "localhost")
			result = append(result, local)
			seen[e.Name] = true
		}
	}
	for _, e := range entries {
		if e.Source != consumerKey && !seen[e.Name] {
			result = append(result, e)
		}
	}
	return result
}

// GlobalEnvForImage builds the env-var injection list for a consumer container
// from the deploy state's env + MCP provides, filtered by the consumer's accepts.
func GlobalEnvForImage(dc *FleetConfig, consumerKey, ctrName string, acceptedEnv map[string]bool) []string {
	if dc == nil || dc.Provides == nil {
		return nil
	}
	var result []string
	for _, entry := range PodAwareEnvProvides(dc.Provides.Env, consumerKey, ctrName) {
		if acceptedEnv == nil || acceptedEnv[entry.Name] {
			result = AppendOrReplaceEnv(result, entry.Name+"="+entry.Value)
		}
	}
	if len(dc.Provides.MCP) > 0 {
		mcpEntries := spec.PodAwareMCPProvides(dc.Provides.MCP, consumerKey, ctrName)
		if len(mcpEntries) > 0 {
			mcpJSON, _ := json.Marshal(mcpEntries)
			result = append(result, "CHARLY_MCP_SERVERS="+string(mcpJSON))
		}
	}
	return result
}
