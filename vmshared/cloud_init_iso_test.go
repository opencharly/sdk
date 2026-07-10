package vmshared

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestISOBuilderVolumeIDIsISOCompliant pins the ONE shared volume identifier every
// ISO-builder arm passes. ISO 9660 / ECMA 119 d-characters are A-Z 0-9 _ only, so a
// lowercase label makes xorriso emit
//
//	xorriso : WARNING : -volid text does not comply to ISO 9660 / ECMA 119 rules
//
// on every single VM boot. A warning is not a pass: while it persists, the
// zero-warnings R10 gate is un-meetable for every vm bed.
//
// Uppercase is safe because cloud-init's NoCloud datasource searches BOTH cases
// (DataSourceNoCloud._get_devices tries LABEL=<label>.upper() then .lower()).
func TestISOBuilderVolumeIDIsISOCompliant(t *testing.T) {
	if cloudInitVolumeID != strings.ToUpper(cloudInitVolumeID) {
		t.Fatalf("cloudInitVolumeID %q must be uppercase: ISO 9660 d-characters are A-Z 0-9 _ only", cloudInitVolumeID)
	}
	for _, r := range cloudInitVolumeID {
		compliant := (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
		if !compliant {
			t.Fatalf("cloudInitVolumeID %q contains non-d-character %q", cloudInitVolumeID, r)
		}
	}
	// cloud-init defaults fs_label to "cidata"; only its case may differ.
	if !strings.EqualFold(cloudInitVolumeID, "cidata") {
		t.Fatalf("cloudInitVolumeID %q must case-insensitively equal cloud-init's default fs_label %q", cloudInitVolumeID, "cidata")
	}
}

// TestResolveISOBuilderPassesSharedVolumeID asserts the resolved builder's argv carries
// the shared constant (R3: one constant, not three literals) — so a future arm cannot
// silently reintroduce a lowercase label.
func TestResolveISOBuilderPassesSharedVolumeID(t *testing.T) {
	b := resolveISOBuilder()
	if b.Bin == "" {
		t.Skip("no ISO builder on PATH (xorriso/genisoimage/mkisofs)")
	}
	args := b.Args("/tmp/out.iso", []string{"/tmp/user-data"})
	i := slices.Index(args, "-volid")
	if i < 0 || i+1 >= len(args) {
		t.Fatalf("builder argv has no -volid: %v", args)
	}
	if args[i+1] != cloudInitVolumeID {
		t.Fatalf("-volid = %q, want the shared constant %q", args[i+1], cloudInitVolumeID)
	}
}

// TestWriteSeedISO_NoXorrisoWarning is the live regression: it actually builds a seed
// ISO and requires xorriso to emit NO warning. Against a lowercase volid this fails with
// the verbatim production warning. It also asserts the label libvirt/cloud-init will see.
func TestWriteSeedISO_NoXorrisoWarning(t *testing.T) {
	bin, err := exec.LookPath("xorriso")
	if err != nil {
		t.Skip("xorriso not on PATH")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "user-data")
	if err := os.WriteFile(src, []byte("#cloud-config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := filepath.Join(dir, "meta-data")
	if err := os.WriteFile(meta, []byte("instance-id: t\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "seed.iso")

	args := append([]string{"-as", "mkisofs", "-volid", cloudInitVolumeID, "-joliet", "-rock", "-output", out}, src, meta)
	combined, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("xorriso: %v\n%s", err, combined)
	}
	if strings.Contains(string(combined), "WARNING") {
		t.Fatalf("xorriso emitted a warning for volid %q:\n%s", cloudInitVolumeID, combined)
	}

	// The label the guest's cloud-init will match on: PVD volume identifier at
	// sector 16 (32768) + 40, 32 bytes, space-padded.
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck
	buf := make([]byte, 32)
	if _, err := f.ReadAt(buf, 32768+40); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(buf)); got != cloudInitVolumeID {
		t.Fatalf("ISO volume identifier = %q, want %q", got, cloudInitVolumeID)
	}
}
