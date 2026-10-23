package deploykit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// init_runtime_test.go — the container-venue init runtime gate.
//
// The declaration lives on the image's baked init contract; these tests pin that
// BOTH pod render surfaces (the direct-run argv and the quadlet unit) turn it
// into the runtime facts a systemd PID 1 needs. Each test carries its own
// control: the SAME call with a nil init contract must emit neither fact, so the
// assertion cannot pass on a builder that always emits them.

func systemdContainerInitDef() *spec.CapabilityInitDef {
	return &spec.CapabilityInitDef{
		Entrypoint:   []string{"/sbin/init"},
		RequiresRoot: true,
		SystemdMode:  true,
		StopSignal:   "SIGRTMIN+3",
	}
}

// TestBuildStartArgs_EmitsDeclaredInitRuntime pins the direct-run argv: podman
// (which has a systemd mode) gets `--systemd=always` and `--stop-signal`, and a
// nil init contract gets neither.
func TestBuildStartArgs_EmitsDeclaredInitRuntime(t *testing.T) {
	args := strings.Join(BuildStartArgs(
		"podman", false, "img:latest", 0, 0, nil, "svc",
		nil, nil, false, "127.0.0.1", nil, SecurityConfig{}, []string{"/sbin/init"}, systemdContainerInitDef(), "/work",
	), " ")
	for _, want := range []string{"--systemd=always", "--stop-signal=SIGRTMIN+3"} {
		if !strings.Contains(args, want) {
			t.Errorf("systemd init argv missing %q: %s", want, args)
		}
	}

	control := strings.Join(BuildStartArgs(
		"podman", false, "img:latest", 0, 0, nil, "svc",
		nil, nil, false, "127.0.0.1", nil, SecurityConfig{}, []string{"/sbin/init"}, nil, "/work",
	), " ")
	for _, unwanted := range []string{"--systemd", "--stop-signal"} {
		if strings.Contains(control, unwanted) {
			t.Errorf("control (nil init contract) must not emit %q: %s", unwanted, control)
		}
	}
}

// TestBuildStartArgs_EngineWithoutSystemdModeStillStops pins the capability-driven
// split: docker has no systemd-mode fact, so the flag is omitted rather than
// invented, while the engine-neutral stop signal is still emitted.
func TestBuildStartArgs_EngineWithoutSystemdModeStillStops(t *testing.T) {
	args := strings.Join(BuildStartArgs(
		"docker", false, "img:latest", 0, 0, nil, "svc",
		nil, nil, false, "127.0.0.1", nil, SecurityConfig{}, []string{"/sbin/init"}, systemdContainerInitDef(), "/work",
	), " ")
	if strings.Contains(args, "--systemd") {
		t.Errorf("docker has no systemd mode; the flag must not be invented: %s", args)
	}
	if !strings.Contains(args, "--stop-signal=SIGRTMIN+3") {
		t.Errorf("the stop signal is engine-neutral and must be emitted: %s", args)
	}
}

// TestGenerateQuadlet_EmitsDeclaredInitRuntime pins the quadlet surface. There
// is NO quadlet `Systemd=` key (the generator skips the whole unit on an unknown
// key, measured), so the engine flag rides `PodmanArgs=` exactly as the argv path
// spells it, and the stop signal uses the supported `StopSignal=` key.
func TestGenerateQuadlet_EmitsDeclaredInitRuntime(t *testing.T) {
	base := QuadletConfig{
		BoxName:    "systemd-box",
		ImageRef:   "ghcr.io/test/systemd-box:latest",
		Home:       "/root",
		Entrypoint: []string{"/sbin/init"},
	}
	withInit := base
	withInit.InitDef = systemdContainerInitDef()
	content := GenerateQuadlet(withInit)
	for _, want := range []string{"Exec=/sbin/init", "PodmanArgs=--systemd=always", "StopSignal=SIGRTMIN+3"} {
		if !quadletContainsLine(content, want) {
			t.Errorf("expected %q in quadlet:\n%s", want, content)
		}
	}

	control := GenerateQuadlet(base)
	for _, unwanted := range []string{"--systemd=always", "StopSignal="} {
		if strings.Contains(control, unwanted) {
			t.Errorf("control (nil init contract) must not emit %q:\n%s", unwanted, control)
		}
	}
}
