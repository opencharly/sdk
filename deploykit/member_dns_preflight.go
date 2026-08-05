package deploykit

// member_dns_preflight.go — the cross-member container-DNS preflight BringUpMembers runs once
// every member of a group is up.
//
// Why it exists: a group whose members address each other by container name (the ${HOST:<member>}
// form — e.g. check-cross-pod-cdp's chrome driver fetching http://${HOST:web}:8080) depends on
// podman's aardvark-dns resolving that name on the shared charly network. aardvark can survive in
// a STALE rootless netns — podman's pidfile conflates "the process is alive" with "it is serving
// the CURRENT netns" — and while it does, NO container name resolves host-wide even though every
// container still starts perfectly normally.
//
// Without this preflight that host fault lands on the probe steps, which burn their entire retry
// budget on an unresolvable name and then report a CHECK failure (exit 2) — i.e. "the thing under
// test is broken". It is not; the host's DNS is. Failing here instead classifies it as INFRA
// (exit 1), because BringUpMembers runs inside the deploy step, and names the recovery command
// rather than leaving an operator to rediscover it.
//
// Scope is deliberately PRECISE rather than blanket: only the (member -> sibling) pairs a member's
// own plan actually references get probed. A group that never does cross-member addressing pays
// nothing and can never be failed by this. The port-bearing form ${HOST:<member>:<port>} is
// excluded — that resolves to a host-vantage 127.0.0.1 endpoint and never touches container DNS.
//
// Deliberately NOT auto-recovering: reloading podman's networks would mutate host-wide podman
// state from inside a bed, disrupting every unrelated running container including a concurrent
// bed's. That is a policy decision of its own, so this reports and stops.

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// dnsRecoveryCommand is the operator-facing remediation for a stale-aardvark netns.
const dnsRecoveryCommand = "podman network reload --all"

// resolveNameFromMember reports whether `name` resolves from inside member `memberKey`'s venue.
// A package var so tests can substitute a fake resolver, mirroring the proc.RunCharlySubcommand
// stubbing seam the bring-up routing tests already use.
var resolveNameFromMember = func(memberKey, name string) error {
	// getent is glibc-resident, so it is present wherever the container has a libc at all —
	// unlike ss/pgrep/nc, which minimal images routinely omit. `charly cmd` is the CLI's own
	// in-venue exec surface, which keeps this inside the charly-CLI-only mandate.
	out, err := charlyCmdCapture(memberKey, "getent hosts "+name)
	if err != nil {
		if detail := strings.TrimSpace(out); detail != "" {
			return fmt.Errorf("%w: %s", err, detail)
		}
		return err
	}
	return nil
}

// resolveContainerForPreflight maps a member name to its container name, confirming en route that
// the member is actually running. A package var for the same stubbing reason as above —
// ResolveContainer consults the live container engine.
var resolveContainerForPreflight = func(name, instance string) (string, error) {
	_, container, err := ResolveContainer(name, instance)
	return container, err
}

// charlyCmdCapture runs `charly cmd <member> <script>` and CAPTURES its combined output. It does
// not go through proc.RunCharlySubcommand, which streams to the parent's stdio and so cannot hand
// back the resolver's own output for the diagnostic.
func charlyCmdCapture(memberKey, script string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	out, err := exec.Command(exe, "cmd", memberKey, script).CombinedOutput()
	return string(out), err
}

// memberDNSRefs returns, per container-venue member key, the sorted set of SIBLING member names
// that member's own plan addresses by container DNS — the ${HOST:<name>} form with no :<port>.
func memberDNSRefs(node *spec.BundleNode) map[string][]string {
	if node == nil || len(node.Members) == 0 {
		return nil
	}
	refs := map[string][]string{}
	for memberKey, member := range node.Members {
		if member == nil || !spec.IsContainerVenue(member) {
			continue
		}
		ops := make([]spec.Op, 0, len(member.Plan))
		for _, st := range member.Plan {
			ops = append(ops, st.Op)
		}
		seen := map[string]bool{}
		var names []string
		for _, key := range kit.CollectHostRefs(ops) {
			_, arg, ok := kit.SplitHostKey(key)
			if !ok {
				continue
			}
			// A :<port> segment selects the host-vantage endpoint form, which bypasses
			// container DNS entirely.
			if strings.Contains(arg, ":") {
				continue
			}
			// Only a SIBLING member is reachable by this name over the shared charly net; a
			// self-reference or a name that is not a member is not this preflight's business.
			if arg == memberKey || node.Members[arg] == nil {
				continue
			}
			if !seen[arg] {
				seen[arg] = true
				names = append(names, arg)
			}
		}
		if len(names) > 0 {
			sort.Strings(names)
			refs[memberKey] = names
		}
	}
	return refs
}

// preflightMemberDNS verifies that every cross-member container-DNS name a member's plan
// references actually resolves from that member's venue, BEFORE any probe spends its retry budget
// discovering otherwise.
func preflightMemberDNS(node *spec.BundleNode) error {
	refs := memberDNSRefs(node)
	if len(refs) == 0 {
		return nil
	}
	memberKeys := make([]string, 0, len(refs))
	for k := range refs {
		memberKeys = append(memberKeys, k)
	}
	sort.Strings(memberKeys)

	for _, memberKey := range memberKeys {
		for _, sibling := range refs[memberKey] {
			container, err := resolveContainerForPreflight(sibling, "")
			if err != nil {
				return fmt.Errorf("cross-member DNS preflight: member %q addresses ${HOST:%s}, but that member is not resolvable: %w", memberKey, sibling, err)
			}
			if err := resolveNameFromMember(memberKey, container); err != nil {
				return fmt.Errorf(
					"cross-member DNS preflight: member %q cannot resolve sibling %q (container %q) on the shared charly network: %w\n"+
						"  Container DNS is served by aardvark-dns, which can survive in a STALE rootless netns; while it does,\n"+
						"  NO container name resolves host-wide even though every container starts normally. Recover with:\n"+
						"      %s",
					memberKey, sibling, container, err, dnsRecoveryCommand)
			}
		}
	}
	return nil
}
