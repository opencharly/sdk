package kit

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/opencharly/sdk/spec"
	"github.com/opencharly/sdk/vmshared"
)

// vm_domain_lock.go — the per-libvirt-domain host contention lock for a check bed, moved from
// charly/check_bed_run.go (CHECK-wave bed-session spike). Pure over an already-LOADED (loader-
// stamped) spec.Deploy tree: it reads node.Descent directly rather than falling back to the
// registry-backed deployTraitsFor resolver charly/deploy_tree.go's nodeTraits uses for a
// SYNTHETIC (un-stamped) node — a check bed's node always comes from LoadUnified, so it is
// always stamped, and the registry fallback path never fires for this caller. A caller holding a
// possibly-synthetic node must NOT use this pair; it needs the registry-aware core nodeTraits.

// BedVmDomains returns the sorted, deduped libvirt domain names (charly-<from>) a bed's VM(s)
// occupy — the bed's own vm target plus any group-member vm targets. This is the unit of
// exclusive host contention two DISTINCT beds can collide on (the per-domain lock in
// AcquireVmDomainLock serializes them).
func BedVmDomains(name string, node spec.BundleNode) []string {
	seen := map[string]bool{}
	var out []string
	add := func(domainID string) {
		if domainID == "" {
			return
		}
		dom := "charly-" + domainID
		if seen[dom] {
			return
		}
		seen[dom] = true
		out = append(out, dom)
	}
	if node.Descent != nil && node.Descent.Venue == "ssh" { // vm (ssh venue) root
		add(vmshared.VmDomainIdentity(name))
	}
	for memberKey, m := range node.Members {
		if m != nil && m.Descent != nil && m.Descent.Venue == "ssh" {
			add(vmshared.VmDomainIdentity(memberKey))
		}
	}
	sort.Strings(out)
	return out
}

// AcquireVmDomainLock takes a BLOCKING, host-global advisory lock serializing every check bed
// that occupies the given libvirt domain. Host-global (under ~/.cache/charly/.locks/) because
// the qemu:///session domain namespace is host-wide, shared across project dirs.
func AcquireVmDomainLock(domain string) (func() error, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".cache", "charly", ".locks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return AcquireFileLock(filepath.Join(dir, "vm-domain-"+domain+".lock"), true)
}
