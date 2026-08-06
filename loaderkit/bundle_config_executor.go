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
// same non-nil &BundleConfig{}-on-absent/empty contract (inherited from
// deploykit.LoadBundleConfig's line-112 wrap), same ProjectBundleConfig projection the host
// handler used.

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
// overlay dir for a deploy read; a project dir for a project read). An ABSENT or EMPTY overlay
// returns a NON-NIL &deploykit.BundleConfig{} — matching deploykit.LoadBundleConfig's contract
// (deploy_file.go line 112: a present-but-empty config returns &BundleConfig{} so callers that
// range/index dc.Deploy without a nil guard keep working after an overlay's last entry is
// removed); the former LoadBundleConfigViaSeam round-tripped to LoadBundleConfig and inherited
// the same non-nil-empty wrap..
func LoadBundleConfigViaExecutor(ctx context.Context, ex *sdk.Executor, dir string) (*deploykit.BundleConfig, error) {
	if ex == nil {
		return nil, fmt.Errorf("load bundle config via executor: no host reverse channel (command not compiled-in?)")
	}
	uf, ok, err := LoadUnifiedViaExecutor(ctx, ex, dir)
	if err != nil {
		return nil, fmt.Errorf("load bundle config via executor: %w", err)
	}
	if !ok || uf == nil {
		// Absent/empty overlay → non-nil &BundleConfig{}, matching deploykit.LoadBundleConfig's
		// line-112 wrap (deploy_file.go: a present-but-empty config returns &BundleConfig{} so
		// callers that range/index dc.Deploy without a nil guard keep working). The K5 rewiring
		// returned nil here, dropping the wrap — R1 RCA: that short-circuited reap-orphans' orphan
		// scan in a bed's XDG_CONFIG_HOME-isolated empty overlay ("no charly.yml; nothing to
		// reap" instead of the expected "no orphaned ephemerals"); restored.
		return &deploykit.BundleConfig{}, nil
	}
	return deploykit.ProjectBundleConfig(uf), nil
}

// hostBundleConfigDir returns the per-host deploy-overlay config directory
// (filepath.Dir(kit.DefaultDeployConfigPath)) — the dir LoadBundleConfigViaSeam reached
// indirectly through the host handler's deploykit.LoadBundleConfig. Empty string when the path
// can't be resolved (the read then degrades to a non-nil &BundleConfig{}, matching
// deploykit.LoadBundleConfig's absent/empty contract).
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
// Returns a NON-NIL &BundleConfig{} on an absent/empty overlay (matching
// deploykit.LoadBundleConfig's absent-file contract); (nil, nil) only when the config path
// itself can't be resolved (the dir=="" guard, matching LoadBundleConfig's path-error nil).
func LoadHostBundleConfigViaExecutor(ctx context.Context, ex *sdk.Executor) (*deploykit.BundleConfig, error) {
	dir := hostBundleConfigDir()
	if dir == "" {
		return nil, nil
	}
	return LoadBundleConfigViaExecutor(ctx, ex, dir)
}

// VmStateFromBundleConfig extracts a domain's persisted VmDeployState (the "vm:"+entity key) from
// a per-host deploy overlay BundleConfig — the pure lookup ResolveVmStateViaExecutor applies.
func VmStateFromBundleConfig(dc *deploykit.BundleConfig, entity string) *spec.VmDeployState {
	if dc == nil {
		return nil
	}
	if entry, ok := dc.Bundle["vm:"+entity]; ok {
		return entry.VmState
	}
	return nil
}

// ResolveVmStateViaExecutor reads a domain's persisted VmDeployState (instance-id, ssh_port, disk
// path) from the per-host deploy overlay PLUGIN-SIDE — the cycle-free replacement for the deleted
// "config-resolve" HostBuild seam's VmState leg (K-wave 2 cone R2 bank D). Three plugins consume it
// (candy/plugin-vm's hostConfigResolve, candy/plugin-deploy-vm's resolvePriorVmState,
// candy/plugin-kube's deployVMForwards), so it lives here once (R3). A miss or an unreadable
// overlay degrades to nil, matching the former seam's own swallow.
func ResolveVmStateViaExecutor(ctx context.Context, ex *sdk.Executor, entity string) (*spec.VmDeployState, error) {
	dir := hostBundleConfigDir()
	if dir == "" {
		return nil, nil
	}
	dc, err := LoadBundleConfigViaExecutor(ctx, ex, dir)
	if err != nil {
		return nil, nil
	}
	return VmStateFromBundleConfig(dc, entity), nil
}

// ResolveLifecycleDeployNodeViaExecutor is the drop-in replacement for the deleted
// deploykit.ResolveLifecycleDeployNodeViaSeam: it resolves the per-host deploy overlay entry for
// a start/stop/shell/cmd/logs/service verb PLUGIN-SIDE, threading the DATA a command:pod /
// command:cmd plugin passes into the single pod-lifecycle HostBuild request
// (spec.PodLifecycleRequest.Node, #55 W3 A10b) so the host's dispatchLifecycleTarget operates on
// the passed *spec.Deploy instead of re-reading the per-host config itself.
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
