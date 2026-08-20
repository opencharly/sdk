package kit

import (
	"maps"
	"strings"

	"github.com/opencharly/spec/proc"
	"github.com/opencharly/spec/spec"
)

// EnvConfig — resolved candy env (KEY=value vars + PATH-append entries). CUE-SOURCED in spec now
// (sdk/schema/candymodel.cue, the S-CM enabler) so #CandyModel can compose it; this ALIASES onto
// spec (SDD). The helper functions below operate on it unchanged.
type EnvConfig = spec.EnvConfig

// ExpandPath (a pure ~/${HOME}/$HOME path expander) now lives in spec
// (spec.ExpandPath, #55 import-purity cone-render); kit re-exports it via alias so
// every existing kit.ExpandPath call site (sdk) is untouched (R3, one source).
var ExpandPath = spec.ExpandPath

// ExpandEnvConfig expands all ~ and $HOME references in an EnvConfig
func ExpandEnvConfig(cfg *EnvConfig, home string) *EnvConfig {
	expanded := &EnvConfig{
		Vars:       make(map[string]string),
		PathAppend: make([]string, len(cfg.PathAppend)),
	}

	for key, value := range cfg.Vars {
		expanded.Vars[key] = ExpandPath(value, home)
	}

	for i, path := range cfg.PathAppend {
		expanded.PathAppend[i] = ExpandPath(path, home)
	}

	return expanded
}

// MergeEnvConfigs merges multiple env configs, later configs override earlier
func MergeEnvConfigs(configs []*EnvConfig) *EnvConfig {
	merged := &EnvConfig{
		Vars:       make(map[string]string),
		PathAppend: []string{},
	}

	for _, cfg := range configs {
		if cfg == nil {
			continue
		}
		// Merge vars (later overrides earlier)
		maps.Copy(merged.Vars, cfg.Vars)
		// Accumulate PATH entries
		merged.PathAppend = append(merged.PathAppend, cfg.PathAppend...)
	}

	return merged
}

// EnvPairsToMap converts KEY=VALUE pairs (the CLI -e / label wire form) into
// the map form the deploy schema stores since the env-shape unification.
func EnvPairsToMap(pairs []string) map[string]string {
	if len(pairs) == 0 {
		return nil
	}
	out := make(map[string]string, len(pairs))
	for _, kv := range pairs {
		k, v, _ := strings.Cut(kv, "=")
		out[k] = v
	}
	return out
}

// EnvMapToPairs converts the deploy schema's env map into sorted KEY=VALUE
// pairs (the OCI-label wire + env-resolution chain form). RELOCATED to the spec/spec fabric
// slice (#55 coneB build-render cone, Class A — github.com/opencharly/spec/spec/env_pairs_coneb.go);
// re-exported here so every existing kit.EnvMapToPairs call site (sdk/deploykit's deploy_file.go
// + read_labels.go, kit/box_metadata.go) is unchanged. New consumers reference proc.EnvMapToPairs
// directly.
var EnvMapToPairs = proc.EnvMapToPairs
