package deploykit

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/vmshared"
	"gopkg.in/yaml.v3"
)

// bed_session.go — the check-bed HOST helpers relocated from charly/check_bed_run.go
// (Cutover B unit 6b, the InvokeProvider-generalization family). Every caller of these was
// ALREADY core-only (host_build_check_bed.go, bundle_members.go — no plugin calls them
// directly today), so this is the SAME "portable orchestration in sdk, thin core call sites"
// pattern already applied to the credential family: no provider-registry coupling was ever
// load-bearing here except ONE classification — isExternalDeploySubstrate (a live
// provider-registry query) — which the caller now computes ONCE and threads in as a plain
// `externalInPlace bool` parameter (the team-lead's ruling: a plain Go parameter, not a new
// wire surface — there is no cross-process consumer for this data today, so adding one would
// be exactly the "seam a later wave deletes" anti-pattern). The HostRooted trait check
// (formerly nodeTraits(&node).HostRooted, itself only a provider-registry fallback for an
// UNSTAMPED node) reads node.Descent directly instead — every node these functions see comes
// from a LoadUnified'd project, whose loader always stamps Descent (stampBundleDescents), so
// this is a pure wire-field read, never a registry query.

// hostRooted reports whether node's stamped descent trait is host-rooted (a local/SSH-shell
// venue, as opposed to a container/VM venue). Reads the wire-stamped node.Descent directly —
// see the file header for why this needs no registry access.
func hostRooted(node *BundleNode) bool {
	return node != nil && node.Descent != nil && node.Descent.HostRooted
}

// ResolveBedCheckLevel resolves the acceptance-depth rung for a bed. hasResolvedBox is true
// iff the bed's root carries an `image:` AND that image resolved to a box config; checkLevel
// is that box's authored check_level (ignored when hasResolvedBox is false). VM / local beds
// carry no box image, so they always run at the default rung — the caller (which alone can
// resolve a box ref against the loaded project) computes hasResolvedBox/checkLevel and calls
// this pure classifier.
func ResolveBedCheckLevel(hasResolvedBox bool, checkLevel string) string {
	if !hasResolvedBox {
		return kit.DefaultCheckLevel
	}
	return kit.ResolveCheckLevel(checkLevel)
}

// PersistBedDeployOverrides seeds the per-host charly.yml with a kind:check bed's
// project-declared deploy-shaped fields (port / volume / env / tunnel / security / network),
// its disposable/lifecycle classification, AND its resource-arbitration role (preemptible
// holder / requires_exclusive / requires_shared claimant), BEFORE the bed's `charly config`
// step runs. Seeding the arbiter fields is what lets a bed/deploy MEMBER be an arbiter
// participant: bringUpMembers persists each member here, then the member's `charly start`
// reloads the per-host node and fires the arbiter off these fields — without them a member's
// requires_exclusive reloaded as [] and the arbiter silently no-op'd. The folded bed node is
// the source of truth, but `charly bundle add` / `charly config` otherwise source those fields
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
func PersistBedDeployOverrides(name string, node BundleNode, externalInPlace bool, marshalNode func(name string, node *BundleNode) (*yaml.Node, error)) {
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
	// place during `charly bundle add`), so the whole reason PersistBedDeployOverrides exists
	// — seeding port/volume/env overrides BEFORE config — does not apply. Worse, a local bed's
	// only persistable cross-ref is its `local:` template, which lives in the bed's OWN
	// project; writing it into the GLOBAL per-host overlay makes that overlay un-loadable from
	// every OTHER project (validateCheckBeds: "references local template … which is not
	// defined"), poisoning concurrent/cross-project bed runs. Local deploys persist via the
	// install ledger, not this bundle-map path, so skipping is also lossless.
	if hostRooted(&node) || externalInPlace {
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
	}, marshalNode)
}

// DeployNestedLocalChildren deploys a VM's nested target:local children via the dotted-path
// dispatch, which applies each child's local-deploy candies INSIDE the guest over the
// NestedExecutor (SSH).
//
// plugin-deploy-vm's PostApply brings up nested target:pod children as in-guest quadlets, but
// it SKIPS target:local children — they carry no image, they apply candies in place. Without
// this loop a nested local child never deploys, and a deploy-scope check against it either
// fails or (worse) silently checks nothing.
//
// Both sites that own a VM venue call this: the isVM bed ROOT and bringUpMembers' VM-member
// branch. They differ only in how a child deploy is executed (the root wraps it in a recorded
// step(); a member shells out directly), so that is the injected apply func.
func DeployNestedLocalChildren(parent string, children map[string]*BundleNode, apply func(childKey, dotted string) error) error {
	for _, childKey := range SortedNestedKeys(children) {
		child := children[childKey]
		if child == nil || !hostRooted(child) { // local (host-rooted shell venue) only
			continue // container/vm children handled in-guest by plugin-deploy-vm's PostApply
		}
		if err := apply(childKey, parent+"."+childKey); err != nil {
			return fmt.Errorf("deploy nested local child %s.%s: %w", parent, childKey, err)
		}
	}
	return nil
}

// WaitForVmSshReady gates on the VM being SSH-reachable AND cloud-init having settled, using
// the SAME deterministic SSHExecutor preflight the VM check-live path and the external vm
// deploy walk run — NOT a fixed sleep. WaitForSSH polls until sshd answers; WaitForCloudInit
// retries until an ssh connection survives a `cloud-init status` poll (the deterministic
// cloud-init-settled signal — so deploy-add never races a still-running first-boot pacman).
// domainID is the per-deploy DOMAIN IDENTITY (the bed/member deploy name), not the shared
// kind:vm entity — the alias the create path published. Best-effort: silent on timeout (the
// downstream deploy-add surfaces the real error).
func WaitForVmSshReady(domainID string) {
	gate := &kit.SSHExecutor{Host: kit.VmSshAlias(domainID), ConnectTimeout: 5}
	ctx := context.Background()
	if err := gate.WaitForSSH(ctx); err != nil {
		return
	}
	_ = gate.WaitForCloudInit(ctx)
}

// WaitForContainerReady gates on the container being exec-able AND its supervisord-managed
// children having left their transitional states, so a one-shot check-live port/service probe
// never races a child that has not yet bound. `charly start` returns when systemd reports the
// service active, but supervisord's autostart children are still STARTING for a moment after.
// This polls `supervisorctl status` until no child is STARTING/BACKOFF (a child binds its port
// the instant it reaches RUNNING) instead of sleeping a fixed, host-tuned interval. Images
// without supervisord settle immediately. Best-effort: silent on timeout (the next check-live
// step surfaces the real failure). Reads the project's readiness bounds via
// kit.ReadinessProvider() — the SAME plugin-portable channel every other executor wait uses
// (the host threads its resolved bounds via ResolvedReadiness.PluginEnv; a project-unaware
// caller falls back to kit's built-in defaults).
func WaitForContainerReady(bed string) {
	containerName := "charly-" + bed
	// supervisorStatus reports __NOSUP__ when the image has no supervisorctl, so
	// "no supervisord" is distinguishable from "socket not up yet".
	const supervisorStatus = `command -v supervisorctl >/dev/null 2>&1 || { echo __NOSUP__; exit 0; }; supervisorctl status 2>&1`
	// MONOTONIC readiness via the unified pollUntil primitive: the progress marker is the
	// count of SETTLED children — it climbs as children reach RUNNING, so a slow startup
	// under heavy parallel load is waited for (the no-progress watchdog resets on each new
	// settled child); a child crash-looping back to BACKOFF drops the count below its
	// high-water, so the watchdog correctly does NOT treat the flap as progress and the bed
	// stalls out instead of hiding the fault. Best-effort: silent on stall/cap (the next
	// check-live step surfaces the real failure).
	cfg := kit.ReadinessProvider().Wait("container-ready "+bed, vmshared.PollLocal)
	_ = vmshared.PollUntil(context.Background(), cfg, func(actx context.Context) (bool, float64, error) {
		if exec.CommandContext(actx, "podman", "exec", containerName, "true").Run() != nil {
			return false, 0, nil // container not exec-able yet
		}
		out, _ := exec.CommandContext(actx, "podman", "exec", containerName, "sh", "-c", supervisorStatus).CombinedOutput()
		if bytes.Contains(out, []byte("__NOSUP__")) {
			return true, 0, nil // no supervisord — nothing to settle
		}
		settled := float64(bytes.Count(out, []byte("RUNNING")) + bytes.Count(out, []byte("STOPPED")) +
			bytes.Count(out, []byte("EXITED")) + bytes.Count(out, []byte("FATAL")))
		if bytes.Contains(out, []byte("STARTING")) || bytes.Contains(out, []byte("BACKOFF")) {
			return false, settled, nil // children still coming up
		}
		if settled > 0 {
			return true, settled, nil // supervisord answered + nothing transitional
		}
		return false, 0, nil // supervisord control socket not up yet
	})
}
