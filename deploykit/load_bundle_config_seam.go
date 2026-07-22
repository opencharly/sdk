package deploykit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/opencharly/sdk"
	"github.com/opencharly/sdk/spec"
)

// load_bundle_config_seam.go — the ONE shared placement-invariant read of the per-host deploy
// overlay via the "pod-config-load-bundle" HostBuild seam (bed-robustness batch, charly#176
// round-1 R3 fix).
//
// Four independent, near-identical copies of this exact "marshal spec.PodConfigLoadDeployRequest
// → ex.HostBuild(ctx, "pod-config-load-bundle", …) → unmarshal spec.PodConfigLoadBundleReply →
// unmarshal rep.ConfigJSON into a BundleConfig" sequence had accumulated across plugin module
// boundaries: candy/plugin-bundle/ephemeral.go, candy/plugin-pod/remove_orchestration.go
// (resolveSidecarNames), candy/plugin-status/nested_tree.go, and candy/plugin-substrate/
// status_flat.go — each independently re-deriving the SAME DeployStateHost
// out-of-process-read fix (deploykit.LoadBundleConfig() silently no-ops — empty result, no
// error — unless charly-core's own init() has populated the package var DeployStateHost, so an
// out-of-process plugin calling it directly gets a silent empty read). A fresh pr-validator
// review (charly#176 round 1) rejected the "plugin modules can't cross-import each other"
// justification for landing a 3rd and 4th copy in one cutover: every one of the four call sites
// already imports sdk/deploykit + sdk/spec + the root sdk package freely, and an sdk kit is
// EXACTLY the mechanism this project uses to share code across plugin module boundaries (R3).
// This file is the ONE shared implementation; all four call sites now delegate to
// LoadBundleConfigViaSeam instead of re-deriving the sequence.
//
// deploykit already imports the root "github.com/opencharly/sdk" package elsewhere
// (render_generator_from_project.go — sdk.Executor/HostBuild is the plugin-side reverse-channel
// handle, needed there for the identical reason) — this adds no new import cycle and no new
// dependency direction.
//
// Pure consolidation — byte-equivalent semantics: every call site's observable behavior is
// unchanged (same seam kind, same request/reply shape, same nil-on-empty contract, same error
// propagation on a HostBuild/decode failure). Only the caller-facing error TEXT is now emitted
// from one place instead of four near-identical copies; no test asserts on that text (verified:
// none of the four packages' tests match on an error string from this path — each documents that
// the seam-coupled load itself is not unit-testable without a live reverse channel and instead
// tests only the pure extraction/merge logic downstream of it).

// podConfigLoadBundleSeamKind names the HostBuild seam this function calls — the SAME wire kind
// string charly/host_build_pod_config_seams.go serves (hostBuildPodConfigLoadBundle). Owned here
// now instead of re-declared per call site, since the seam CALL itself is now owned here too.
const podConfigLoadBundleSeamKind = "pod-config-load-bundle"

// LoadBundleConfigViaSeam reads the per-host deploy overlay (~/.config/charly/charly.yml) via the
// "pod-config-load-bundle" HostBuild seam — placement-invariant: works identically whether the
// calling plugin is compiled-in or out-of-process, unlike LoadBundleConfig (deploy_file.go), which
// silently no-ops outside the process that ran charly-core's own init(). ex is the caller's
// already-resolved reverse-channel executor (a package-var stash for a single-command-per-process
// command plugin, or a per-call sdk.ExecutorForInvoke result for a multi-call verb provider) —
// this function does not resolve one itself, since HOW a caller obtains its executor is its own
// placement concern, not this seam's. caller is a short human-readable label (e.g.
// "candy/plugin-status nested tree") threaded into the wire request for host-side diagnostics,
// mirroring each call site's former Caller string. Returns (nil, nil) on an absent/empty overlay,
// matching LoadBundleConfig's own contract.
func LoadBundleConfigViaSeam(ctx context.Context, ex *sdk.Executor, caller string) (*BundleConfig, error) {
	if ex == nil {
		return nil, fmt.Errorf("load bundle config: no host reverse channel (command not compiled-in?)")
	}
	reqJSON, err := json.Marshal(spec.PodConfigLoadDeployRequest{Caller: caller})
	if err != nil {
		return nil, fmt.Errorf("load bundle config: marshal request: %w", err)
	}
	resJSON, err := ex.HostBuild(ctx, podConfigLoadBundleSeamKind, reqJSON)
	if err != nil {
		return nil, fmt.Errorf("load bundle config: %w", err)
	}
	var rep spec.PodConfigLoadBundleReply
	if err := json.Unmarshal(resJSON, &rep); err != nil {
		return nil, fmt.Errorf("load bundle config: decode reply: %w", err)
	}
	if len(rep.ConfigJSON) == 0 {
		return nil, nil
	}
	var dc BundleConfig
	if err := json.Unmarshal(rep.ConfigJSON, &dc); err != nil {
		return nil, fmt.Errorf("load bundle config: decode config: %w", err)
	}
	return &dc, nil
}
