package loaderkit

// bundle_config_executor.go — the cycle-free plugin-side replacement for the deleted
// sdk/deploykit/load_bundle_config_seam.go (#55 coneC Unit C2). The former
// deploykit.LoadBundleConfigViaSeam reached the per-host deploy overlay by calling BACK to
// charly's "pod-config-load-bundle" HostBuild host handler (which called
// deploykit.LoadBundleConfig — the DeployStateHost-seam path). That seam is now retired:
// loaderkit already imports deploykit (bundle_load.go et al.), so a helper LIVING IN loaderkit
// can call LoadUnifiedViaExecutor + deploykit.ProjectBundleConfig DIRECTLY — cycle-free — and a
// plugin (which can import both loaderkit + deploykit) calls this helper instead of round-tripping
// to the host. deploykit itself CANNOT host this helper (it cannot import loaderkit — the cycle),
// so the plugin-reachable overlay read lives HERE, in the kit that already depends on deploykit.
//
// Placement-invariant: works identically compiled-in or out-of-process (LoadUnifiedViaExecutor
// drives the registry-coupled LoadSeams over the reverse channel when out-of-process, in-proc when
// compiled-in). Byte-equivalent to the former LoadBundleConfigViaSeam: same per-host overlay read,
// same (nil, nil)-on-absent contract, same ProjectBundleConfig projection the host handler used.

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/opencharly/sdk"
	"github.com/opencharly/sdk/deploykit"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// LoadBundleConfigViaExecutor loads <dir>/charly.yml through the unified loader PLUGIN-SIDE (over
// the reverse channel when out-of-process) and projects it to a *deploykit.BundleConfig — the
// cycle-free, placement-invariant overlay/project read. dir is the config directory (the per-host
// overlay dir for a deploy read; a project dir for a project read). Returns (nil, nil) when the
// file is absent (matching LoadBundleConfigViaSeam's nil-on-empty contract).
func LoadBundleConfigViaExecutor(ctx context.Context, ex *sdk.Executor, dir string) (*deploykit.BundleConfig, error) {
	if ex == nil {
		return nil, fmt.Errorf("load bundle config via executor: no host reverse channel (command not compiled-in?)")
	}
	uf, ok, err := LoadUnifiedViaExecutor(ctx, ex, dir)
	if err != nil {
		return nil, fmt.Errorf("load bundle config via executor: %w", err)
	}
	if !ok || uf == nil {
		return nil, nil
	}
	return deploykit.ProjectBundleConfig(uf), nil
}

// hostBundleConfigDir returns the per-host deploy-overlay config directory
// (filepath.Dir(kit.DefaultDeployConfigPath)) — the dir LoadBundleConfigViaSeam reached
// indirectly through the host handler's deploykit.LoadBundleConfig. Empty string when the path
// can't be resolved (the read then degrades to (nil, nil), matching the old nil-on-absent contract).
func hostBundleConfigDir() string {
	path, err := kit.DefaultDeployConfigPath()
	if err != nil {
		return ""
	}
	return filepath.Dir(path)
}

// LoadHostBundleConfigViaExecutor is the drop-in replacement for the deleted
// deploykit.LoadBundleConfigViaSeam(ctx, ex, caller): it reads the PER-HOST deploy overlay
// (~/.config/charly/charly.yml) plugin-side via LoadBundleConfigViaExecutor + the derived per-host
// config dir. The former `caller` label was host-side diagnostics only (threaded into the wire
// request); this path does not round-trip to a host handler, so no caller label is carried.
// Returns (nil, nil) on an absent/empty overlay.
func LoadHostBundleConfigViaExecutor(ctx context.Context, ex *sdk.Executor) (*deploykit.BundleConfig, error) {
	dir := hostBundleConfigDir()
	if dir == "" {
		return nil, nil
	}
	return LoadBundleConfigViaExecutor(ctx, ex, dir)
}

// ResolveLifecycleDeployNodeViaExecutor is the drop-in replacement for the deleted
// deploykit.ResolveLifecycleDeployNodeViaSeam: it resolves the per-host deploy overlay entry for
// a start/stop/shell/cmd/logs/service verb PLUGIN-SIDE, threading the DATA a command:pod /
// command:cmd plugin passes into the pod-lifecycle HostBuild requests (spec.PodStartRequest.Node
// et al.) so the host's dispatchLifecycleTarget operates on the passed *spec.Deploy instead of
// re-reading the per-host config itself.
//
// Byte-identical to the former core resolver: the dc.Bundle[key] lookup keyed by DeployKey, the
// container/""→pod Target normalization, and the {Target:pod} fallback for a bare image with no
// deploy entry (the former standalone-podman path). Returns (node, deployKey); node is never nil.
func ResolveLifecycleDeployNodeViaExecutor(ctx context.Context, ex *sdk.Executor, box, instance string) (*spec.Deploy, string) {
	key := spec.DeployKey(box, instance)
	if dc, _ := LoadHostBundleConfigViaExecutor(ctx, ex); dc != nil {
		if node, ok := dc.Bundle[key]; ok {
			n := node
			if n.Target == "" || n.Target == "container" {
				n.Target = "pod"
			}
			return &n, key
		}
	}
	return &spec.Deploy{Target: "pod"}, key
}