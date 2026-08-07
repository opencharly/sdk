package deploykit

import (
	"fmt"

	"github.com/opencharly/sdk/kit"
)

// deploy_config_cycle.go — THE ONE locked read-modify-write cycle over the per-host deploy overlay
// (~/.config/charly/charly.yml).
//
// WHY THIS EXISTS (the lost-update race). The overlay is a single shared file that every concurrent
// charly process writes: `charly config`, `charly fleet add/import/reset`, `charly vm create`, the
// ephemeral registrar, the bed runner. deploykit.SaveFleetConfig is a WHOLE-FILE write — its
// tempfile+rename makes a reader never see a torn file, but it does NOT prevent a LOST UPDATE: a
// writer that loaded the config at T0 and saves at T1 silently discards every entry any other
// process wrote in between. Four writers already guarded that window with the process-shared flock
// (SaveDeployState / CleanDeployEntry inline, SaveVmDeployState / RemoveVmDeployEntry via an
// injected callback) and three candies each carried their OWN identical lock helper; the
// candy/plugin-deploy-pod config-setup path and candy/plugin-fleet's import/reset/ephemeral writes
// carried NONE. Observed consequences on a 32-bed concurrent roster: overlay `resolved_image` refs
// lost (two beds deployed their BASE image), a RELEASED exclusive arbiter claim resurrected by a
// stale write-back, and failed beds' entries vanishing from the overlay entirely.
//
// THE CONTRACT. MutateFleetConfig acquires the flock FIRST, re-reads the overlay INSIDE the lock,
// and runs the caller's mutation against THAT FRESH COPY. Every save is therefore a merge-on-latest,
// never a write-back of a snapshot the caller loaded earlier. This is what makes it safe for a
// caller whose own orchestration spans minutes (`charly config` resolves ports, prompts for
// encryption passphrases, provisions volume data between its load and its writes): the caller holds
// the lock only for the duration of its MUTATION, not for its orchestration, because the mutation is
// expressed as a FUNCTION OVER FRESH STATE rather than a pre-computed config to write back.
//
// COROLLARY FOR AUTHORS: anything the mutation's outcome depends on must be COMPUTED INSIDE the
// closure, not before it. The motivating case is port allocation — `kit.ResolveDeployPorts` picks a
// free host port against `OccupiedHostPorts(dc, key)`, so computing it outside the lock against a
// stale dc can hand two concurrent deploys the same host port even though the file write itself is
// serialized.
//
// R3: this is the SINGLE cycle shell. SaveDeployState, CleanDeployEntry, SaveVmDeployState,
// RemoveVmDeployEntry, candy/plugin-deploy-pod's config-setup writes and candy/plugin-fleet's
// import/reset/ephemeral writes all route through it; the two private per-candy lock helpers
// (plugin-fleet's and plugin-vm's — plugin-deploy-pod had none, which is how it shipped with no
// lock at all) are deleted. A new overlay writer adds a mutation closure here, never a fourth
// lock copy.

// FleetConfigMutator mutates a FRESH FleetConfig read under the deploy-config lock. It reports
// whether it changed anything: false skips the write entirely (so a no-op decision costs a read,
// not a whole-file rewrite). dc is never nil and dc.Fleet is never nil — MutateFleetConfig
// self-heals both before calling, so a mutation writes `dc.Fleet[key] = entry` unconditionally.
type FleetConfigMutator func(dc *FleetConfig) (changed bool, err error)

// MutateFleetConfig runs ONE locked read-modify-write cycle over the per-host deploy overlay and
// returns the fresh config the mutation ran against, so an in-memory caller can adopt it as its new
// view instead of continuing on its stale snapshot.
//
// read is the caller's overlay reader (a plugin passes its loader-backed
// loaderkit.LoadHostFleetConfigViaExecutor; an in-proc host caller passes LoadFleetConfig). save
// is the caller's persist callback (a SaveFleetConfig closure carrying that caller's node-form
// marshal). Both are injected for the SAME reason every other write path injects them: the
// deploy-kind-specific marshal and the placement-specific read are the caller's responsibility,
// and this shell stays kind-blind.
//
// The lock is BLOCKING: a config write is brief, so a concurrent writer waits rather than failing.
func MutateFleetConfig(read func() (*FleetConfig, error), save func(dc *FleetConfig) error, mutate FleetConfigMutator) (*FleetConfig, error) {
	if read == nil {
		return nil, fmt.Errorf("MutateFleetConfig: read callback is nil")
	}
	if save == nil {
		return nil, fmt.Errorf("MutateFleetConfig: save callback is nil")
	}
	if mutate == nil {
		return nil, fmt.Errorf("MutateFleetConfig: mutate callback is nil")
	}
	unlock, err := AcquireDeployConfigLock()
	if err != nil {
		return nil, err
	}
	defer func() { _ = unlock() }()

	dc, err := read()
	if err != nil {
		return nil, fmt.Errorf("reading charly.yml for update: %w", err)
	}
	dc = ensureFleetConfig(dc)
	changed, err := mutate(dc)
	if err != nil {
		return dc, err
	}
	if !changed {
		return dc, nil
	}
	if err := save(dc); err != nil {
		return dc, err
	}
	return dc, nil
}

// AcquireDeployConfigLock takes the process-shared blocking flock that serializes the
// read-modify-write of the per-host deploy overlay. Exported because two callers need the lock
// around a cycle MutateFleetConfig cannot express — a write path that also removes the file
// (CleanDeployEntry, `charly fleet reset`) and must decide save-vs-remove under the same hold.
// Every ordinary writer uses MutateFleetConfig instead.
func AcquireDeployConfigLock() (func() error, error) {
	path, err := kit.DefaultDeployConfigPath()
	if err != nil {
		return nil, fmt.Errorf("determining deploy config path for lock: %w", err)
	}
	return kit.AcquireFileLock(path+".lock", true)
}

// ensureFleetConfig self-heals a nil config / nil Fleet map into a usable empty overlay — the
// state a first-ever `charly config` on a fresh XDG-isolated bed sees. Every write path repeated
// this three-line dance; it lives here once (R3).
func ensureFleetConfig(dc *FleetConfig) *FleetConfig {
	if dc == nil {
		dc = &FleetConfig{}
	}
	if dc.Fleet == nil {
		dc.Fleet = make(map[string]FleetNode)
	}
	return dc
}
