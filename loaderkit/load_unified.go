// load_unified.go — the K1 keystone (task #24 unit 2) port of charly's TOP-LEVEL
// LoadUnified ORCHESTRATION out of charly core. This is the entry point every
// command (build/deploy/check/validate) calls to load a project's charly.yml — the
// sequence of steps (bootstrap phase, early + post-merge schema gate, kind-blind
// walk, registry materialize, venue flatten, member fold, descent stamp, the
// validation chain) now lives here, kind-blind, exactly as charly/unified.go's
// former inline body did. Every step that touches the provider registry, the
// build-vocabulary plugins, or a standing K5-final-decision core file
// (deploy_members.go's foldMembers/validateMembers) is a SEAM CALLBACK the host
// supplies via LoadSeams — the same injected-seam pattern spec.WalkSeams/
// spec.MaterializeSeams already established (#46); LoadUnified itself never
// touches the registry. charly-core's own LoadUnified(dir) becomes a thin wrapper
// that builds a LoadSeams from its existing host-coupled functions and delegates
// here.
package loaderkit

import (
	"fmt"
	"path/filepath"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// LoadSeams bundles every registry-coupled or standing-core-resident step
// LoadUnified's orchestration calls out to. A nil field panics on use — the host
// wrapper (charly's LoadUnified, loader_threaded.go) is the SOLE constructor and always populates every
// field before calling LoadUnified.
type LoadSeams struct {
	// RunBootstrapPhase invokes every registered bootstrap-phase plugin
	// (sdk.PhaseBootstrap) on the raw root config bytes, returning the
	// (possibly transformed) bytes. A leg failure is a hard error — never a
	// silent fallback to the raw, un-bootstrapped bytes.
	RunBootstrapPhase func(data []byte) ([]byte, error)
	// WalkProject runs the kind-blind import/discover/namespace walk (the
	// registered spec.ProjectWalker, reached via the host's spec.WalkSeams) and
	// returns the generic spec.LoadedProject envelope — no materialize, no merge.
	WalkProject func(dir string, rootData []byte) (spec.LoadedProject, error)
	// MaterializeLoadedProject replays the host's per-document/per-namespace
	// MATERIALIZE + root-wins MERGE over the walk envelope, reconstructing merged
	// (registry kind-decode via the registered spec.Materializer).
	MaterializeLoadedProject func(lp *spec.LoadedProject, merged *spec.UnifiedFile, byID map[int64]*spec.UnifiedFile) error
	// FlattenVenuesByPosition stamps every plan step's execution venue from its
	// deploy-tree position and hoists member/child steps into the root Plan.
	FlattenVenuesByPosition func(uf *spec.UnifiedFile) error
	// FoldMembers copies every deploy node's `peer:` entries into the Deploy map
	// as top-level addressable entries. Its relocation (if any) is a FINAL/K5
	// decision (deploy_members.go) — it stays host-resident, reached only via
	// this seam.
	FoldMembers func(uf *spec.UnifiedFile) error
	// StampDeployDescents stamps every deploy node's venue-hop descent
	// descriptor (P9 DeployTraits, resolved from the provider registry).
	StampDeployDescents func(uf *spec.UnifiedFile)
	// ValidateEphemeral auto-promotes disposable:true on ephemeral entries and
	// validates the ephemeral / vm-naming invariants.
	ValidateEphemeral func(uf *spec.UnifiedFile) error
	// ValidateCheckBeds enforces the kind:check bed invariants (disposable,
	// cross-ref, external-substrate recognition via the provider registry).
	ValidateCheckBeds func(uf *spec.UnifiedFile) error
	// ValidateAndroidDevices enforces the kind:android box⊻adb XOR (resolves
	// android templates via the plugin-substrate provider).
	ValidateAndroidDevices func(uf *spec.UnifiedFile) error
	// ValidateMembers enforces the member-specific invariants beyond the generic
	// deploy validation. Paired with FoldMembers under the same FINAL/K5 ruling.
	ValidateMembers func(uf *spec.UnifiedFile) error
	// ValidatePreemptible validates preemptible/requires_exclusive/requires_shared
	// across the deploy map, including the resource-vocabulary cross-check
	// (resolves the `resource:` plugin kind via the provider registry).
	ValidatePreemptible func(uf *spec.UnifiedFile) error
}

// GateSchemaVersion enforces the load-time schema-version contract: a config
// NEWER than this binary supports → "update charly"; an OLDER/absent/non-CalVer
// version → the `charly migrate` hint. Shared by the early pre-parse gate (root's
// raw version) and the post-merge gate (merged version) so both speak identically.
// Pure — kit.ParseCalVer/kit.LatestSchemaVersion carry no registry coupling.
func GateSchemaVersion(root, version string) error {
	fileVer, verOK := kit.ParseCalVer(version)
	switch {
	case verOK && kit.LatestSchemaVersion().Less(fileVer):
		return fmt.Errorf(
			"%s: config schema %s is newer than this charly supports (max %s). Update charly (reinstall the latest opencharly package, or run 'task build:binary' from a fresh checkout and use ./bin/charly)",
			root, version, kit.LatestSchemaVersion(),
		)
	case !verOK || fileVer.Less(kit.LatestSchemaVersion()):
		return fmt.Errorf(
			"%s: schema %s is required (found %q). Run: charly migrate",
			root, kit.LatestSchemaVersion(), version,
		)
	}
	return nil
}

// LoadUnified reads <dir>/charly.yml and returns the fully loaded, validated
// *spec.UnifiedFile — the kind-blind orchestration ported verbatim from charly's
// former inline LoadUnified body. Every registry-coupled or standing-core-
// resident step is reached through seams; LoadUnified itself never imports or
// touches the provider registry.
func LoadUnified(dir string, seams LoadSeams) (*spec.UnifiedFile, bool, error) {
	root := filepath.Join(dir, spec.UnifiedFileName)
	// THE CONFIG STACK: read the layered charly.yml files (system → user →
	// in-dir, later files winning) and merge them at the raw document level.
	// The merged bytes ARE the root document — the bootstrap phase, the early
	// schema gate, the kind-blind walk, and the validation chain all operate on
	// the merged config (config_stack.go). A missing system/user layer is
	// skipped; no layer at all means no project.
	rootData, ok, err := readConfigStack(dir)
	if err != nil {
		return nil, true, err
	}
	if !ok {
		return nil, false, nil
	}
	// F9 BOOTSTRAP PHASE: invoke bootstrap-phase plugins on the RAW merged root
	// config bytes FIRST — before the early schema gate AND before the walk — so
	// a bootstrap plugin's rewrite reaches both. The transformed bytes are
	// threaded into the walk as the root override so it PARSES them.
	bootstrapped, berr := seams.RunBootstrapPhase(rootData)
	if berr != nil {
		return nil, true, fmt.Errorf("bootstrap phase: %w", berr)
	}
	rootData = bootstrapped
	// EARLY schema-version gate: a below-HEAD (or absent) merged `version:` is
	// rejected with the `charly migrate` hint BEFORE any shape parsing — so an
	// out-of-date config never reaches node-form CUE validation. The merged
	// version is the LATER layer's (project > user > system).
	var vdoc yaml.Node
	if yaml.Unmarshal(rootData, &vdoc) == nil {
		ver := ""
		if vn := kit.MapValue(kit.MappingRoot(&vdoc), "version"); vn != nil {
			ver = vn.Value
		}
		if err := GateSchemaVersion(root, ver); err != nil {
			return nil, true, err
		}
	}
	// THE KIND-BLIND WALK: import queue + discover + namespaced-import mounts +
	// per-document parse → a generic spec.LoadedProject. No materialize, no
	// merge — those are the registry-coupled host half below (boundary law).
	lp, err := seams.WalkProject(dir, rootData)
	if err != nil {
		return nil, true, err
	}
	// MATERIALIZE + root-wins MERGE (host, registry kind-decode) → the typed
	// *spec.UnifiedFile, exactly as the former inline loadUnifiedInto did.
	merged := &spec.UnifiedFile{}
	if err := seams.MaterializeLoadedProject(&lp, merged, map[int64]*spec.UnifiedFile{}); err != nil {
		return nil, true, err
	}
	if err := GateSchemaVersion(root, merged.Version); err != nil {
		return nil, true, err
	}
	// Stamp each plan step's execution VENUE from its deploy-tree position and
	// hoist member/child steps into the root deploy's flat Plan. MUST run before
	// FoldMembers (which mutates the Deploy map by promoting members to
	// top-level) and before ValidateCheckBeds (which counts the root Plan's
	// check: steps). After this, both runner entry points read the venue-stamped
	// root Plan.
	if err := seams.FlattenVenuesByPosition(merged); err != nil {
		return nil, true, fmt.Errorf("%s: %w", root, err)
	}
	// Fold sibling members (companion deployments) into the Deploy map as
	// addressable top-level entries (inheriting the owner's disposability) so
	// the SAME deploy verbs bring them up/down. Runs BEFORE the deployment-tree
	// validation (so folded members get the same deploy validation).
	// Agent-provisioned members are SKIPPED by FoldMembers (the AI deploys them
	// in-run).
	if err := seams.FoldMembers(merged); err != nil {
		return nil, true, fmt.Errorf("%s: %w", root, err)
	}
	// Stamp every deploy node's venue-hop descent-descriptor (the descent
	// de-type) — uniformly here, after ALL structural kinds have folded into
	// merged.Deploy, so the kernel's deploy chain descends by TRANSPORT and
	// never switches on the substrate kind word.
	seams.StampDeployDescents(merged)
	// LOAD/FINALIZE DEFAULTS FILL (F5.2): the ephemeral → disposable:true promotion
	// (ephemeral implies destroy-and-rebuild authorization) runs here, once, at
	// finalize time — BEFORE the validators, which are read-only. Ordering matters:
	// ValidateEphemeral / ValidateCheckBeds / ValidateMembers all read IsDisposable().
	fillEphemeralDefaults(merged)
	if err := seams.ValidateEphemeral(merged); err != nil {
		return nil, true, fmt.Errorf("%s: %w", root, err)
	}
	if err := spec.ValidateDeploymentTree(merged.Deploy); err != nil {
		return nil, true, fmt.Errorf("%s: %w", root, err)
	}
	if err := seams.ValidateCheckBeds(merged); err != nil {
		return nil, true, fmt.Errorf("%s: %w", root, err)
	}
	if err := seams.ValidateAndroidDevices(merged); err != nil {
		return nil, true, fmt.Errorf("%s: %w", root, err)
	}
	if err := seams.ValidateMembers(merged); err != nil {
		return nil, true, fmt.Errorf("%s: %w", root, err)
	}
	if err := seams.ValidatePreemptible(merged); err != nil {
		return nil, true, fmt.Errorf("%s: %w", root, err)
	}
	return merged, true, nil
}
