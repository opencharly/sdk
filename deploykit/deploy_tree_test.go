package deploykit

import "testing"

// TestVmChildExecutor_PerDeployAlias pins the P33 fix: a nested child under a vm bed must SSH to the
// per-deploy domain alias (charly-<deploy>), NOT the shared kind:vm entity. This is the R10 regression
// where two beds sharing `vm: {from: eval-vm}` both derived the entity alias `charly-eval-vm` and the
// nested-local-child hop failed with `Could not resolve hostname charly-eval-vm` (no such stanza — the
// live stanzas were the per-deploy `charly-check-builder-vm` / `charly-check-substrate`).
func TestVmChildExecutor_PerDeployAlias(t *testing.T) {
	cases := []struct{ deploy, wantHost string }{
		{"check-substrate", "charly-check-substrate"},   // bed root → its OWN domain, not charly-eval-vm
		{"check-builder-vm", "charly-check-builder-vm"}, // sibling bed on the same entity → distinct
		{"stack.vm-member", "charly-stack-vm-member"},   // dotted member path flattens like the domain
		{"vm:arch", "charly-arch"},                      // bundle ref drops the vm: prefix
	}
	for _, c := range cases {
		exe, err := VmChildExecutor(nil, c.deploy) // nil parent → a plain SSHExecutor at the root
		if err != nil {
			t.Fatalf("VmChildExecutor(nil, %q): %v", c.deploy, err)
		}
		ssh, ok := exe.(*SSHExecutor)
		if !ok {
			t.Fatalf("VmChildExecutor(nil, %q) = %T; want *SSHExecutor", c.deploy, exe)
		}
		if ssh.Host != c.wantHost {
			t.Errorf("VmChildExecutor(nil, %q).Host = %q; want %q (per-deploy domain, not the entity)", c.deploy, ssh.Host, c.wantHost)
		}
	}
	// The load-bearing collision-free property: two sibling beds sharing ONE entity map to DISTINCT hosts.
	a, _ := VmChildExecutor(nil, "check-builder-vm")
	b, _ := VmChildExecutor(nil, "check-substrate")
	if a.(*SSHExecutor).Host == b.(*SSHExecutor).Host {
		t.Fatal("sibling beds sharing an entity collapsed to one ssh alias — collision reintroduced")
	}
}
