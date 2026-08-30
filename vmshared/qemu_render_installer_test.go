package vmshared

import (
	"strings"
	"testing"
)

// TestRenderQemuArgv_InstallerIsoIsASecondCdromWithDiskFirstBoot proves the mechanism that
// makes an unattended ISO install TERMINATE.
//
// Both design passes on this feature assumed the installer had to be ejected or detached
// once it finished, and invented machinery to detect that moment. It does not: attaching
// the installer as a second cdrom and booting DISK FIRST is sufficient and self-limiting.
// An empty disk is not bootable, so the firmware falls through to the installer on the
// first boot; once the installer has written the disk, the disk boots and the ISO is never
// reached again. Both cdroms stay attached forever.
//
// The assertion that matters is `-boot order=cd`: c is the first hard disk and d the first
// cdrom, so "cd" means disk-then-cdrom. Reversed ("dc") the guest would re-enter the
// installer on every boot and reinstall over itself indefinitely.
func TestRenderQemuArgv_InstallerIsoIsASecondCdromWithDiskFirstBoot(t *testing.T) {
	spec := &VmSpec{Network: &VmNetwork{Mode: "user"}}
	rt := VmRuntimeParams{
		QCOW2Path:        "/state/omarchy-vm/disk.qcow2",
		SeedISOPath:      "/state/omarchy-vm/seed.iso",
		InstallerISOPath: "/cache/omarchy-4.0.1.iso",
	}
	args := strings.Join(RenderQemuArgv(spec, rt, QemuRuntimePaths{}), " ")

	if !strings.Contains(args, "file=/cache/omarchy-4.0.1.iso,media=cdrom,readonly=on") {
		t.Errorf("the installer ISO was not attached as a cdrom; got %q", args)
	}
	// BOTH, not one instead of the other: the answers volume is what makes the install
	// unattended, and an installer with no answers sits at its first prompt forever.
	if !strings.Contains(args, "file=/state/omarchy-vm/seed.iso,media=cdrom,readonly=on") {
		t.Errorf("the answers volume was dropped when an installer ISO was present; got %q", args)
	}
	if !strings.Contains(args, "-boot order=cd") {
		t.Errorf("boot order must be disk-then-cdrom (order=cd); got %q", args)
	}
	if strings.Contains(args, "order=dc") {
		t.Errorf("boot order is cdrom-first — the guest would reinstall over itself on every boot: %q", args)
	}
}

// The boot order must NOT change for any other source kind. A cloud image, a bootc VM and a
// bootstrap VM all boot their own disk and attach at most the cidata seed; emitting a
// cdrom-capable boot order for them would be a behaviour change to every existing VM.
func TestRenderQemuArgv_NoInstallerLeavesBootOrderAlone(t *testing.T) {
	spec := &VmSpec{Network: &VmNetwork{Mode: "user"}}
	rt := VmRuntimeParams{
		QCOW2Path:   "/state/arch-vm/disk.qcow2",
		SeedISOPath: "/state/arch-vm/seed.iso",
	}
	args := strings.Join(RenderQemuArgv(spec, rt, QemuRuntimePaths{}), " ")

	if strings.Contains(args, "-boot") {
		t.Errorf("a VM with no installer ISO must not get a -boot argument; got %q", args)
	}
	if !strings.Contains(args, "file=/state/arch-vm/seed.iso,media=cdrom,readonly=on") {
		t.Errorf("the cidata seed cdrom regressed; got %q", args)
	}
}
