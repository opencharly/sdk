package loaderkit

// config_stack.go — the LAYERED CONFIG STACK: charly reads and MERGES charly.yml
// files in the order system → user → in-dir, LATER files winning, so the effective
// config of any invocation (and of the systemd-started MCP server) is the merged
// document.
//
//   - system: /etc/charly/charly.yml (CHARLY_SYSTEM_CONFIG override) — shipped by
//     the charly package (`charly generate-packages`, packaging.config);
//   - user:   spec.DefaultDeployConfigPath() (~/.config/charly/charly.yml) — the
//     existing per-host deploy overlay, resolved per invoking user;
//   - in-dir: <dir>/charly.yml — the project's own config (existing resolution).
//
// The merge happens at the RAW yaml.Node document level (top-level keys, later
// files winning). The raw round-trip preserves EVERY node — a typed
// spec.UnifiedFile parse drops the opaque candy bodies (json.RawMessage), which
// the plan's P1 principle ("any charly.yml must be capable of anything") forbids.

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// SystemConfigEnv overrides the system config layer's path (tests, alternate
// roots). Defaults to /etc/charly/charly.yml.
const SystemConfigEnv = "CHARLY_SYSTEM_CONFIG"

// DefaultSystemConfigPath is the system config layer path.
const DefaultSystemConfigPath = "/etc/charly/charly.yml"

// readConfigStack reads the three config layers and merges them (later files
// winning) at the raw document level. Returns (nil, false) when no layer exists
// (no project). A missing system/user layer is skipped (a stat).
func readConfigStack(dir string) ([]byte, bool, error) {
	systemPath := os.Getenv(SystemConfigEnv)
	if systemPath == "" {
		systemPath = DefaultSystemConfigPath
	}
	userPath, err := spec.DefaultDeployConfigPath()
	if err != nil {
		userPath = ""
	}
	projectPath := filepath.Join(dir, spec.UnifiedFileName)

	var layers [][]byte
	for _, p := range []string{systemPath, userPath, projectPath} {
		if p == "" || !kit.FileExists(p) {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, true, fmt.Errorf("read config stack layer %s: %w", p, err)
		}
		layers = append(layers, data)
	}
	if len(layers) == 0 {
		return nil, false, nil
	}
	merged, err := mergeConfigStackRaw(layers)
	if err != nil {
		return nil, true, err
	}
	return merged, true, nil
}

// mergeConfigStackRaw merges the raw charly.yml documents, LATER files winning
// per top-level key. The yaml.Node round-trip preserves every node losslessly.
// A layer that is not a mapping document (or is empty) is skipped.
func mergeConfigStackRaw(layers [][]byte) ([]byte, error) {
	dst := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for i, data := range layers {
		var doc yaml.Node
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("parse config stack layer %d: %w", i, err)
		}
		if len(doc.Content) == 0 {
			continue
		}
		root := doc.Content[0]
		if root.Kind != yaml.MappingNode {
			continue // non-mapping document — not a charly.yml; skip
		}
		mergeYamlMapping(dst, root)
	}
	out, err := yaml.Marshal(dst)
	if err != nil {
		return nil, fmt.Errorf("marshal merged config stack: %w", err)
	}
	return out, nil
}

// mergeYamlMapping merges src's key/value pairs into dst, src winning on a key
// conflict (later files win).
func mergeYamlMapping(dst, src *yaml.Node) {
	idx := make(map[string]int, len(dst.Content)/2)
	for i := 0; i+1 < len(dst.Content); i += 2 {
		idx[dst.Content[i].Value] = i
	}
	for i := 0; i+1 < len(src.Content); i += 2 {
		k := src.Content[i]
		v := src.Content[i+1]
		if j, ok := idx[k.Value]; ok {
			dst.Content[j+1] = v
		} else {
			dst.Content = append(dst.Content, k, v)
			idx[k.Value] = len(dst.Content) - 2
		}
	}
}
