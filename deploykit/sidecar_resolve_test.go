package deploykit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/opencharly/sdk/spec"
)

func TestResolvedSidecarFromSpec(t *testing.T) {
	sec := &spec.Security{CapAdd: []string{"NET_ADMIN"}}
	in := spec.ResolvedSidecar{
		Name:     "tailscale",
		Image:    "ghcr.io/tailscale/tailscale:latest",
		Env:      map[string]string{"TS_USERSPACE": "false"},
		Security: sec,
		Volume:   []spec.ResolvedSidecarVolume{{VolumeName: "state", ContainerPath: "/var/lib/tailscale"}},
		Secret:   []spec.ResolvedSidecarSecret{{Name: "ts-authkey", Env: "TS_AUTHKEY", SecretName: "charly-versa-ts-authkey"}},
	}
	got := ResolvedSidecarFromSpec(in)
	if got.Name != "tailscale" || got.Image != in.Image {
		t.Fatalf("Name/Image mismatch: %+v", got)
	}
	if got.Env["TS_USERSPACE"] != "false" {
		t.Errorf("Env not carried over: %+v", got.Env)
	}
	if got.Security.CapAdd == nil || got.Security.CapAdd[0] != "NET_ADMIN" {
		t.Errorf("Security not dereferenced: %+v", got.Security)
	}
	if len(got.Volume) != 1 || got.Volume[0].VolumeName != "state" {
		t.Errorf("Volume not adapted: %+v", got.Volume)
	}
	if len(got.Secret) != 1 || got.Secret[0].Name != "ts-authkey" || got.Secret[0].SecretName != "charly-versa-ts-authkey" {
		t.Errorf("Secret not adapted: %+v", got.Secret)
	}
}

func TestSidecarTemplatesOf(t *testing.T) {
	if got := SidecarTemplatesOf(nil); got != nil {
		t.Errorf("SidecarTemplatesOf(nil) = %v, want nil", got)
	}
	dc := &BundleConfig{Sidecar: map[string]json.RawMessage{"tailscale": json.RawMessage("{}")}}
	got := SidecarTemplatesOf(dc)
	if len(got) != 1 || got["tailscale"] == nil {
		t.Errorf("SidecarTemplatesOf = %v, want the dc.Sidecar map", got)
	}
}

func TestHasTailscaleSidecar(t *testing.T) {
	if HasTailscaleSidecar(nil) {
		t.Error("nil should return false")
	}
	if !HasTailscaleSidecar(map[string]json.RawMessage{"tailscale": json.RawMessage("{}")}) {
		t.Error("tailscale should return true")
	}
}

func writeQuadlet(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

// TestFindPodSidecarQuadlets_ExcludesSiblingInstance is the regression test
// for the charly config remove sidecar-sweep bug: the prior implementation matched
// `<podPrefix>` as a bare filename prefix, which swept up sibling instances of
// the same image (e.g. running `charly config remove versa` stopped the unrelated
// production `charly-versa-ecovoyage.service`). The fix requires the candidate
// quadlet to declare `Pod=<podname>.pod` in its content — the load-bearing
// invariant that distinguishes true sidecars from sibling instances.
func TestFindPodSidecarQuadlets_ExcludesSiblingInstance(t *testing.T) {
	qdir := t.TempDir()

	// Main pod container — caller excludes this from the returned list.
	mainQuadlet := "[Unit]\nDescription=main\n\n[Container]\nPod=charly-versa.pod\nContainerName=charly-versa\nImage=ghcr.io/x/versa:latest\n"
	writeQuadlet(t, qdir, "charly-versa.container", mainQuadlet)

	// True sidecar — has Pod=charly-versa.pod, should match.
	sidecarQuadlet := "[Unit]\nDescription=sidecar\n\n[Container]\nPod=charly-versa.pod\nContainerName=charly-versa-tailscale\nImage=ghcr.io/tailscale/tailscale:latest\n"
	writeQuadlet(t, qdir, "charly-versa-tailscale.container", sidecarQuadlet)

	// Sibling instance — no Pod= directive, must NOT match even though the
	// filename shares the charly-versa- prefix. This is the regression scenario.
	siblingQuadlet := "[Unit]\nDescription=sibling instance\n\n[Container]\nContainerName=charly-versa-ecovoyage\nImage=ghcr.io/x/versa:2026.135.1326\n"
	writeQuadlet(t, qdir, "charly-versa-ecovoyage.container", siblingQuadlet)

	// Sibling instance with its OWN pod — also must NOT match (its Pod=
	// directive references a different pod).
	siblingPodQuadlet := "[Unit]\nDescription=sibling pod instance\n\n[Container]\nPod=charly-versa-canary.pod\nContainerName=charly-versa-canary\nImage=ghcr.io/x/versa:latest\n"
	writeQuadlet(t, qdir, "charly-versa-canary.container", siblingPodQuadlet)

	// Unrelated image whose filename happens to start with charly-versa-something
	// but is NOT in our pod.
	unrelatedQuadlet := "[Unit]\n\n[Container]\nPod=charly-different.pod\nContainerName=charly-versa-something\n"
	writeQuadlet(t, qdir, "charly-versa-something.container", unrelatedQuadlet)

	// Pod file (.pod, not .container) — must be ignored by the sweep.
	writeQuadlet(t, qdir, "charly-versa.pod", "[Pod]\nPodName=charly-versa\n")

	got, err := FindPodSidecarQuadlets(qdir, "charly-versa", "charly-versa.container")
	if err != nil {
		t.Fatalf("FindPodSidecarQuadlets: %v", err)
	}
	want := []string{"charly-versa-tailscale.container"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sidecars = %v, want %v", got, want)
	}
}

// TestFindPodSidecarQuadlets_InstanceScoping covers the instance variant: a
// removal of `versa -i ecovoyage` (pod name charly-versa-ecovoyage) must NOT pick
// up the BASE versa's quadlets, and must pick up ecovoyage-scoped sidecars.
func TestFindPodSidecarQuadlets_InstanceScoping(t *testing.T) {
	qdir := t.TempDir()

	// Base versa pod members (different pod name — must be excluded).
	writeQuadlet(t, qdir, "charly-versa.container", "[Container]\nPod=charly-versa.pod\n")
	writeQuadlet(t, qdir, "charly-versa-tailscale.container", "[Container]\nPod=charly-versa.pod\n")

	// Ecovoyage instance + its sidecar.
	writeQuadlet(t, qdir, "charly-versa-ecovoyage.container", "[Container]\nPod=charly-versa-ecovoyage.pod\n")
	writeQuadlet(t, qdir, "charly-versa-ecovoyage-tailscale.container", "[Container]\nPod=charly-versa-ecovoyage.pod\n")

	got, err := FindPodSidecarQuadlets(qdir, "charly-versa-ecovoyage", "charly-versa-ecovoyage.container")
	if err != nil {
		t.Fatalf("FindPodSidecarQuadlets: %v", err)
	}
	want := []string{"charly-versa-ecovoyage-tailscale.container"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sidecars = %v, want %v", got, want)
	}
}

// TestFindPodSidecarQuadlets_EmptyDir handles the no-quadlets case (a
// just-installed system or a fully-cleaned host).
func TestFindPodSidecarQuadlets_EmptyDir(t *testing.T) {
	qdir := t.TempDir()
	got, err := FindPodSidecarQuadlets(qdir, "charly-versa", "charly-versa.container")
	if err != nil {
		t.Fatalf("FindPodSidecarQuadlets: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}
