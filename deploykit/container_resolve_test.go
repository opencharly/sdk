package deploykit

import (
	"context"
	"testing"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// container_resolve_test.go — the discriminating coverage for the ROOT-namespaced container
// resolution: the resolver must probe the name the DEPLOY CREATED, and must resolve the engine
// from the SPLIT pair's deploy key. Both assertions FAIL if the split is reverted, which is the
// property a regression test needs (a test that cannot fail proves nothing).
//
// The container-runtime probe is the package-level `kit.ContainerRunning` var — the documented
// test-injection seam (`kit/container_probe.go`: "Package-level var for testability") — so these
// tests exercise the real `ResolveContainer` / `ResolveSidecarContainer` bodies without needing a
// live container. The live boundary (a real engine answering for a real container) is exercised
// by the bed named in the PR body, not here.

// probeCall records what the resolver asked the container runtime about.
type probeCall struct {
	engine string
	name   string
}

// stubContainerProbe replaces the runtime probe for one test, records every call, and answers
// `running`. The previous value is restored on cleanup.
func stubContainerProbe(t *testing.T, running bool) *[]probeCall {
	t.Helper()
	prev := kit.ContainerRunning
	var calls []probeCall
	kit.ContainerRunning = func(engine, name string) bool {
		calls = append(calls, probeCall{engine: engine, name: name})
		return running
	}
	t.Cleanup(func() { kit.ContainerRunning = prev })
	return &calls
}

func TestSplitNamespacedBox(t *testing.T) {
	for _, tc := range []struct {
		box, instance         string
		wantBox, wantInstance string
		why                   string
	}{
		{"openclaw.check-openclaw-pod", "", "openclaw", "check-openclaw-pod", "a ROOT-namespaced key splits at its first dot"},
		{"charly.check-sidecar-pod", "", "charly", "check-sidecar-pod", "the umbrella's own qualified bed key"},
		{"a.b.c", "", "a", "b.c", "only the FIRST dot is the namespace boundary"},
		{"versa", "", "versa", "", "a bare name has no boundary to cut"},
		{"versa", "ecovoyage", "versa", "ecovoyage", "an explicit instance wins over the dotted form"},
		{"openclaw.check-openclaw-pod", "keep", "openclaw.check-openclaw-pod", "keep", "an explicit instance leaves the box untouched"},
		{".name", "", ".name", "", "an empty namespace is not a boundary"},
		{"ns.", "", "ns.", "", "an empty name is not a boundary"},
		{"", "", "", "", "an empty key stays empty"},
	} {
		gotBox, gotInstance := splitNamespacedBox(tc.box, tc.instance)
		if gotBox != tc.wantBox || gotInstance != tc.wantInstance {
			t.Errorf("splitNamespacedBox(%q, %q) = (%q, %q), want (%q, %q): %s",
				tc.box, tc.instance, gotBox, gotInstance, tc.wantBox, tc.wantInstance, tc.why)
		}
	}
}

// TestResolveContainer_NamespacedKeyProbesTheCreatedName is the regression arm for the live
// failure: `charly check` on `openclaw.check-openclaw-pod` reported "container
// charly-openclaw.check-openclaw-pod is not running" for a container that was running under
// `charly-openclaw-check-openclaw-pod`. The want name is stated with the CREATOR's own formula
// (`ContainerNameInstance(<ns>, <rest>)`), not with the helper under test, so reverting the split
// makes this assertion fail rather than follow the code.
func TestResolveContainer_NamespacedKeyProbesTheCreatedName(t *testing.T) {
	calls := stubContainerProbe(t, true)

	for _, tc := range []struct {
		box, instance     string
		wantBox, wantInst string
	}{
		{"openclaw.check-openclaw-pod", "", "openclaw", "check-openclaw-pod"},
		{"charly.check-sidecar-pod", "", "charly", "check-sidecar-pod"},
		{"versa", "", "versa", ""},
		{"versa", "ecovoyage", "versa", "ecovoyage"},
	} {
		*calls = (*calls)[:0]
		_, name, err := ResolveContainer(context.Background(), tc.box, tc.instance)
		if err != nil {
			t.Fatalf("ResolveContainer(%q, %q): %v", tc.box, tc.instance, err)
		}
		want := kit.ContainerNameInstance(tc.wantBox, tc.wantInst)
		if name != want {
			t.Errorf("ResolveContainer(%q, %q) name = %q, want %q (the name the deploy created)",
				tc.box, tc.instance, name, want)
		}
		if len(*calls) != 1 {
			t.Fatalf("ResolveContainer(%q, %q) probed %d time(s), want 1", tc.box, tc.instance, len(*calls))
		}
		if got := (*calls)[0]; got.name != want {
			t.Errorf("ResolveContainer(%q, %q) probed %q, want %q — the probe must ask about the SAME container the name reports",
				tc.box, tc.instance, got.name, want)
		}
	}
}

// TestResolveContainer_EngineComesFromTheSplitDeployKey is the second half of the same contract:
// the split pair is ALSO what the per-deploy engine lookup is keyed by (the engine input changes
// from `(<dotted>, "")` to `(<ns>, <rest>)`). The fixture carries BOTH keys with DIFFERENT engines,
// so the test reads which key the lookup used instead of depending on the host's global engine —
// `kit.EngineBinary` is stubbed to the identity because it is a host engine-plugin call that
// otherwise collapses every engine name to the platform default.
func TestResolveContainer_EngineComesFromTheSplitDeployKey(t *testing.T) {
	calls := stubContainerProbe(t, true)

	prevHost := DeployStateHost
	prevBinary := kit.EngineBinary
	t.Cleanup(func() {
		DeployStateHost = prevHost
		kit.EngineBinary = prevBinary
	})
	kit.EngineBinary = func(engine string) string { return engine }
	splitKey := spec.DeployKey("openclaw", "check-openclaw-pod")
	DeployStateHost = &StateHostMechanisms{
		LoadUnifiedDeployConfig: func(string) (*DeployConfig, error) {
			return &DeployConfig{Deploy: map[string]DeployNode{
				splitKey:                      {Engine: "containerd"},
				"openclaw.check-openclaw-pod": {Engine: "podman"},
			}}, nil
		},
	}

	engine, name, err := ResolveContainer(context.Background(), "openclaw.check-openclaw-pod", "")
	if err != nil {
		t.Fatalf("ResolveContainer: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("probed %d time(s), want 1", len(*calls))
	}
	if engine != "containerd" {
		t.Errorf("returned engine = %q, want %q — the engine must come from the deploy entry keyed %q (the split pair), not the dotted key",
			engine, "containerd", splitKey)
	}
	if got := (*calls)[0].engine; got != "containerd" {
		t.Errorf("probe engine = %q, want %q", got, "containerd")
	}
	if want := kit.ContainerNameInstance("openclaw", "check-openclaw-pod"); name != want {
		t.Errorf("returned name = %q, want %q", name, want)
	}
}

// TestResolveSidecarContainer_NamespacedKeyProbesTheCreatedName covers the SAME mechanism on the
// sidecar resolver: a sidecar container is named `ContainerNameInstance(<ns>, <rest>) + "-" +
// sidecar`, so resolving it from the unsplit dotted key reported a running sidecar as missing.
func TestResolveSidecarContainer_NamespacedKeyProbesTheCreatedName(t *testing.T) {
	calls := stubContainerProbe(t, true)

	_, name, err := ResolveSidecarContainer(context.Background(), "charly.check-sidecar-pod", "", "mypause")
	if err != nil {
		t.Fatalf("ResolveSidecarContainer: %v", err)
	}
	want := kit.SidecarContainerNameInstance("charly", "check-sidecar-pod", "mypause")
	if name != want {
		t.Errorf("ResolveSidecarContainer name = %q, want %q", name, want)
	}
	if len(*calls) != 1 || (*calls)[0].name != want {
		t.Errorf("probe calls = %+v, want one call naming %q", *calls, want)
	}
}

// TestResolveContainer_NotRunningKeepsTheResolvedName pins the error path the live failure was
// read from: a container that is genuinely absent reports the SPLIT name, so the message names the
// container the operator would find with `charly status`.
func TestResolveContainer_NotRunningKeepsTheResolvedName(t *testing.T) {
	stubContainerProbe(t, false)

	_, _, err := ResolveContainer(context.Background(), "openclaw.check-openclaw-pod", "")
	if err == nil {
		t.Fatal("expected an error for a container that is not running")
	}
	want := "container " + kit.ContainerNameInstance("openclaw", "check-openclaw-pod") + " is not running"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}
