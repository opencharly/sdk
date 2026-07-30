package loaderkit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/opencharly/sdk"
	"github.com/opencharly/sdk/deploykit"
	"github.com/opencharly/spec/spec"
)

// load_via_executor.go — the ONE shared, placement-invariant "run the whole project loader
// PLUGIN-SIDE over the reverse channel" helper. It is the executor-backed loaderkit.LoaderExecutor
// witness that FOUR plugin candies (candy/plugin-bundle, candy/plugin-build, candy/plugin-vm,
// candy/plugin-loader) each carried a private near-identical copy of (an `execLoaderExecutor`
// struct + a `LoadUnified(dir, LoadSeamsFromExecutor(...))` call). loaderkit is the correct home:
// it ALREADY imports the root sdk package (resolve_via_executor.go's Resolve*ViaExecutor take a
// *sdk.Executor) and owns the LoaderExecutor interface + LoadSeamsFromExecutor + LoadUnified +
// UnmarshalMaterialized themselves — so this adds no new import direction (sdk root does NOT import
// loaderkit; deploykit does NOT import loaderkit). Same R3 consolidation as
// deploykit.LoadBundleConfigViaSeam (the pod-config-load-bundle seam), blessed by the charly#176
// round-1 pr-validator: "an sdk kit is EXACTLY the mechanism this project uses to share code across
// plugin module boundaries."
//
// The four core legs dispatch each registry-/host-coupled loader step over sdk.Executor.HostBuild to
// charly's "loader-*" host legs (charly/host_build_loader_floor.go); the two capability validators
// self-serve plugin-side over the same-package Resolve*ViaExecutor InvokeProvider callbacks. Pure
// consolidation — byte-identical to the per-candy copies (same seam kinds, same nil/degradation
// contract). New callers (candy/plugin-deploy-pod's deploy-key→box project fallback) use this helper;
// the remaining per-candy copies are tracked R3 debt migrating onto it.

// executorLoaderExecutor implements LoaderExecutor by dispatching each registry-coupled loader step
// over sdk.Executor.HostBuild to charly's "loader-*" host legs.
type executorLoaderExecutor struct {
	ctx context.Context
	ex  *sdk.Executor
}

// LoaderThreaded returns the CURRENT registry snapshot (∅ → spec.Threaded). No error return in the
// interface: a (never-observed) HostBuild error degrades to the zero snapshot, matching the host's
// own empty-registry semantics.
func (e *executorLoaderExecutor) LoaderThreaded() spec.Threaded {
	var t spec.Threaded
	if out, err := e.ex.HostBuild(e.ctx, "loader-threaded", nil); err == nil {
		_ = json.Unmarshal(out, &t)
	}
	return t
}

// RunBootstrapPhase runs the host's bootstrap-phase plugins over the raw root bytes ([]byte→[]byte).
// A leg failure returns the bytes unchanged (the no-op-seam contract).
func (e *executorLoaderExecutor) RunBootstrapPhase(data []byte) []byte {
	if out, err := e.ex.HostBuild(e.ctx, "loader-bootstrap", data); err == nil {
		return out
	}
	return data
}

// WalkProject runs the host's kind-blind import/discover/namespace walk (spec.LoaderWalkRequest →
// spec.LoadedProject).
func (e *executorLoaderExecutor) WalkProject(dir string, rootData []byte) (spec.LoadedProject, error) {
	reqJSON, err := json.Marshal(spec.LoaderWalkRequest{Dir: dir, RootData: rootData})
	if err != nil {
		return spec.LoadedProject{}, err
	}
	out, err := e.ex.HostBuild(e.ctx, "loader-walk", reqJSON)
	if err != nil {
		return spec.LoadedProject{}, err
	}
	var lp spec.LoadedProject
	if err := json.Unmarshal(out, &lp); err != nil {
		return spec.LoadedProject{}, fmt.Errorf("loader-walk: decode reply: %w", err)
	}
	return lp, nil
}

// MaterializeLoadedProject runs the host MATERIALIZE + root-wins MERGE (spec.LoadedProject → the
// merged spec.UnifiedFile). The host leg owns its OWN byID scratch; the plugin passes none and
// decodes the reply into merged via UnmarshalMaterialized (the PluginKinds json:"-" out-of-band
// carry, so the reconstructed merged is byte-identical to host-side).
func (e *executorLoaderExecutor) MaterializeLoadedProject(lp *spec.LoadedProject, merged *spec.UnifiedFile, _ map[int64]*spec.UnifiedFile) error {
	reqJSON, err := json.Marshal(lp)
	if err != nil {
		return err
	}
	out, err := e.ex.HostBuild(e.ctx, "loader-materialize", reqJSON)
	if err != nil {
		return err
	}
	if err := UnmarshalMaterialized(out, merged); err != nil {
		return fmt.Errorf("loader-materialize: decode reply: %w", err)
	}
	return nil
}

// ValidateAndroidDevices runs the kind:android box⊻adb XOR validator PLUGIN-SIDE, threading the
// android resolve callback over InvokeProvider(kind, "local", OpResolve) — no host round-trip.
func (e *executorLoaderExecutor) ValidateAndroidDevices(uf *spec.UnifiedFile) error {
	return ValidateAndroidDevices(uf, ResolveAndroidViaExecutor(e.ctx, e.ex))
}

// ValidatePreemptible runs the preemptible/requires_exclusive/requires_shared validator PLUGIN-SIDE,
// threading the resource/vm resolve callbacks over InvokeProvider(kind, OpResolve) — no host round-trip.
func (e *executorLoaderExecutor) ValidatePreemptible(uf *spec.UnifiedFile) error {
	return ValidatePreemptible(uf, ResolveResourceViaExecutor(e.ctx, e.ex), ResolveVmViaExecutor(e.ctx, e.ex))
}

// LoadUnifiedViaExecutor drives LoadUnified PLUGIN-SIDE over the reverse channel: the PURE LOAD-half
// seams run in-plugin and the registry-/host-coupled legs dispatch to charly's "loader-*" host legs
// via ex.HostBuild. dir is the project directory (a plugin obtains it from the
// "deploy-plugins-connect" host seam's DeployPluginsConnectReply.Dir). Returns the fully-merged,
// validated project the SAME way the compiled-in host loader (charly.LoadUnified) does.
func LoadUnifiedViaExecutor(ctx context.Context, ex *sdk.Executor, dir string) (*spec.UnifiedFile, bool, error) {
	if ex == nil {
		return nil, false, fmt.Errorf("load unified via executor: no host reverse channel (command not compiled-in?)")
	}
	return LoadUnified(dir, LoadSeamsFromExecutor(&executorLoaderExecutor{ctx: ctx, ex: ex}))
}

// ResolveMergedTreeViaExecutor is the PLUGIN-SIDE twin of charly-core's resolveTreeRoot
// (deploy_tree.go): the merged project+operator deploy-node tree, ready for dotted-path traversal.
// Byte-for-byte resolveTreeRoot's composition — the PROJECT config via LoadUnifiedViaExecutor +
// deploykit.ProjectBundleConfig, the per-host operator overlay via deploykit.LoadBundleConfigViaSeam
// (the placement-invariant "pod-config-load-bundle" seam read — a plugin CANNOT call the bare
// deploykit.LoadBundleConfig, which silently no-ops outside charly-core's own init per the
// DeployStateHost placement class), merged root-wins via deploykit.MergeDeployConfigs. Returns
// (nil, nil) on an absent/empty project+overlay, matching resolveTreeRoot. This is the shared
// resolver the #55 Cone A Unit 3a seams (deploy-del-resolve / pod-config-project-volume / the check
// venue+gather host helpers) call so their host handlers stop re-loading the tree via the core
// resolveTreeRoot — the tree-threading that lets deploy_tree.go's resolveTreeRoot ultimately DIE.
func ResolveMergedTreeViaExecutor(ctx context.Context, ex *sdk.Executor, dir string) (map[string]spec.BundleNode, error) {
	if ex == nil {
		return nil, fmt.Errorf("resolve merged tree via executor: no host reverse channel (command not compiled-in?)")
	}
	var projectDC *deploykit.BundleConfig
	if uf, ok, err := LoadUnifiedViaExecutor(ctx, ex, dir); err != nil {
		return nil, err
	} else if ok && uf != nil {
		projectDC = deploykit.ProjectBundleConfig(uf)
	}
	localDC, _ := deploykit.LoadBundleConfigViaSeam(ctx, ex, "resolve merged tree")
	merged := deploykit.MergeDeployConfigs(projectDC, localDC)
	if merged == nil || merged.Bundle == nil {
		return nil, nil
	}
	return merged.Bundle, nil
}
