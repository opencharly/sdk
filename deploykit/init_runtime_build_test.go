package deploykit

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/spec/spec"
)

// init_runtime_build_test.go — the BUILD-side half of the container-venue init
// contract: the label projection the deploy reads, and the final USER the image
// must leave at root. Both are data-plane changes that fail SILENTLY when a field
// is dropped, so each carries a control.

// TestCapabilityInitDef_ProjectsContainerRuntimeContract pins the ONE projection
// of the resolved init contract onto the ai.opencharly.init_def label. The
// control (a sparse def) must yield zero values, so the assertion cannot pass on
// a projection that hardcodes them.
func TestCapabilityInitDef_ProjectsContainerRuntimeContract(t *testing.T) {
	full := &spec.ResolvedInit{
		Entrypoint:         []string{"/sbin/init"},
		FallbackEntrypoint: []string{"sleep", "infinity"},
		RequiresRoot:       true,
		SystemdMode:        true,
		StopSignal:         "SIGRTMIN+3",
		ManagementTool:     "systemctl",
		ManagementCommands: map[string]string{"start": "start {{.Service}}"},
	}
	got := capabilityInitDef(full)
	if got == nil {
		t.Fatal("capabilityInitDef(full) = nil")
	}
	if !got.RequiresRoot || !got.SystemdMode || got.StopSignal != "SIGRTMIN+3" {
		t.Errorf("container runtime contract dropped: %+v", got)
	}
	if len(got.Entrypoint) != 1 || got.Entrypoint[0] != "/sbin/init" {
		t.Errorf("Entrypoint = %v", got.Entrypoint)
	}
	if got.ManagementTool != "systemctl" || got.ManagementCommands["start"] == "" {
		t.Errorf("management surface dropped: %+v", got)
	}

	control := capabilityInitDef(&spec.ResolvedInit{Entrypoint: []string{"/sbin/init"}})
	if control == nil {
		t.Fatal("capabilityInitDef(sparse) = nil")
	}
	if control.RequiresRoot || control.SystemdMode || control.StopSignal != "" {
		t.Errorf("control (sparse def) must carry no container runtime contract: %+v", control)
	}
	if capabilityInitDef(nil) != nil {
		t.Error("capabilityInitDef(nil) must be nil")
	}
}

// TestRenderDrive_RequiresRootSkipsFinalUser pins the build-side USER decision:
// an init that declares requires_root leaves the image at root (systemd refuses a
// non-root PID 1), while the control — the same box with no such init — still
// gets the final `USER <uid>`. Reverting the RequiresRoot branch fails the first
// assertion while the control stays green.
func TestRenderDrive_RequiresRootSkipsFinalUser(t *testing.T) {
	render := func(initDef *spec.ResolvedInit) string {
		t.Helper()
		tmp := t.TempDir()
		box := trivialBox()
		box.InitDef = initDef
		dg := NewRenderGenerator()
		dg.Dir = tmp
		dg.BuildDir = filepath.Join(tmp, ".build")
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

	control := render(nil)
	if !strings.Contains(control, "USER 1000") {
		t.Fatalf("control (no init contract) must emit the final USER 1000; got:\n%s", control)
	}

	withRoot := render(&spec.ResolvedInit{Entrypoint: []string{"/sbin/init"}, RequiresRoot: true})
	if strings.Contains(withRoot, "USER 1000") {
		t.Errorf("a root-required init must leave the image at root; got a final USER:\n%s", withRoot)
	}

	notRoot := render(&spec.ResolvedInit{Entrypoint: []string{"/sbin/init"}})
	if !strings.Contains(notRoot, "USER 1000") {
		t.Errorf("an init WITHOUT requires_root must not change the USER decision; got:\n%s", notRoot)
	}
}
