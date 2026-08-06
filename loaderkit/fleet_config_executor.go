package loaderkit

// fleet_config_executor.go — the cycle-free plugin-side replacement for the deleted
// sdk/deploykit/load_fleet_config_seam.go (#55 coneC Unit C2). The former
// deploykit.LoadFleetConfigViaSeam reached the per-host deploy overlay by calling BACK to
// charly's "pod-config-load-fleet" HostBuild host handler (which called
// deploykit.LoadFleetConfig — the DeployStateHost-seam path). That seam is now retired:
// loaderkit already imports deploykit (fleet_load.go et al.), so a helper LIVING IN loaderkit
// can call LoadUnifiedViaExecutor + deploykit.ProjectFleetConfig DIRECTLY — cycle-free — and a
// plugin (which can import both loaderkit + deploykit) calls this helper instead of round-tripping
// to the host. deploykit itself CANNOT host this helper (it cannot import loaderkit — the cycle),
// so the plugin-reachable overlay read lives HERE, in the kit that already depends on deploykit.
//
// Placement-invariant: works identically compiled-in or out-of-process (LoadUnifiedViaExecutor
// drives the registry-coupled LoadSeams over the reverse channel when out-of-process, in-proc when
// compiled-in). Byte-equivalent to the former LoadFleetConfigViaSeam: same per-host overlay read,
// same non-nil &FleetConfig{}-on-absent/empty contract (inherited from
// deploykit.LoadFleetConfig's line-112 wrap), same ProjectFleetConfig projection the host
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

// LoadFleetConfigViaExecutor loads <dir>/charly.yml through the unified loader PLUGIN-SIDE (over
// the reverse channel when out-of-process) and projects it to a *deploykit.FleetConfig — the
// cycle-free, placement-invariant overlay/project read. dir is the config directory (the per-host
// overlay dir for a deploy read; a project dir for a project read). An ABSENT or EMPTY overlay
// returns a NON-NIL &deploykit.FleetConfig{} — matching deploykit.LoadFleetConfig's contract
// (deploy_file.go line 112: a present-but-empty config returns &FleetConfig{} so callers that
// range/index dc.Deploy without a nil guard keep working after an overlay's last entry is
// removed); the former LoadFleetConfigViaSeam round-tripped to LoadFleetConfig and inherited
// the same non-nil-empty wrap..
func LoadFleetConfigViaExecutor(ctx context.Context, ex *sdk.Executor, dir string) (*deploykit.FleetConfig, error) {
	if ex == nil {
		return nil, fmt.Errorf("load fleet config via executor: no host reverse channel (command not compiled-in?)")
	}
	uf, ok, err := LoadUnifiedViaExecutor(ctx, ex, dir)
	if err != nil {
		return nil, fmt.Errorf("load fleet config via executor: %w", err)
	}
	if !ok || uf == nil {
		// Absent/empty overlay → non-nil &FleetConfig{}, matching deploykit.LoadFleetConfig's
		// line-112 wrap (deploy_file.go: a present-but-empty config returns &FleetConfig{} so
		// callers that range/index dc.Deploy without a nil guard keep working). The K5 rewiring
		// returned nil here, dropping the wrap — R1 RCA: that short-circuited reap-orphans' orphan
		// scan in a bed's XDG_CONFIG_HOME-isolated empty overlay ("no charly.yml; nothing to
		// reap" instead of the expected "no orphaned ephemerals"); restored.
		return &deploykit.FleetConfig{}, nil
	}
	return deploykit.ProjectFleetConfig(uf), nil
}

// hostFleetConfigDir returns the per-host deploy-overlay config directory
// (filepath.Dir(kit.DefaultDeployConfigPath)) — the dir LoadFleetConfigViaSeam reached
// indirectly through the host handler's deploykit.LoadFleetConfig. Empty string when the path
// can't be resolved (the read then degrades to a non-nil &FleetConfig{}, matching
// deploykit.LoadFleetConfig's absent/empty contract).
func hostFleetConfigDir() string {
	path, err := kit.DefaultDeployConfigPath()
	if err != nil {
		return ""
	}
	return filepath.Dir(path)
}

// LoadHostFleetConfigViaExecutor is the drop-in replacement for the deleted
// deploykit.LoadFleetConfigViaSeam(ctx, ex, caller): it reads the PER-HOST deploy overlay
// (~/.config/charly/charly.yml) plugin-side via LoadFleetConfigViaExecutor + the derived per-host
// config dir. The former `caller` label was host-side diagnostics only (threaded into the wire
// request); this path does not round-trip to a host handler, so no caller label is carried.
// Returns a NON-NIL &FleetConfig{} on an absent/empty overlay (matching
// deploykit.LoadFleetConfig's absent-file contract); (nil, nil) only when the config path
// itself can't be resolved (the dir=="" guard, matching LoadFleetConfig's path-error nil).
func LoadHostFleetConfigViaExecutor(ctx context.Context, ex *sdk.Executor) (*deploykit.FleetConfig, error) {
	dir := hostFleetConfigDir()
	if dir == "" {
		return nil, nil
	}
	return LoadFleetConfigViaExecutor(ctx, ex, dir)
}

// VmStateFromFleetConfig extracts a domain's persisted VmDeployState (the "vm:"+entity key) from
// a per-host deploy overlay FleetConfig — the pure lookup ResolveVmStateViaExecutor applies.
func VmStateFromFleetConfig(dc *deploykit.FleetConfig, entity string) *spec.VmDeployState {
	if dc == nil {
		return nil
	}
	if entry, ok := dc.Fleet["vm:"+entity]; ok {
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
	dir := hostFleetConfigDir()
	if dir == "" {
		return nil, nil
	}
	dc, err := LoadFleetConfigViaExecutor(ctx, ex, dir)
	if err != nil {
		return nil, nil
	}
	return VmStateFromFleetConfig(dc, entity), nil
}

// ResolveLifecycleDeployNodeViaExecutor is the drop-in replacement for the deleted
// deploykit.ResolveLifecycleDeployNodeViaSeam: it resolves the per-host deploy overlay entry for
// a start/stop/shell/cmd/logs/service verb PLUGIN-SIDE, threading the DATA a command:pod /
// command:cmd plugin passes into the single pod-lifecycle HostBuild request
// (spec.PodLifecycleRequest.Node, #55 W3 A10b) so the host's dispatchLifecycleTarget operates on
// the passed *spec.Deploy instead of re-reading the per-host config itself.
//
// Byte-identical to the former core resolver: the dc.Fleet[key] lookup keyed by DeployKey, the
// container/""→pod Target normalization, and the {Target:pod} fallback for a bare image with no
// deploy entry (the former standalone-podman path). Returns (node, deployKey); node is never nil.
func ResolveLifecycleDeployNodeViaExecutor(ctx context.Context, ex *sdk.Executor, box, instance string) (*spec.Deploy, string) {
	key := spec.DeployKey(box, instance)
	if dc, _ := LoadHostFleetConfigViaExecutor(ctx, ex); dc != nil {
		if node, ok := dc.Fleet[key]; ok {
			n := node
			if n.Target == "" || n.Target == "container" {
				n.Target = "pod"
			}
			return &n, key
		}
	}
	return &spec.Deploy{Target: "pod"}, key
}
