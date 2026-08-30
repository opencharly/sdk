package kit

import (
	"os"
	"path/filepath"
)

// VmKnownHostsFile answers ONE question — which known_hosts file this VM's managed ssh alias
// records into — for every writer of that alias.
//
// It exists because there are TWO writers of the same stanza and they disagreed:
// plugin-vm's `vm create` (publishVmSshAlias) and plugin-deploy-vm's prepare-venue. Each
// publishes the alias; only the first carried the installer-guest policy. prepare-venue runs
// AFTER `vm create` on a rebuild — whenever a port is already persisted — so it silently
// overwrote the correct stanza with one that records a host key again, and the guest became
// permanently unreachable a few minutes later.
//
// That is why the policy is a FUNCTION and not an `if` at each call site: a policy expressed
// twice is a policy that will differ once (R3). Adding the same guard to the second writer
// would have fixed this instance and left the next one exactly as available.
//
// THE POLICY. A guest installed from an installer ISO changes its SSH host identity exactly
// once, and no amount of polling can survive it:
//
//  1. the live installer environment brings up its own sshd with freshly generated host keys
//     (Omarchy's ISO has cloud-init do this — visible on its console as "root@archiso"),
//  2. the first thing to connect pins that key under `accept-new`,
//  3. the installer finishes and the guest reboots into the INSTALLED system, whose host keys
//     are entirely different,
//  4. every connection after that fails with "Host key verification failed", so the readiness
//     gate burns its whole cap and the deploy never starts.
//
// Whether step 2 happens at all is a RACE against the install finishing, which is why this
// presents as an intermittent failure: the first deploy of a domain can win the race and a
// later rebuild lose it, on the same seed and the same disk.
//
// /dev/null rather than a relaxed StrictHostKeyChecking: `accept-new` still applies, so the
// first connection is accepted exactly as before and nothing is ever RECORDED to conflict
// with. What is given up is host-key continuity for a per-domain loopback guest whose login
// key charly generated itself — weighed against a guest that is otherwise unusable.
//
// Every other source kind gets a real per-domain known_hosts, because their identity is
// stable from first boot and continuity is worth having.
func VmKnownHostsFile(sourceKind, stateDir string) string {
	if sourceKind == "iso" {
		return os.DevNull
	}
	return filepath.Join(stateDir, "known_hosts")
}
