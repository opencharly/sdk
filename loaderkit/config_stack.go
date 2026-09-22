package loaderkit

// config_stack.go — the LAYERED CONFIG STACK: charly reads and MERGES charly.yml
// PROJECT documents in the order system → in-dir, LATER files winning, so the
// effective PROJECT config of any invocation (and of the systemd-started MCP
// server) is the merged document.
//
//   - system: /etc/charly/charly.yml (CHARLY_SYSTEM_CONFIG override) — shipped by
//     the charly package (`charly generate-packages`, packaging.config) as a bare
//     minimal PROJECT (a version + a charly-mcp candy node), so a host with no
//     project directory still resolves one;
//   - in-dir: <dir>/charly.yml — the project's own config (existing resolution).
//
// The per-host DEPLOY OVERLAY (~/.config/charly/charly.yml, spec.DefaultDeployConfigPath)
// is deliberately NOT a project-stack layer. It is a DeployConfig — per-host RUNTIME
// STATE (deploy entries + cache/ledger/system), not an authored project document. Raw-
// merging it as a document layer injects its deploy entries into the authored deploy map
// BEFORE FoldMembers runs, and a folded deploy-level MEMBER (which the overlay persists
// flattened to a TOP-LEVEL key so each member is independently addressable) then collides
// with the member the owner still declares nested — `peer name ... collides with an
// existing deploy/bed/peer entry`, seen live on every member-bearing bed
// (check-preempt-live-pod, check-cross-pod-cdp). The overlay has ALWAYS been applied the
// correct way — a per-field MERGE onto the loaded project tree (deploykit.MergeDeployConfigs
// / deploy.MergeDeployNode, driven by loaderkit.ResolveMergedTreeViaExecutor and
// deploykit.MergedDeployTree), whose lookup recurses into a node's Member tree, so it can
// never create a stray top-level key. That merge remains the overlay's ONE application
// (R3); this stack is projects only.
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

// readConfigStack reads the PROJECT config layers (system → in-dir) and merges them
// (later files winning) at the raw document level. Returns (nil, false) when no layer
// exists (no project). A missing system layer is skipped (a stat). The per-host deploy
// overlay is NOT a layer here — see the file header for why.
func readConfigStack(dir string) ([]byte, bool, error) {
	systemPath := os.Getenv(SystemConfigEnv)
	if systemPath == "" {
		systemPath = DefaultSystemConfigPath
	}
	projectPath := filepath.Join(dir, spec.UnifiedFileName)

	var layers [][]byte
	for _, p := range []string{systemPath, projectPath} {
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
// A layer that is not a mapping document (or is empty) is skipped. A single
// document whose top-level names repeat is rejected with the loader's ParseDoc
// contract message (parse.go) — the duplicate surfaces at the config-stack
// merge, before any seam is dereferenced.
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
		// The yaml.Node parse tolerates repeated mapping keys (the decoder
		// rejects them only when decoding into a map) — enforce the loader's
		// top-level-name-uniqueness contract here, matching ParseDoc's message.
		seen := make(map[string]string, len(root.Content)/2)
		for j := 0; j+1 < len(root.Content); j += 2 {
			name := root.Content[j].Value
			if prior, dup := seen[name]; dup {
				return nil, fmt.Errorf("node %q: duplicate top-level entity name (already declared as a %q node) — a single document's top-level node names are globally unique; rename one (keep the user-facing deploy name, suffix the template)", name, prior)
			}
			seen[name] = name
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
