package vmshared

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// The arithmetic exists for exactly one job: computing a root partition's size from the
// disk size, the way upstream Omarchy's own configurator does it from `lsblk -bdno SIZE`:
//
//	gpt_backup_reserve   = 1 MiB
//	boot_partition_start = 1 MiB
//	boot_partition_size  = 2 GiB
//	main_partition_start = boot_size + boot_start
//	main_partition_size  = disk_size - main_start - gpt_backup_reserve
//
// This test reproduces that arithmetic in a template and checks it against the number
// computed independently here, so a wrong helper cannot pass.
func TestSeedTemplateArithmetic_ReproducesUpstreamPartitionMath(t *testing.T) {
	const disk = int64(42949672960) // 40 GiB
	inst := &spec.DistroInstaller{
		VolumeID: "cidata",
		Files: []spec.DistroInstallerFile{{
			Path: "geometry",
			Content: `boot_start={{mib 1}}
boot_size={{gib 2}}
main_start={{add (gib 2) (mib 1)}}
main_size={{sub (sub .DiskSizeBytes (add (gib 2) (mib 1))) (mib 1)}}`,
		}},
	}

	files, err := RenderInstallerSeed(inst, spec.InstallerSeedContext{DiskSizeBytes: disk})
	if err != nil {
		t.Fatalf("RenderInstallerSeed: %v", err)
	}

	mib := int64(1024 * 1024)
	gib := mib * 1024
	mainStart := 2*gib + mib
	want := strings.Join([]string{
		"boot_start=1048576",
		"boot_size=2147483648",
		"main_start=2148532224",
		"main_size=40800092160",
	}, "\n")

	// Independently: the literals above must equal the computed values, so a typo in the
	// expected string is caught too.
	if mib != 1048576 || 2*gib != 2147483648 || mainStart != 2148532224 {
		t.Fatalf("the test's own constants are wrong: mib=%d gib2=%d mainStart=%d", mib, 2*gib, mainStart)
	}
	if got := disk - mainStart - mib; got != 40800092160 {
		t.Fatalf("expected main_size is wrong: %d", got)
	}

	if files["geometry"] != want {
		t.Fatalf("rendered geometry\n got:\n%s\nwant:\n%s", files["geometry"], want)
	}
}

// A template using arithmetic must FAIL LOUDLY when the disk size is absent rather than
// rendering a plausible-looking wrong number. Go's zero value for the missing field is 0,
// so `sub 0 x` yields a NEGATIVE size — which archinstall would accept as an int and then
// fail on deep inside a partitioner, or worse, silently create something wrong.
//
// This documents the behaviour rather than asserting an error, because the renderer cannot
// know that 0 is invalid for a given format. It is the reason BuildIsoVM populates
// DiskSizeBytes unconditionally for an iso source rather than leaving it optional there.
func TestSeedTemplateArithmetic_AbsentDiskSizeYieldsNegative(t *testing.T) {
	inst := &spec.DistroInstaller{
		VolumeID: "cidata",
		Files: []spec.DistroInstallerFile{{
			Path:    "geometry",
			Content: `main_size={{sub .DiskSizeBytes (gib 2)}}`,
		}},
	}
	files, err := RenderInstallerSeed(inst, spec.InstallerSeedContext{})
	if err != nil {
		t.Fatalf("RenderInstallerSeed: %v", err)
	}
	if !strings.Contains(files["geometry"], "-2147483648") {
		t.Fatalf("an absent disk size should render a negative size (the caller must set it); got %q", files["geometry"])
	}
}

// The func set is deliberately CLOSED. A template reaching for anything else must fail at
// PARSE time — a loud, early error naming the function — rather than silently rendering an
// empty string into an answer file.
func TestSeedTemplateArithmetic_UnknownFunctionIsAParseError(t *testing.T) {
	inst := &spec.DistroInstaller{
		VolumeID: "cidata",
		Files:    []spec.DistroInstallerFile{{Path: "x", Content: `{{div .DiskSizeBytes 2}}`}},
	}
	_, err := RenderInstallerSeed(inst, spec.InstallerSeedContext{DiskSizeBytes: 4})
	if err == nil {
		t.Fatal("an unknown template function must be a parse error")
	}
	if !strings.Contains(err.Error(), "div") {
		t.Fatalf("the error must name the function; got: %v", err)
	}
}
