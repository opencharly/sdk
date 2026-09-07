package deploykit

import (
	"fmt"

	"github.com/opencharly/sdk/kit"
)

// deploy_config_cycle.go — THE ONE locked read-modify-write cycle over the per-host deploy overlay
// (~/.config/charly/charly.yml).
//
// WHY THIS EXISTS (the lost-update race). The overlay is a single shared file that every concurrent
// charly process writes: `charly config`, `charly deploy add/import/reset`, `charly vm create`, the
// ephemeral registrar, the bed runner. deploykit.SaveDeployConfig is a WHOLE-FILE write — its
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
// THE CONTRACT. MutateDeployConfig acquires the flock FIRST, re-reads the overlay INSIDE the lock,
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

// DeployConfigMutator mutates a FRESH DeployConfig read under the deploy-config lock. It reports
// whether it changed anything: false skips the write entirely (so a no-op decision costs a read,
// not a whole-file rewrite). dc is never nil and dc.Deploy is never nil — MutateDeployConfig
// self-heals both before calling, so a mutation writes `dc.Deploy[key] = entry` unconditionally.
type DeployConfigMutator func(dc *DeployConfig) (changed bool, err error)

// MutateDeployConfig runs ONE locked read-modify-write cycle over the per-host deploy overlay and
// returns the fresh config the mutation ran against, so an in-memory caller can adopt it as its new
// view instead of continuing on its stale snapshot.
//
// read is the caller's overlay reader (a plugin passes its loader-backed
// loaderkit.LoadHostDeployConfigViaExecutor; an in-proc host caller passes LoadDeployConfig). save
// is the caller's persist callback (a SaveDeployConfig closure carrying that caller's node-form
// marshal). Both are injected for the SAME reason every other write path injects them: the
// deploy-kind-specific marshal and the placement-specific read are the caller's responsibility,
// and this shell stays kind-blind.
//
// The lock is BLOCKING: a config write is brief, so a concurrent writer waits rather than failing.
func MutateDeployConfig(read func() (*DeployConfig, error), save func(dc *DeployConfig) error, mutate DeployConfigMutator) (*DeployConfig, error) {
	if read == nil {
		return nil, fmt.Errorf("MutateDeployConfig: read callback is nil")
	}
	if save == nil {
		return nil, fmt.Errorf("MutateDeployConfig: save callback is nil")
	}
	if mutate == nil {
		return nil, fmt.Errorf("MutateDeployConfig: mutate callback is nil")
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
	dc = ensureDeployConfig(dc)
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
// around a cycle MutateDeployConfig cannot express — a write path that also removes the file
// (CleanDeployEntry, `charly deploy reset`) and must decide save-vs-remove under the same hold.
// Every ordinary writer uses MutateDeployConfig instead.
func AcquireDeployConfigLock() (func() error, error) {
	path, err := kit.DefaultDeployConfigPath()
	if err != nil {
		return nil, fmt.Errorf("determining deploy config path for lock: %w", err)
	}
	return kit.AcquireFileLock(path+".lock", true)
}

// ensureDeployConfig self-heals a nil config / nil Deploy map into a usable empty overlay — the
// state a first-ever `charly config` on a fresh XDG-isolated bed sees. Every write path repeated
// this three-line dance; it lives here once (R3).
func ensureDeployConfig(dc *DeployConfig) *DeployConfig {
	if dc == nil {
		dc = &DeployConfig{}
	}
	if dc.Deploy == nil {
		dc.Deploy = make(map[string]DeployNode)
	}
	return dc
}
