package deploykit

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// image_venue_ops_test.go — coverage for the venue-op seam (image_venue_ops.go). Two things
// must hold: (1) a plain ImageVenue (no explicit op set) emits EXACTLY the podman commands
// it always did — the default is behaviour-preserving; (2) a node venue built with
// NewNodeVenue addresses a k8s node's containerd through `ctr -n k8s.io` and never claims a
// torn-overlay probe it cannot perform.

// opsRecExec is a DeployExecutor that records every command and answers probes from canned
// tables — no process is spawned.
type opsRecExec struct {
	calls  []string
	stdout map[string]string
	exit   map[string]int
	runErr map[string]error
}

func newOpsRecExec() *opsRecExec {
	return &opsRecExec{
		stdout: map[string]string{},
		exit:   map[string]int{},
		runErr: map[string]error{},
	}
}

func (e *opsRecExec) Venue() string { return "venueops-rec://test" }
func (e *opsRecExec) RunCapture(_ context.Context, script string) (string, string, int, error) {
	e.calls = append(e.calls, script)
	return e.stdout[script], "", e.exit[script], e.runErr[script]
}
func (e *opsRecExec) RunSystem(_ context.Context, script string, _ EmitOpts) error {
	e.calls = append(e.calls, "SYSTEM "+script)
	return nil
}
func (e *opsRecExec) RunUser(_ context.Context, script string, _ EmitOpts) error {
	e.calls = append(e.calls, "USER "+script)
	return nil
}
func (e *opsRecExec) RunBuilder(context.Context, BuilderRunOpts) ([]byte, error) { return nil, nil }
func (e *opsRecExec) PutFile(context.Context, string, string, uint32, bool, EmitOpts) error {
	return nil
}
func (e *opsRecExec) GetFile(context.Context, string, bool, EmitOpts) ([]byte, error) {
	return nil, nil
}
func (e *opsRecExec) RunInteractive(context.Context, string) (int, error) {
	return -1, spec.ErrNotSupported
}
func (e *opsRecExec) RunStream(context.Context, string) (int, error) {
	return -1, spec.ErrNotSupported
}
func (e *opsRecExec) Kind() string { return "venueops-rec" }
func (e *opsRecExec) ResolveHome(context.Context, string) (string, error) {
	return "/home/guest", nil
}

func (e *opsRecExec) sawContains(sub string) bool {
	for _, c := range e.calls {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

// TestImageVenueDefaultIsPodmanUnchanged proves a bare ImageVenue (no op set) still routes
// every operation through the podman prefix — the refactor is invisible to existing venues
// (charly vm cp-box, charly box load).
func TestImageVenueDefaultIsPodmanUnchanged(t *testing.T) {
	rec := newOpsRecExec()
	ctx := context.Background()
	v := ImageVenue{Exec: rec, PodmanCmd: "podman", Rootless: true, Label: "cp-box"}

	const exists = "podman image exists 'quay.io/x/y:v1'"
	rec.exit[exists] = 1
	if VenueHasImage(ctx, v, "quay.io/x/y:v1") {
		t.Fatal("VenueHasImage = true, want false on a non-zero `podman image exists`")
	}
	if !rec.sawContains("podman image exists") {
		t.Fatalf("default venue did not probe via podman; calls=%v", rec.calls)
	}
	rec.exit[exists] = 0
	if !VenueHasImage(ctx, v, "quay.io/x/y:v1") {
		t.Fatal("VenueHasImage = false on a zero exit, want true")
	}

	// The torn-overlay signature is the ONLY corruption signal; a non-zero run without it
	// is NOT corruption.
	const runCmd = "podman run --rm --entrypoint /usr/bin/true 'quay.io/x/y:v1'"
	rec.calls = nil
	rec.exit[runCmd], rec.stdout[runCmd] = 125, "some other error"
	if VenueImageCorrupt(ctx, v, "quay.io/x/y:v1") {
		t.Fatal("non-overlay run failure reported as corruption")
	}
	rec.stdout[runCmd] = "error …/storage/overlay/abc: no such file"
	if !VenueImageCorrupt(ctx, v, "quay.io/x/y:v1") {
		t.Fatal("torn-overlay signature not detected")
	}
	if !rec.sawContains("podman run --rm") {
		t.Fatalf("default venue did not run the overlay probe; calls=%v", rec.calls)
	}

	rec.calls = nil
	removeVenueImages(ctx, v, "quay.io/x/y:v1")
	if !rec.sawContains("podman rmi -f") {
		t.Fatalf("default venue did not remove via podman; calls=%v", rec.calls)
	}

	rec.calls = nil
	if err := v.opsFor().Tag(ctx, "quay.io/x/y:v1", "stable:1", EmitOpts{}); err != nil {
		t.Fatalf("default tag: %v", err)
	}
	if !rec.sawContains("USER podman tag") {
		t.Fatalf("default rootless tag did not go through RunUser; calls=%v", rec.calls)
	}

	if got := v.opsFor().Describe(); got != "podman" {
		t.Fatalf("default Describe() = %q, want %q", got, "podman")
	}
}

// TestNodeVenueUsesCtrContainerd proves NewNodeVenue addresses the k8s node's containerd
// store (ctr), answers presence from `ctr images ls -q`, removes/tags with ctr verbs, and
// reports NOT corrupt (containerd import is atomic; there is no `run` to probe).
func TestNodeVenueUsesCtrContainerd(t *testing.T) {
	const lsCmd = "ctr -n k8s.io images ls -q"
	rec := newOpsRecExec()
	rec.stdout[lsCmd] = "quay.io/x/y:v1\nregistry.k8s.io/other:v2\n"
	ctx := context.Background()

	v := NewNodeVenue(rec, "", func() *exec.Cmd { return nil }, "box load")
	if v.Exec != rec || v.NewLoadCmd == nil || v.Label != "box load" {
		t.Fatalf("NewNodeVenue wiring wrong: %+v", v)
	}

	if !VenueHasImage(ctx, v, "quay.io/x/y:v1") {
		t.Fatalf("node venue missed a present ref; ls output=%q", rec.stdout[lsCmd])
	}
	if VenueHasImage(ctx, v, "quay.io/x/absent:v9") {
		t.Fatal("node venue claimed an absent ref is present")
	}
	if !rec.sawContains("ctr -n k8s.io images ls -q") {
		t.Fatalf("node venue did not probe via ctr; calls=%v", rec.calls)
	}
	if VenueImageCorrupt(ctx, v, "quay.io/x/y:v1") {
		t.Fatal("node venue claimed corruption — containerd has no `run` probe; want false")
	}

	rec.calls = nil
	removeVenueImages(ctx, v, "quay.io/x/y:v1")
	if !rec.sawContains("ctr -n k8s.io images rm") {
		t.Fatalf("node venue did not remove via ctr; calls=%v", rec.calls)
	}

	rec.calls = nil
	if err := v.opsFor().Tag(ctx, "quay.io/x/y:v1", "stable:1", EmitOpts{}); err != nil {
		t.Fatalf("node tag: %v", err)
	}
	if !rec.sawContains("ctr -n k8s.io images tag") {
		t.Fatalf("node venue did not tag via ctr; calls=%v", rec.calls)
	}

	if got := v.opsFor().Describe(); got != "ctr -n k8s.io" {
		t.Fatalf("node Describe() = %q, want the k8s.io namespace", got)
	}
}
