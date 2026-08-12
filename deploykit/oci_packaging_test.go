package deploykit

// oci_packaging_test.go — the box OCI packaging emission: a box-declared
// entrypoint/cmd is baked into the image's OCI config (final Containerfile
// ENTRYPOINT/CMD). A NORMAL charly image declares neither and ships no baked
// command (the deploy-time init injects it) — the third test guards that default
// blast radius. The declared shape exists for images spawned DIRECTLY from their
// baked OCI config (e.g. the AgentTeams controller spawning manager/worker
// containers through a runtime socket).

import (
	"strings"
	"testing"

	"github.com/opencharly/sdk/buildkit"
)

// renderOCIPackagingBox renders a Containerfile for a minimal external-base box
// carrying the given OCI packaging fields and returns the emitted content.
func renderOCIPackagingBox(t *testing.T, entrypoint, cmd []string) string {
	t.Helper()
	box := trivialBox()
	box.Entrypoint = entrypoint
	box.Cmd = cmd

	dg := NewRenderGenerator()
	dg.Dir = t.TempDir()
	dg.BuildDir = dg.Dir + "/.build"
	dg.Containerfiles = map[string]string{}
	dg.Boxes = map[string]*buildkit.ResolvedBox{"demo": box}
	dg.Candies = map[string]CandyModel{}
	dg.EmitBakedPlugins = func(b *strings.Builder, boxName string, candyOrder []string) error { return nil }
	dg.ValidateTextEgress = func(label, text string) error { return nil }
	dg.CollectBoxPorts = func(boxName string) ([]string, error) { return nil, nil }
	dg.CollectBoxVolume = func(boxName, home string) ([]VolumeMount, error) { return nil, nil }

	if err := dg.Generate([]string{"demo"}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return dg.Containerfiles["demo"]
}

func TestOCIPackaging_EmittedAsOCIEntrypointAndCmd(t *testing.T) {
	got := renderOCIPackagingBox(t,
		[]string{"supervisord", "-n", "-c", "/etc/supervisord.conf"},
		[]string{"--foreground"})
	wantEP := `ENTRYPOINT ["supervisord","-n","-c","/etc/supervisord.conf"]`
	wantCmd := `CMD ["--foreground"]`
	if !strings.Contains(got, wantEP) {
		t.Errorf("Containerfile missing %s\n%s", wantEP, got)
	}
	if !strings.Contains(got, wantCmd) {
		t.Errorf("Containerfile missing %s\n%s", wantCmd, got)
	}
}

func TestOCIPackaging_DeclaredEmptyCmdClearsBaseCmd(t *testing.T) {
	got := renderOCIPackagingBox(t,
		[]string{"supervisord", "-n", "-c", "/etc/supervisord.conf"},
		[]string{})
	if !strings.Contains(got, `ENTRYPOINT ["supervisord","-n","-c","/etc/supervisord.conf"]`) {
		t.Errorf("Containerfile missing ENTRYPOINT\n%s", got)
	}
	// A declared empty cmd (and an absent one) must NOT leave the base image's
	// inherited default command running behind the entrypoint.
	if !strings.Contains(got, "CMD []") {
		t.Errorf("Containerfile missing CMD [] (base cmd must clear)\n%s", got)
	}
}

func TestOCIPackaging_DefaultBoxShipsNoBakedCommand(t *testing.T) {
	// The blast-radius guard: a normal image (no declared entrypoint/cmd) must
	// keep the pre-change behaviour — no ENTRYPOINT/CMD directives, so the
	// deploy-time init injection is unaffected.
	got := renderOCIPackagingBox(t, nil, nil)
	for _, bad := range []string{"ENTRYPOINT", "CMD "} {
		if strings.Contains(got, bad) {
			t.Errorf("default box must not bake %q\n%s", bad, got)
		}
	}
}
