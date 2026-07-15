package deploykit

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
	"gopkg.in/yaml.v3"
)

// deploy_file.go — the deploy STATE-MODEL load/save/merge/export body relocated out of
// charly/deploy.go to sdk/deploykit (K5-Unit-1, the S-K5 keystone that unblocks P13). The
// PURE helpers (LoadDeployFile / RemoveBoxDeploy) were already here; this cutover moved the
// state-model bodies — LoadBundleConfig / SaveBundleConfig / LoadDeployConfigForRead /
// LoadDeployConfigForWrite / MergeDeployOntoMetadata / CleanDeployEntry / SaveDeployState /
// ExportAllBox — plus the SaveDeployStateInput type (deploy_state.go) and the pure helpers
// (ScopeVolumesToDeployKey / DescriptionInfo / IsSameBaseBox / RemoveBySource /
// RemoveByExactSource / Named).
//
// ONE of these bodies reaches a core Mechanism the SDK cannot import — the unified LOADER
// (LoadUnified → uf.ProjectBundleConfig), reached through the DeployStateHost seam below
// (filled by charly at init). The process-shared deploy-config FLOCK + the runtime VERSION
// stamp are kind-blind kit primitives (kit.AcquireFileLock / kit.LatestSchemaVersion) called
// directly; the deploy-kind-specific marshal (the struct-body → compact node-form transform)
// is the caller's responsibility, supplied as a callback to the kind-blind SaveBundleConfig
// shell. ExportAllBox is the #67 keystone: it takes the spec.ResolvedProject envelope the
// "resolved-project" HostBuild seam produces (K5-Unit-0) and projects the box-authored
// deploy-overlay fields into a BundleConfig — no live *Config graph. The per-host ledger is
// read/written by compiled-in command plugins via the config-resolve / config-persist host
// seams (the plugin-vm precedent); these bodies are the shared library those seams and the
// charly command family both call.

// StateHostMechanisms carries the ONE host-side Mechanism the state-file load/save bodies need
// but the SDK cannot import: the unified LOADER (LoadUnified → uf.ProjectBundleConfig), reached
// through LoadUnifiedBundleConfig. charly/ fills it at init (RegisterDeployStateHost); a plugin
// or SDK consumer that does not register it gets nil-safe no-ops from the write paths (the read
// paths fall back to the kit-only file path when the seam is absent).
//
// E/M justification (the kernel/plugin boundary law): LoadUnifiedBundleConfig is a kind-blind
// MECHANISM (M) — it loads ANY per-host charly.yml through the unified loader and returns its
// ProjectBundleConfig, never branching on a deploy kind. It lives in the seam (not in deploykit
// directly) ONLY because the LoadUnified orchestration has not yet relocated to sdk/loaderkit
// (K1, task #31): once K1 lands, plugin-bundle calls loaderkit.LoadUnified directly and this
// seam dies entirely. The deploy-kind-specific marshal is NOT here — it is the caller's
// responsibility, supplied as a callback to SaveBundleConfig (a kind-blind file shell). Tracked
// K1-exit task: this seam field is deleted when K1 lands.
type StateHostMechanisms struct {
	// LoadUnifiedBundleConfig loads a per-host charly.yml configDir through the unified
	// loader and returns its ProjectBundleConfig (or nil, nil when absent / empty).
	LoadUnifiedBundleConfig func(configDir string) (*BundleConfig, error)
}

// DeployStateHost is the seam charly fills at init. Nil-safe: the write paths (SaveDeployState /
// CleanDeployEntry) no-op when it is nil; LoadBundleConfig returns (nil, nil) (absent file) so
// the read-only validate/inspect paths work without a registration. A plugin/SDK consumer that
// never writes the per-host ledger leaves it nil.
var DeployStateHost *StateHostMechanisms

// RegisterDeployStateHost is the charly init hook (called once from package main at startup).
func RegisterDeployStateHost(h *StateHostMechanisms) {
	if h != nil {
		DeployStateHost = h
	}
}

// LoadBundleConfig reads the per-host deploy overlay (~/.config/charly/charly.yml) through the
// unified loader — the SAME LoadUnified path as every project charly.yml. Returns nil, nil if
// the file doesn't exist. Relocated from charly/deploy.go (K5-Unit-1); the LoadUnified hop
// reaches core through DeployStateHost.LoadUnifiedBundleConfig.
//
// Every transform the old bespoke parser did — the `images:` legacy-key reject, the
// deployment-tree / required-box: / preemptible / ephemeral-naming validation, and the
// ephemeral→disposable auto-promotion — runs INSIDE LoadUnified (its version gate + the
// deploy-validation block subsume the legacy check; the ephemeral/naming validators +
// promotion were consolidated there so a PROJECT charly.yml's inline deploy: entries get them
// too — R3, one path).
func LoadBundleConfig() (*BundleConfig, error) {
	path, err := kit.DefaultDeployConfigPath()
	if err != nil {
		return nil, nil
	}
	configDir := filepath.Dir(path)

	// Host-file-existence guard: a host still on the legacy `deploy.yml` filename would
	// otherwise silently lose its overlay (LoadUnified reads charly.yml only when the project
	// is already at HEAD). Fail loud with the migration hint — mirrors the old
	// hasLegacyImagesKey safety.
	if legacy := filepath.Join(configDir, "deploy.yml"); kit.FileExists(legacy) && !kit.FileExists(path) {
		return nil, fmt.Errorf(
			"per-host deploy overlay at %s uses the legacy `deploy.yml` filename — rename it to charly.yml (the unified per-host config)",
			legacy,
		)
	}

	if DeployStateHost == nil || DeployStateHost.LoadUnifiedBundleConfig == nil {
		return nil, nil
	}
	dc, err := DeployStateHost.LoadUnifiedBundleConfig(configDir)
	if err != nil {
		return nil, err
	}
	if dc != nil {
		return dc, nil
	}
	// A present-but-empty config still returns a non-nil BundleConfig (matching the old
	// bespoke parser), so callers that range/index dc.Deploy without a nil guard keep working
	// after an overlay's last entry is removed.
	return &BundleConfig{}, nil
}

// SaveBundleConfig writes a BundleConfig to the standard charly.yml path. The kind-blind file
// shell: the HEAD version stamp (kit.LatestSchemaVersion), the provides directive, the
// caller-supplied per-entry node-form bodies, and the atomic tempfile+os.Rename write with a
// fail-safe re-check. The deploy-kind-specific marshal (the struct-body → compact node-form
// transform) is the caller's responsibility — supplied via marshalNode, which returns the
// MIGRATED node-form body for one BundleNode (the value placed under the entry's name key).
// This keeps SaveBundleConfig kind-blind: no per-kind map, no target-vocabulary switch, no
// deploy-kind-specific struct knowledge. Relocated from charly/deploy.go (K5-Unit-1); the
// LoadUnified hop (the fail-safe re-check) reaches core through DeployStateHost.
func SaveBundleConfig(dc *BundleConfig, marshalNode func(name string, node *BundleNode) (*yaml.Node, error)) error {
	path, err := kit.DefaultDeployConfigPath()
	if err != nil {
		return fmt.Errorf("determining deploy config path: %w", err)
	}
	// FAIL-SAFE (data-safety): refuse to clobber a present-but-currently-unloadable per-host
	// config. A writer that loaded through the error-swallowing LoadDeployConfigForRead path
	// holds a DEGRADED (empty) BundleConfig whenever the on-disk file fails the loader gate;
	// writing that degraded config would TRUNCATE the user's recoverable deploy state. Re-check
	// the on-disk file here and abort with a `charly migrate` hint instead — the bytes stay on
	// disk for the migration to recover.
	if _, lerr := LoadBundleConfig(); lerr != nil {
		return fmt.Errorf("refusing to overwrite %s — the existing per-host config fails to load (%w); fix it (or remove it to regenerate) first", path, lerr)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	if dc == nil {
		dc = &BundleConfig{}
	}
	if marshalNode == nil {
		return fmt.Errorf("SaveBundleConfig: marshalNode callback is nil (the deploy-kind-specific marshal is the caller's responsibility)")
	}
	// Write a unified node-form per-host charly.yml: the HEAD `version:` stamp lets a re-load
	// through LoadUnified pass the schema gate; `provides:` stays a document directive; each
	// deploy entry is a name-first node `<name>: {<disc>: <body>, <child-nodes>}` — the SAME
	// shape the node-form loader accepts (the only authoring surface), produced by the caller's
	// marshalNode callback.
	root := &yaml.Node{Kind: yaml.MappingNode}
	root.Content = append(root.Content, kit.ScalarNode("version"), kit.ScalarNode(kit.LatestSchemaVersion().String()))
	if dc.Provides != nil {
		pb, perr := yaml.Marshal(dc.Provides)
		if perr != nil {
			return fmt.Errorf("marshaling provides: %w", perr)
		}
		var pd yaml.Node
		if perr := yaml.Unmarshal(pb, &pd); perr != nil {
			return fmt.Errorf("re-parsing provides: %w", perr)
		}
		if len(pd.Content) == 1 {
			root.Content = append(root.Content, kit.ScalarNode("provides"), pd.Content[0])
		}
	}
	names := make([]string, 0, len(dc.Bundle))
	for n := range dc.Bundle {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		node := dc.Bundle[name]
		body, merr := marshalNode(name, &node)
		if merr != nil {
			return fmt.Errorf("marshaling deploy %q: %w", name, merr)
		}
		root.Content = append(root.Content, kit.ScalarNode(name), body)
	}
	data, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("marshaling deploy config: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".charly.yml.tmp.*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("renaming %s -> %s: %w", tmpPath, path, err)
	}
	return nil
}

// LoadDeployConfigForRead loads charly.yml for read-only consumption. Unlike the historical
// `dc, _ := LoadBundleConfig()` pattern (silently discards validation errors → caller proceeds
// with nil → feature degrades invisibly), this helper SURFACES the load error as a stderr
// warning while still returning nil — preserving the existing caller nil-check contract but
// giving the operator visibility into why a command behaved as if charly.yml were absent.
// Relocated from charly/deploy.go (K5-Unit-1).
//
// context is a short human-readable label included in the warning message so the operator can
// trace which code path noticed the problem (e.g. "charly status", "config injectEnvProvides").
func LoadDeployConfigForRead(context string) *BundleConfig {
	dc, err := LoadBundleConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %s: charly.yml unavailable for read: %v\n", context, err)
	}
	// NEVER return nil — a caller dereferences `dc.Deploy[...]` directly (and some assign into
	// it), so an absent config (LoadBundleConfig → (nil, nil)) or a load error both degrade to
	// an EMPTY config with a live map (image-label-driven behavior), not a nil-deref /
	// nil-map-assignment panic.
	if dc == nil {
		return &BundleConfig{Bundle: map[string]BundleNode{}}
	}
	if dc.Bundle == nil {
		dc.Bundle = map[string]BundleNode{}
	}
	return dc
}

// LoadDeployConfigForWrite loads charly.yml for mutation. Unlike the historical
// `dc, _ := LoadBundleConfig()` pattern (silently discards validation errors → writer constructs
// an empty config → SaveBundleConfig truncates the file), this helper PROPAGATES the load error
// so writers can ABORT instead of destroying data. Relocated from charly/deploy.go (K5-Unit-1).
//
// context is a short human-readable label included in the error message (e.g. "saveDeployState").
// Returns (nil, error) when the file exists but failed parse/validation; (fresh empty config,
// nil) when the file doesn't exist; (parsed config, nil) on clean load.
func LoadDeployConfigForWrite(context string) (*BundleConfig, error) {
	dc, err := LoadBundleConfig()
	if err != nil {
		return nil, fmt.Errorf("%s: refusing to write — charly.yml load failed: %w", context, err)
	}
	if dc == nil {
		dc = &BundleConfig{Bundle: make(map[string]BundleNode)}
	}
	if dc.Bundle == nil {
		dc.Bundle = make(map[string]BundleNode)
	}
	return dc, nil
}

// MergeDeployOntoMetadata applies a per-host charly.yml entry's overrides (ports, env,
// security, tunnel, secrets, …) onto label-derived image metadata. Field-level replace
// semantics. Relocated from charly/deploy.go (K5-Unit-1).
//
// The overlay entry is keyed by deployName — the charly.yml key base the caller is operating
// on (the bed / instance / Pattern-B name), NOT meta.Image (the baked ai.opencharly.box
// short-name). For a plain deploy the two coincide, but a kind:check bed or a Pattern-B deploy
// carries a key distinct from its image, so the caller MUST pass its own deploy key (typically
// c.Image). Keying off meta.Image would read whichever sibling deploy merely shares the image
// and clobber this entry's explicit port:/env:/security:.
//
//nolint:gocyclo // field-by-field conditional overlay merge; every branch is a peer
func MergeDeployOntoMetadata(meta *spec.BoxMetadata, dc *BundleConfig, deployName, instance string) {
	// Volume isolation runs UNCONDITIONALLY (independent of any charly.yml overlay), so every
	// distinctly-named deploy gets its own volume namespace on the very first `charly config` and
	// every run after — see ScopeVolumesToDeployKey.
	ScopeVolumesToDeployKey(meta, deployName, instance)

	if dc == nil || dc.Bundle == nil || meta == nil {
		return
	}

	overlay, ok := dc.Bundle[DeployKey(deployName, instance)]
	if !ok {
		return
	}

	if overlay.Description != "" {
		// A deploy overlay's description is purely informational — it carries no status signal
		// (the maturity rung lives on the candy `status:` field and is baked into the image's
		// ai.opencharly.status label). Keep the baked meta.Status; only refresh the human-facing
		// summary.
		meta.Info = DescriptionInfo(overlay.Description)
	}
	if overlay.Tunnel != nil {
		meta.Tunnel = overlay.Tunnel
	}
	if overlay.DNS != "" {
		meta.DNS = overlay.DNS
	}
	if overlay.AcmeEmail != "" {
		meta.AcmeEmail = overlay.AcmeEmail
	}
	// Ports: prefer the persisted ResolvedPort (the auto-allocated / pinned host:container mapping
	// `charly config` computed via ResolveDeployPorts). A deploy `port:` entry is only a PIN INPUT
	// to that resolution — never a wholesale replacement — so it is NOT applied here. If
	// ResolvedPort isn't set yet (deploy not configured), meta.Port keeps the image-label's bare
	// container ports (published 1:1 on 127.0.0.1 until the next charly config resolves them).
	if overlay.ResolvedPort != nil {
		meta.Port = overlay.ResolvedPort
	}
	if overlay.Env != nil {
		meta.Env = kit.EnvMapToPairs(overlay.Env)
	}
	if overlay.Security != nil {
		// Field-level merge: overlay fields override, unset fields fall through to the label-provided
		// values. A full struct replace would wipe candy defaults like shm_size when a user sets just
		// --memory-max via `charly config`.
		if overlay.Security.Privileged {
			meta.Security.Privileged = true
		}
		if len(overlay.Security.CapAdd) > 0 {
			meta.Security.CapAdd = overlay.Security.CapAdd
		}
		if len(overlay.Security.Devices) > 0 {
			meta.Security.Devices = overlay.Security.Devices
		}
		if len(overlay.Security.SecurityOpt) > 0 {
			meta.Security.SecurityOpt = overlay.Security.SecurityOpt
		}
		if overlay.Security.ShmSize != "" {
			meta.Security.ShmSize = overlay.Security.ShmSize
		}
		if overlay.Security.IpcMode != "" {
			meta.Security.IpcMode = overlay.Security.IpcMode
		}
		if overlay.Security.CgroupNS != "" {
			meta.Security.CgroupNS = overlay.Security.CgroupNS
		}
		if len(overlay.Security.GroupAdd) > 0 {
			meta.Security.GroupAdd = overlay.Security.GroupAdd
		}
		if len(overlay.Security.Mounts) > 0 {
			meta.Security.Mounts = overlay.Security.Mounts
		}
		if overlay.Security.MemoryMax != "" {
			meta.Security.MemoryMax = overlay.Security.MemoryMax
		}
		if overlay.Security.MemoryHigh != "" {
			meta.Security.MemoryHigh = overlay.Security.MemoryHigh
		}
		if overlay.Security.MemorySwapMax != "" {
			meta.Security.MemorySwapMax = overlay.Security.MemorySwapMax
		}
		if overlay.Security.Cpus != "" {
			meta.Security.Cpus = overlay.Security.Cpus
		}
	}
	if overlay.Network != "" {
		meta.Network = overlay.Network
	}
	if overlay.Engine != "" {
		meta.Engine = overlay.Engine
	}
	// Merge charly.yml secrets onto image label secrets
	if overlay.Secret != nil {
		deployByName := make(map[string]spec.DeploySecret, len(overlay.Secret))
		for _, ds := range overlay.Secret {
			deployByName[ds.Name] = ds
		}
		// Override matching secrets from image labels with charly.yml source config
		for i, ls := range meta.Secret {
			if _, ok := deployByName[ls.Name]; ok {
				// Deploy.yml provides this secret — keep the label entry (the source override is
				// used at provisioning time, not in the label)
				_ = i
			}
		}
		// Add deploy-only secrets that aren't in the image labels
		for _, ds := range overlay.Secret {
			found := false
			for _, ls := range meta.Secret {
				if ls.Name == ds.Name {
					found = true
					break
				}
			}
			if !found {
				meta.Secret = append(meta.Secret, spec.LabelSecretEntry{
					Name:   ds.Name,
					Target: "/run/secrets/" + ds.Name,
				})
			}
		}
	}
}

// ExportAllBox exports all runtime-relevant fields for all enabled boxes in a RESOLVED project
// envelope. Relocated from charly/deploy.go (K5-Unit-1, the #67 keystone): the former
// `ExportAllBox(cfg *Config)` read the live *Config graph; this rewrite reads the
// spec.ResolvedProject envelope the "resolved-project" HostBuild seam (K5-Unit-0) produces, so
// the deploy state model is built from the SAME envelope inspect/list/validate consume — no
// core *Config dep. The box-authored deploy-overlay surfaces (description / env / env_file /
// security / network) ride #ResolvedBoxView (grown in this cutover); version is the box's
// EffectiveVersion (the stable content-derived CalVer), matching the prior behaviour's
// `img.Version`.
func ExportAllBox(rp *spec.ResolvedProject) *BundleConfig {
	dc := &BundleConfig{Bundle: make(map[string]BundleNode)}
	if rp == nil {
		return dc
	}
	// Deterministic name order (map iteration is random); matches the former cfg.allBoxNames()
	// sort + the ResolvedProject.BuildTargets order.
	names := make([]string, 0, len(rp.Boxes))
	for name := range rp.Boxes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		view := rp.Boxes[name]
		// Schema v4: Tunnel / DNS / AcmeEmail / Engine no longer sourced from BoxConfig
		// (they're deploy-only).
		entry := BundleNode{
			Version:     view.Version,
			Description: view.Description,
			Env:         view.Env,
			EnvFile:     view.EnvFile,
			Security:    view.Security,
			Network:     view.Network,
		}
		// Only include if at least one field is set. Ports are no longer a box field — they're
		// inherited from candies and auto-allocated at deploy.
		if entry.Version != "" || entry.Description != "" ||
			entry.Env != nil ||
			entry.EnvFile != "" || entry.Security != nil || entry.Network != "" {
			dc.Bundle[name] = entry
		}
	}
	return dc
}

// LoadDeployFile reads a charly.yml from an arbitrary path into a BundleConfig.
func LoadDeployFile(path string) (*BundleConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var dc BundleConfig
	if err := yaml.Unmarshal(data, &dc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &dc, nil
}

// RemoveBoxDeploy removes an image's entry from a deploy config.
func RemoveBoxDeploy(dc *BundleConfig, boxName string) {
	if dc != nil && dc.Bundle != nil {
		delete(dc.Bundle, boxName)
	}
}

// ensure strings is used (DescriptionInfo / ScopeVolumesToDeployKey / IsSameBaseBox are in
// deploy_state.go but the package shares the import set — strings is referenced here only by
// kit.ScalarNode-free paths; keep the explicit reference for goimports clarity).
var _ = strings.TrimSpace
