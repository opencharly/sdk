package deploykit

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/vmshared"
	"github.com/opencharly/spec/spec"
)

// nl is the newline, written as a raw string so this file contains no backslash escapes at all.
const nl = `
`

// TestTmpModeGuard_LandsBetweenTheFlipperAndTheConsumer is the GENERATION-LEVEL gate for the /tmp
// guard: it drives the real render drive (Generate -> generateContainerfile) over a two-candy box
// whose FIRST candy runs a nested container engine and whose SECOND writes to /tmp — the shape
// that failed check-charly-selftest-pod (flipper STEP 64, consumer STEP 70).
//
// It is deliberately NOT a unit test of EmitTmpModeGuard: deleting the insertion in generate.go
// makes it fail (no guard emitted), and emitting the guard at the end of the stage makes it fail
// (ordering).
func TestTmpModeGuard_LandsBetweenTheFlipperAndTheConsumer(t *testing.T) {
	tmp := t.TempDir()
	box := trivialBox()
	box.Candy = []string{"nesting", "consumer"}
	box.RenderCandyOrder = []string{"nesting", "consumer"}

	g := NewRenderGenerator()
	g.Dir = tmp
	g.BuildDir = filepath.Join(tmp, ".build")
	g.Containerfiles = map[string]string{}
	g.Boxes = map[string]*buildkit.ResolvedBox{"demo": box}
	g.Candies = map[string]CandyModel{
		"nesting": NewSpecCandyModel(
			spec.CandyModel{RunOps: []vmshared.Op{{Mkdir: "/opt/nesting", Description: "nested podman pre-pull"}}},
			spec.CandyView{Name: "nesting"},
		),
		"consumer": NewSpecCandyModel(
			spec.CandyModel{RunOps: []vmshared.Op{{Mkdir: "/tmp/consumer-marker"}}},
			spec.CandyView{Name: "consumer"},
		),
	}
	g.EmitBakedPlugins = func(b *strings.Builder, boxName string, candyOrder []string) error { return nil }
	g.ValidateTextEgress = func(label, text string) error { return nil }
	g.CollectBoxPorts = func(boxName string) ([]string, error) { return nil, nil }
	g.CollectBoxVolume = func(boxName, home string) ([]VolumeMount, error) { return nil, nil }

	if err := g.Generate([]string{"demo"}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	got := g.Containerfiles["demo"]

	iFlip := strings.Index(got, "/opt/nesting")
	iGuard := strings.Index(got, "chmod 1777 /tmp")
	iCons := strings.Index(got, "/tmp/consumer-marker")
	if iFlip < 0 || iGuard < 0 || iCons < 0 {
		t.Fatalf("missing landmark (flipper=%d guard=%d consumer=%d); emitted=%s", iFlip, iGuard, iCons, got)
	}
	if !(iFlip < iGuard && iGuard < iCons) {
		t.Fatalf("guard not between flipper and consumer (flipper=%d guard=%d consumer=%d); emitted=%s", iFlip, iGuard, iCons, got)
	}
	atGuard := activeUserBefore(got, iGuard)
	if atGuard != "0" && atGuard != "root" {
		t.Fatalf("guard does not run as root (active user=%q); emitted=%s", atGuard, got)
	}
	guardEnd := iGuard + strings.Index(got[iGuard:], nl+nl) + 1
	if after := activeUserBefore(got, guardEnd); after != atGuard {
		t.Fatalf("guard changed the active user (%q to %q); emitted=%s", atGuard, after, got)
	}
}

// activeUserBefore returns the argument of the last USER directive emitted before idx — the
// Containerfile s active user at that byte offset, read from the emitted TEXT rather than assumed
// from any internal flag.
func activeUserBefore(s string, idx int) string {
	last := ""
	for _, line := range strings.Split(s[:idx], nl) {
		if strings.HasPrefix(line, "USER ") {
			last = strings.TrimSpace(strings.TrimPrefix(line, "USER "))
		}
	}
	return last
}
