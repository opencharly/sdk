package deploykit

// bundle_members.go — sibling `peer:` member bring-up/tear-down (#55 W3 A4, relocated from
// charly/bundle_members.go). A BundleNode's `peer:` map declares companion deployments brought
// up ALONGSIDE it on the shared `charly` network (NOT nested inside it) — members are reachable
// by `${HOST:<name>}` and are never check-live'd themselves.
//
// R3 3-way audit (before this move): candy/plugin-bundle/members_persist.go already does the
// ADJACENT per-member deploy-override PERSIST immediately before calling bring-up, and
// candy/plugin-check/venue.go independently re-implements a registry-free nodeTraits twin. The
// audit found the ACTUAL duplication risk was narrower than "the whole engine" — it was the
// venue-CLASSIFICATION predicate (isVmMember/isPodMember, formerly reading core's private
// nodeTraits): a naive lift would have added a FOURTH copy. Resolved by promoting
// spec.IsVmVenue/spec.IsContainerVenue (mirroring the already-promoted spec.HostRooted, #55 U4)
// as the ONE shared predicate — every node BringUpMembers/TearDownMembers sees comes from an
// already-loaded, Descent-stamped project (foldMembers marks only the folded top-level copy), so
// the registry-fallback branch core's OWN nodeTraits carried is dead weight here, exactly as it
// already was for candy/plugin-check's own registry-free twin.
//
// BringUpMembers/TearDownMembers shell out via spec/proc.RunCharlySubcommand (a `charly <verb>`
// re-entrant CLI call in the SAME process the caller runs in — NOT the HostBuild("cli")
// reverse-channel reentry an out-of-process plugin would need) and spec/hostenv +
// spec/exec's readiness gates — all plugin-importable fabric, no host-private state. Both
// consumers — the operator path (candy/plugin-bundle's walk, via the now-deleted
// "deploy-members-up"/"deploy-members-down" HostBuild seam) and the check-bed runner
// (candy/plugin-check, via host_build_check_bed.go's members-up/members-down ops) — call these
// directly now; candy/plugin-bundle already does (this commit). candy/plugin-check's bed-runner
// call sites relocate in the immediately-following unit (#55 W3 B2-full), which also retires the
// transitional core copy charly/bundle_members.go keeps for one commit cycle.
//
// TRANSITIONAL NOTE (dies with B2-full, the very next unit): charly/bundle_members.go still
// carries its OWN copy of BringUpMembers/TearDownMembers (unexported, unchanged) because
// host_build_check_bed.go's members-up/members-down ops call them as a same-package function —
// core cannot import sdk/deploykit (import-purity). B2-full deletes that core copy once
// candy/plugin-check calls THIS package directly instead, closing the transitional window in the
// same working session it opened in — never a permanent duplicate.

import (
	"errors"
	"fmt"

	specexec "github.com/opencharly/spec/exec"
	"github.com/opencharly/spec/hostenv"
	"github.com/opencharly/spec/proc"
	"github.com/opencharly/spec/spec"
)

// withMemberTag appends `--tag <imageTag>` to a member deploy argv when imageTag is non-empty (a
// bed run's per-run tag #75). Empty on the operator bring-up path, where members resolve their
// images normally. One appender (R3).
func withMemberTag(args []string, imageTag string) []string {
	if imageTag == "" {
		return args
	}
	return append(args, "--tag", imageTag)
}

// BringUpMembers brings up every member of `node` ALONGSIDE the (already-deployed) owner, in
// deterministic order, on the shared `charly` network. Each member is a folded top-level deploy
// entry, so bring-up reuses the standard pod pipeline verbatim: the caller persists the member's
// declared deploy overrides FIRST (so its declared `port:` actually publishes — `charly config`
// otherwise sources ports from image labels behind an operator -p — see
// candy/plugin-bundle/members_persist.go and candy/plugin-check's bed-member persist), then this
// function runs `charly config <member>` + `charly start <member>`, then waits for readiness. A
// VM member (target: vm) gets the full libvirt lifecycle (create + ssh-wait + deploy), a
// kind:local member is registered via `charly bundle add <member>`. The SAME helper serves the
// kind:check bed runner and the operator deploy path (R3). Idempotent on an already-running
// member.
func BringUpMembers(node *spec.BundleNode, imageTag string) error {
	if node == nil || len(node.Members) == 0 {
		return nil
	}
	for _, memberKey := range spec.SortedMemberKeys(node.Members) {
		memberNode := node.Members[memberKey]
		switch {
		case spec.IsVmVenue(memberNode):
			// VM member: full libvirt lifecycle, mirroring the isVM bed root
			// (check_bed_run.go). The VM disk is built by the caller's build step
			// (the group bed's build arm); here we (re)create + wait for ssh +
			// deploy the VM node — `bundle add <member> <vm-entity>` (the VM-template
			// ref, like the isVM root's deploy-add), not the bare pod/local form.
			// Best-effort pre-destroy clears a stale domain from an interrupted run.
			hostenv.StartLibvirtUserSession()
			// The member's libvirt domain is named after the MEMBER deploy (memberKey), not the
			// shared kind:vm entity (memberNode.From) — so member VMs sharing one entity across beds
			// get distinct, collision-free domains + per-domain disk overlays + ports (P33). The
			// entity is the disk/spec source (the `bundle add` ref); --domain names this member's domain.
			memberDomain := spec.VmDomainIdentity(memberKey)
			_ = proc.RunCharlySubcommand("vm", "destroy", memberNode.From, "--domain", memberDomain, "--if-exists")
			if err := proc.RunCharlySubcommand("vm", "create", memberNode.From, "--domain", memberDomain); err != nil {
				return fmt.Errorf("peer %q (vm create %s): %w", memberKey, memberNode.From, err)
			}
			specexec.WaitForVmSshReady(memberDomain)
			if err := proc.RunCharlySubcommand(withMemberTag([]string{"bundle", "add", memberKey, memberNode.From}, imageTag)...); err != nil {
				return fmt.Errorf("peer %q (vm bundle add): %w", memberKey, err)
			}
			// Same nested-local-child gap the isVM bed root closes: plugin-deploy-vm's
			// PostApply skips target:local children, so deploy them into the guest here.
			if err := spec.DeployNestedLocalChildren(memberKey, memberNode.Children, func(childKey, dotted string) error {
				return proc.RunCharlySubcommand("bundle", "add", dotted)
			}); err != nil {
				return fmt.Errorf("peer %q: %w", memberKey, err)
			}
		case spec.IsContainerVenue(memberNode):
			for _, step := range [][]string{{"config", memberKey}, {"start", memberKey}} {
				if err := proc.RunCharlySubcommand(withMemberTag(step, imageTag)...); err != nil {
					return fmt.Errorf("peer %q (%v): %w", memberKey, step, err)
				}
			}
			specexec.WaitForContainerReady(memberKey)
		default:
			// kind:local member — applies candies in place during bundle add.
			if err := proc.RunCharlySubcommand(withMemberTag([]string{"bundle", "add", memberKey}, imageTag)...); err != nil {
				return fmt.Errorf("peer %q (bundle add): %w", memberKey, err)
			}
		}
	}
	return nil
}

// TearDownMembers tears down every member of `node` in deterministic order — the companion to
// BringUpMembers. It attempts every member and returns their joined errors so callers can finish
// the full cleanup while still failing the owning operation.
func TearDownMembers(node *spec.BundleNode) error {
	if node == nil || len(node.Members) == 0 {
		return nil
	}
	var errs []error
	for _, memberKey := range spec.SortedMemberKeys(node.Members) {
		memberNode := node.Members[memberKey]
		var err error
		switch {
		case spec.IsVmVenue(memberNode):
			// `vm destroy` removes the libvirt domain (named after the MEMBER deploy, not the shared
			// entity — P33), but bring-up ALSO registered the member in the deploy ledger via
			// `bundle add`. Reverse that too, or a ledger record survives every teardown and they
			// accumulate run over run.
			destroyErr := proc.RunCharlySubcommand("vm", "destroy", memberNode.From, "--domain", spec.VmDomainIdentity(memberKey), "--if-exists")
			delErr := proc.RunCharlySubcommand(spec.BundleDelArgv(memberKey)...)
			err = errors.Join(destroyErr, delErr)
		case spec.IsContainerVenue(memberNode):
			err = proc.RunCharlySubcommand("remove", memberKey, "--purge")
		default:
			err = proc.RunCharlySubcommand(spec.BundleDelArgv(memberKey)...)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("peer %q teardown: %w", memberKey, err))
		}
	}
	return errors.Join(errs...)
}
