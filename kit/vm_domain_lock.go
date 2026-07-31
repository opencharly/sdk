package kit

// vm_domain_lock.go — re-export of the per-libvirt-domain host contention locks for a check
// bed, RELOCATED to the spec/lock fabric slice github.com/opencharly/spec/lock/bed_vm_domain.go
// (#55 CHECK-ENGINE cone Option A — the bed-session lock family charly core's check-bed session
// seam (host_build_check_bed.go) reaches importing zero kit). Pure over an already-LOADED
// (loader-stamped) spec.BundleNode: it reads node.Descent directly rather than falling back to
// a registry-backed resolver — a check bed's node always comes from LoadUnified. kit re-exports
// the symbols here so every existing kit.BedVmDomains / kit.AcquireVmDomainLock call site
// (charly core + plugins) is untouched. New consumers should import spec/lock directly.

import "github.com/opencharly/spec/lock"

// BedVmDomains returns the sorted, deduped libvirt domain names (charly-<from>) a bed's VM(s)
// occupy. Re-exported from lock.BedVmDomains (the body lives there).
var BedVmDomains = lock.BedVmDomains

// AcquireVmDomainLock takes a BLOCKING, host-global advisory lock serializing every check bed
// that occupies the given libvirt domain. Re-exported from lock.AcquireVmDomainLock.
var AcquireVmDomainLock = lock.AcquireVmDomainLock