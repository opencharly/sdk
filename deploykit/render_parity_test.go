package deploykit

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/spec"
)

// render_parity_test.go — the committed byte-golden for the #67 render-DRIVE move.
// The Containerfile render DRIVE (Generate / generateContainerfile) is homed in
// sdk/deploykit; plugin-build drives it over the resolved-project envelope (the
// specCandyAdapter / NewSpecResolvedBox path) + the host-coupled seams. This test
// exercises the EXACT moved path on a minimal hand-built envelope (a trivial
// external-base box, no init/builders/plugin-verbs — so the nil seams are not hit)
// and asserts the emitted Containerfile is byte-stable against a committed golden,
// guarding the render DRIVE against regressions. The FULL box-build byte-parity
// (real boxes with init/builders/localpkg/egress) is proven by the R10 check-pod bed
// (build + fresh-rebuild) + the origin/main-vs-new-binary Containerfile diff.

var updateRenderGolden = flag.Bool("update-render-golden", false, "regenerate the render-parity golden")

// trivialBox builds a minimal *buildkit.ResolvedBox (external base, no init/builders)
// carrying the build-RENDER caches the deploykit render reads.
func trivialBox() *buildkit.ResolvedBox {
	return &buildkit.ResolvedBox{
		Name:            "demo",
		EffectiveVersion: "2026.001.0001",
		Base:            "quay.io/fedora/fedora:43",
		IsExternalBase:  true,
		UID:             1000,
		GID:             1000,
		User:            "user",
		Home:            "/home/user",
		UserAdopted:     true,
		Distro:          []string{"fedora:43", "fedora"},
		BuildFormats:    []string{"rpm"},
		RenderCandyOrder: []string{},
		ActiveInits:     map[string]*spec.ResolvedInit{},
		CandyCaps:       &buildkit.AggregatedCandyCaps{},
		BakedMetadata: &spec.BakedLabelSet{
			Version: "2026.001.0001",
			Box:     "demo",
			UID:     1000,
			GID:     1000,
			User:    "user",
			Home:    "/home/user",
			Distro:    []string{"fedora:43", "fedora"},
			BuildFormat: []string{"rpm"},
			Status:      "testing",
		},
	}
}

func TestRenderDrive_ByteGolden(t *testing.T) {
	tmp := t.TempDir()
	box := trivialBox()

	dg := NewRenderGenerator()
	dg.Dir = tmp
	dg.BuildDir = filepath.Join(tmp, ".build")
	dg.Containerfiles = map[string]string{}
	dg.Boxes = map[string]*buildkit.ResolvedBox{"demo": box}
	dg.Candies = map[string]CandyModel{} // empty candyOrder — no candies to render
	// Host-coupled seams: stub the ones the render calls unconditionally for a trivial box.
	// (RenderService, the builder resolves, EmitPluginOp, localpkg, header-copy, ensure-builders
	// are NOT hit: no init, no builders, no plugin verbs, empty candyOrder.)
	dg.EmitBakedPlugins = func(b *strings.Builder, boxName string, candyOrder []string) error { return nil }
	dg.ValidateTextEgress = func(label, text string) error { return nil }
	dg.CollectBoxPorts = func(boxName string) ([]string, error) { return nil, nil }
	dg.CollectBoxVolume = func(boxName, home string) ([]VolumeMount, error) { return nil, nil }

	if err := dg.Generate([]string{"demo"}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	got := dg.Containerfiles["demo"]

	goldenPath := filepath.Join("testdata", "render_parity_golden.txt")
	if *updateRenderGolden {
		if err := os.MkdirAll("testdata", 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
		t.Skipf("render-parity golden regenerated: %s", goldenPath)
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update-render-golden to generate)", goldenPath, err)
	}
	if string(want) != got {
		t.Fatalf("render-parity golden mismatch (run -update-render-golden if intended):\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}