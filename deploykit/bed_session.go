package deploykit

import (
	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// bed_session.go — the check-bed HOST helpers relocated from charly/check_bed_run.go
// (Cutover B unit 6b, the InvokeProvider-generalization family). At relocation time the caller
// was core-only; candy/plugin-check/bed_persist.go was the first plugin to call
// PersistBedDeployOverrides directly, and it remains the sole caller through #55 W3 B2-full's
// complete check-bed dissolution — core held no direct caller of this file across the whole
// migration. This is the SAME "portable orchestration in sdk, thin core call sites" pattern
// already applied to the credential family: no provider-registry coupling was ever
// load-bearing here except ONE classification — isExternalDeploySubstrate (a live
// provider-registry query) — which the caller now computes ONCE and threads in as a plain
// `externalInPlace bool` parameter (the team-lead's ruling: a plain Go parameter, not a new
// wire surface — there is no cross-process consumer for this data today, so adding one would
// be exactly the "seam a later wave deletes" anti-pattern). The HostRooted trait check
// (formerly nodeTraits(&node).HostRooted, itself only a provider-registry fallback for an
// UNSTAMPED node) reads node.Descent directly instead — every node these functions see comes
// from a LoadUnified'd project, whose loader always stamps Descent (stampFleetDescents), so
// this is a pure wire-field read, never a registry query.
//
// ResolveBedCheckLevel (formerly here, a thin wrapper over kit.DefaultCheckLevel/
// kit.ResolveCheckLevel) DIED as dead code (#55 W3 B2-full, R1 sweep): its sole caller — core's
// check_bed_run.go's bedCheckLevel — already documented having inlined to the spec.* helpers
// directly ("now inlined to those spec helpers... importing zero deploykit"); the relocated
// plugin-side bedCheckLevel (candy/plugin-check/bed_session.go) follows the SAME already-inlined
// pattern, never calling back into this wrapper. Zero remaining callers, confirmed by grep.

// The host-rooted descent predicate is now spec.HostRooted (#55 U4 — a pure #Deploy-tree read
// over the wire-stamped node.Descent, promoted so DeployNestedLocalChildren and this file's
// bed-session apply path share ONE predicate over the spec value type). Callers below reference
// spec.HostRooted directly.

// PersistBedDeployOverrides seeds the per-host charly.yml with a kind:check bed's
// project-declared deploy-shaped fields (port / volume / env / tunnel / security / network),
// its disposable/lifecycle classification, AND its resource-arbitration role (preemptible
// holder / requires_exclusive / requires_shared claimant), BEFORE the bed's `charly config`
// step runs. Seeding the arbiter fields is what lets a bed/deploy MEMBER be an arbiter
// participant: bringUpMembers persists each member here, then the member's `charly start`
// reloads the per-host node and fires the arbiter off these fields — without them a member's
// requires_exclusive reloaded as [] and the arbiter silently no-op'd. The folded bed node is
// the source of truth, but `charly fleet add` / `charly config` otherwise source those fields
// from the IMAGE LABELS and gate port writes behind an operator `-p` — so a bed's declared
// `port:` remap would never reach the quadlet (it would fall back to the image default and
// collide with any same-image deploy already bound to that port). Seeding the per-host entry
// up front lets the existing MergeDeployOntoMetadata → quadlet path honor the overrides with
// no new merge logic; `charly config`'s own SetPorts-gated save then leaves the seeded port
// untouched (it passes no `-p`). SaveDeployState's per-field guards make unset bed fields
// no-ops, so this is safe for beds that declare only a subset.
//
// externalInPlace is the caller-computed isExternalDeploySubstrate(node.Target) result — this
// package cannot query the live provider registry itself (see file header).
//
// read is the current-state re-read this load-mutate-save performs. A nil read falls back to
// SaveDeployState's own DeployStateHost-backed read (the in-proc host path). A plugin caller
// (out-of-process command:check / command:fleet) injects its OWN loader-backed reader
// (loaderkit.LoadHostFleetConfigViaExecutor), so PersistBedDeployOverrides no longer requires the
// DeployStateHost package var (#55 coneC-dsh — mirrors the SaveFleetConfig/SaveDeployState
// reader-callback precedent).
func PersistBedDeployOverrides(name string, node FleetNode, externalInPlace bool, marshalNode func(name string, node *FleetNode) (*yaml.Node, error), read func() (*FleetConfig, error)) {
	// A GROUP bed (boxless root + sibling Members — the §3 cross-deployment shape) has NO
	// root deployment to seed: its members each carry their own port/volume/env overrides
	// (bringUpMembers persists every member), and the boxless root is never `charly config`'d.
	// Persisting the group root here would write a MEMBERLESS bed (no box, no members —
	// SaveDeployState carries no member fields) that validateCheckBeds then HARD-REJECTS on
	// the next overlay load ("no workload cross-ref and no sibling members"), poisoning every
	// subsequent SaveDeployState. So never persist a group bed root.
	if node.IsGroup() {
		return
	}
	// A LOCAL or EXTERNAL in-place bed never runs `charly config` (it applies candies in
	// place during `charly fleet add`), so the whole reason PersistBedDeployOverrides exists
	// — seeding port/volume/env overrides BEFORE config — does not apply. Worse, a local bed's
	// only persistable cross-ref is its `local:` template, which lives in the bed's OWN
	// project; writing it into the GLOBAL per-host overlay makes that overlay un-loadable from
	// every OTHER project (validateCheckBeds: "references local template … which is not
	// defined"), poisoning concurrent/cross-project bed runs. Local deploys persist via the
	// install ledger, not this fleet-map path, so skipping is also lossless.
	if spec.HostRooted(&node) || externalInPlace {
		return
	}
	SaveDeployState(name, "", SaveDeployStateInput{
		Ports:         node.Port,
		SetPorts:      len(node.Port) > 0,
		Volume:        node.Volume,
		Env:           node.Env,
		CleanEnv:      true,
		Tunnel:        node.Tunnel,
		Security:      node.Security,
		Network:       node.Network,
		Box:           node.Image,
		Target:        node.Target,
		SetDisposable: true,
		Disposable:    node.IsDisposable(),
		SetLifecycle:  node.Lifecycle != "",
		Lifecycle:     node.Lifecycle,
		// Resource-arbitration role — so a group MEMBER (holder / claimant) can actually
		// drive the arbiter after its `charly start` reloads this entry.
		Preemptible:       node.Preemptible,
		RequiresExclusive: node.RequiresExclusive,
		RequiresShared:    node.RequiresShared,
	}, marshalNode, read)
}

// DeployNestedLocalChildren is now spec.DeployNestedLocalChildren (#55 U4 — a pure dotted-path
// tree walk over the spec value types, promoted with HostRooted). deploykit keeps a re-export
// forwarder (deploy_fleet_ops_aliases.go) so its charly callers compile unchanged.

// WaitForVmSshReady + WaitForContainerReady (the deploy-venue readiness GATES) moved to
// spec/exec (spec/exec/venue_wait.go, #55 K4) — pure process-driving pollers over spec/exec
// executors + spec readiness primitives, with no bed-session coupling. candy/plugin-check's bed
// runner calls specexec.WaitFor* directly.
//
// CORRECTION to this file's own former prediction (#55 W3 B2-full): PersistBedDeployOverrides
// genuinely STAYS here — a plain library function any placement can call (ResolveBedCheckLevel,
// its sibling, turned out to have zero remaining callers and was deleted as dead code, above).
// But the flock/lease/env "lock/lease/persist family" this header once predicted would ALSO land
// in deploykit landed somewhere more specific instead: directly in
// candy/plugin-check/bed_session.go, NOT here. The reason is placement-class, not
// process-boundary: os.Setenv(CHARLY_REPO_OVERRIDE/…) only lands in the process
// hostBuildCli's cli-reentry children fork from when the CALLER is compiled-in — a fact true of
// candy/plugin-check specifically, not of "any deploykit importer" generically. Putting that
// state management HERE would have silently implied it's safe for an out-of-process placement,
// which it is not (see bed_session.go's own PLACEMENT CLASS note). A kit is the right home for
// placement-agnostic logic; this family is placement-COUPLED, so it belongs in the one compiled-in-
// required consumer, not the shared kit.
