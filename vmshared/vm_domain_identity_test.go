package vmshared

import "testing"

// TestVmDomainIdentity pins the deploy-name → per-deploy DOMAIN IDENTITY mapping that makes eval VM
// beds collision-free by construction (P33): a bare bed name maps to itself (distinct beds sharing
// one kind:vm entity therefore get distinct domains), a "vm:" bundle ref drops the prefix, and the
// instance ("/") and nested-path (".") separators flatten to "-". Without the separation this
// function encodes, sibling beds referencing one entity would collide on one libvirt domain + disk +
// host ssh port — the exact regression this cutover removes.
func TestVmDomainIdentity(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"check-builder-vm", "check-builder-vm"}, // bare bed name → itself (distinct per bed)
		{"check-substrate", "check-substrate"},   // a sibling bed sharing the same entity → distinct id
		{"vm:arch", "arch"},                      // direct bundle ref: drop the vm: prefix
		{"vm:arch/prod", "arch-prod"},            // instance suffix flattens to "-"
		{"stack.myvm", "stack-myvm"},             // nested dotted path flattens to "-"
		{"eval-host-vm", "eval-host-vm"},         // a plain entity-shaped name → itself (direct create parity)
	}
	for _, c := range cases {
		if got := VmDomainIdentity(c.in); got != c.want {
			t.Errorf("VmDomainIdentity(%q) = %q; want %q", c.in, got, c.want)
		}
	}
	// Two distinct beds sharing one entity MUST map to distinct domain identities — the property that
	// makes them collision-free.
	if VmDomainIdentity("check-builder-vm") == VmDomainIdentity("check-substrate") {
		t.Fatal("sibling beds sharing an entity collapsed to one domain identity — collision reintroduced")
	}
}
