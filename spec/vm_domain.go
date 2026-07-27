package spec

import "strings"

// VmDomainIdentity normalizes a deploy/bundle name into the per-deploy VM DOMAIN IDENTITY — the
// token that keys the libvirt domain (charly-<identity>), the per-domain state dir, the managed
// ssh-config alias, and the ssh-port ledger entry (vm:<identity>). It is DISTINCT from the kind:vm
// ENTITY (the disk/spec source, resolved via the deploy's `from:` cross-ref): several distinct beds
// may share one entity, so keying the domain by the ENTITY collided them on one libvirt domain +
// one disk + one host SSH port. Keying by the DEPLOY NAME instead makes distinct beds collision-free
// by construction (each gets its own domain, per-deploy disk overlay, and auto-allocated port).
//
// The normalization strips a leading "vm:" prefix and flattens the instance ("/") and nested-path
// (".") separators to "-", so a bare bed name maps to itself (check-builder-vm → check-builder-vm),
// a bundle ref maps to its VM token (vm:arch → arch), and a direct `charly vm create <entity>` (whose
// domain identity IS the entity) is unchanged. Both the host preresolver and candy/plugin-deploy-vm
// call this on the SAME deploy name, so the domain they derive always agrees.
//
// Home note (FLOOR-legal leaf): lives in package spec — the always-floor-legal
// wire/vocabulary leaf — so kernel-floor charly files (host_build_check_bed.go)
// derive the domain identity without a vmshared import. stdlib-only (strings).
func VmDomainIdentity(deployName string) string {
	id := strings.TrimPrefix(deployName, "vm:")
	id = strings.ReplaceAll(id, "/", "-")
	id = strings.ReplaceAll(id, ".", "-")
	return id
}
