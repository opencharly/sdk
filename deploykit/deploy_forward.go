package deploykit

// deploy_forward.go — pure VM port-forward resolution + kubeconfig
// server-URL rewriting, moved from charly/k3s_post.go (Cutover B unit 5,
// P13-KERNEL-B). Both functions are pure: no loader, no registry, no I/O —
// they only transform already-resolved data (an authored port_forwards list
// plus a persisted allocation map, or kubeconfig text plus a guest->host
// port map). The LoadUnified-coupled orchestration around them — resolving
// which VM entity/deploy a forward belongs to, reading the persisted
// allocation from the deploy-config ledger, deciding whether a deploy is a
// k3s-server deploy at all — stays host-side in charly/k3s_post.go:
// LoadUnified/materialize is K1-permanent core (the K1-spike finding stands,
// R-E2), so no further enabler moves that orchestration here.

import (
	"fmt"
	"regexp"

	"github.com/opencharly/sdk/vmshared"
)

// k3sServerURLRe matches an `https://<host>:<port>` server URL in a kubeconfig.
var k3sServerURLRe = regexp.MustCompile(`(https?://)([^:/\s]+):(\d+)`)

// RewriteServerPorts rewrites every `https://<host>:<guestPort>` in data to
// `https://127.0.0.1:<hostPort>` for each guest->host mapping (the QEMU
// user-mode forward lives on the host loopback). Pure; unit-tested.
func RewriteServerPorts(data string, guestToHost map[string]string) string {
	return k3sServerURLRe.ReplaceAllStringFunc(data, func(m string) string {
		p := k3sServerURLRe.FindStringSubmatch(m)
		if hport, ok := guestToHost[p[3]]; ok && hport != p[3] {
			return p[1] + "127.0.0.1:" + hport
		}
		return m
	})
}

// ResolveDeployForwards maps authored network.port_forwards entries to
// concrete "<host>:<guest>" strings: an `auto:<guest>` entry resolves to its
// persisted auto-allocated host port, and a fixed "<host>:<guest>" passes
// through unchanged. An `auto:<guest>` with NO persisted allocation is a
// LOUD ERROR, never a silent drop: the caller runs this only
// POST-vm-create, where the allocation MUST exist, so a miss means a
// persist/read key mismatch — surfacing it here turns a confusing
// downstream `connection refused` into a diagnostic naming the unresolved
// entry (R1/R4). Pure; unit-tested. Reuses vmshared.SplitPortForward (R3)
// for the "<host>:<guest>" split, the same helper vmshared's own QEMU argv
// renderer uses.
func ResolveDeployForwards(authored []string, alloc map[string]int) ([]string, error) {
	out := make([]string, 0, len(authored))
	for _, pf := range authored {
		host, guest := vmshared.SplitPortForward(pf)
		if guest == "" {
			continue
		}
		if host == "auto" {
			h, hit := alloc[guest]
			if !hit || h <= 0 {
				return nil, fmt.Errorf("auto port_forward %q has no persisted host-port allocation (the vm-create allocation must exist post-create)", pf)
			}
			out = append(out, fmt.Sprintf("%d:%s", h, guest))
			continue
		}
		out = append(out, pf)
	}
	return out, nil
}
