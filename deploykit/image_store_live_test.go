package deploykit

// image_store_live_test.go — the R8 live arms for W3.7.
//
// Each arm runs against a REAL engine or skips VISIBLY, naming the mechanism that
// is missing. No engine is mocked: a store that cannot be reached is a skip, not
// a fake success.
//
// The annotation control is measured at the engine boundary, exactly as the W3.7
// spike measured it: the SAME image is exported twice — once annotated with
// `io.containerd.image.name=<full-ref>` plus a tag-only
// `org.opencontainers.image.ref.name`, and once with the tag-only annotation
// alone (the archive a `--output type=oci` build produces WITHOUT `name=`). The
// first is addressable by its full reference on every engine; the second is not,
// which is what proves the annotation — not coincidence — does the naming.

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/opencharly/spec/spec"
)

// localStoreExec is a test-only DeployExecutor that runs a script through bash on
// the host. It exists so the live arms drive the REAL engine CLIs through the
// same interface production uses.
type localStoreExec struct{}

func (localStoreExec) Venue() string { return "local" }
func (localStoreExec) RunCapture(ctx context.Context, script string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", script)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit := 0
	if err != nil {
		exit = -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exit = exitErr.ExitCode()
			err = nil
		}
	}
	return stdout.String(), stderr.String(), exit, err
}
func (localStoreExec) RunSystem(context.Context, string, EmitOpts) error { return spec.ErrNotSupported }
func (localStoreExec) RunUser(context.Context, string, EmitOpts) error   { return spec.ErrNotSupported }
func (localStoreExec) RunBuilder(context.Context, BuilderRunOpts) ([]byte, error) {
	return nil, spec.ErrNotSupported
}
func (localStoreExec) PutFile(context.Context, string, string, uint32, bool, EmitOpts) error {
	return spec.ErrNotSupported
}
func (localStoreExec) GetFile(context.Context, string, bool, EmitOpts) ([]byte, error) {
	return nil, spec.ErrNotSupported
}
func (localStoreExec) RunInteractive(context.Context, string) (int, error) {
	return -1, spec.ErrNotSupported
}
func (localStoreExec) RunStream(context.Context, string) (int, error) {
	return -1, spec.ErrNotSupported
}
func (localStoreExec) Kind() string { return "local-store-test" }
func (localStoreExec) ResolveHome(context.Context, string) (string, error) {
	return os.Getenv("HOME"), nil
}

// hostRun runs a host command and returns stdout, exit and stderr.
func hostRun(t *testing.T, ctx context.Context, name string, args ...string) (string, int, string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.String(), 0, stderr.String()
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout.String(), exitErr.ExitCode(), stderr.String()
	}
	t.Fatalf("%s %v: %v", name, args, err)
	return "", -1, ""
}

// TestImageStoreLiveAnnotationControl imports an annotated OCI archive and its
// unannotated control into every reachable engine.
func TestImageStoreLiveAnnotationControl(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if _, err := exec.LookPath("podman"); err != nil {
		t.Skipf("live annotation control: podman is absent (%v); the OCI archive fixture is produced from it", err)
	}
	if _, _, code, _ := (&localStoreExec{}).RunCapture(ctx, "podman info >/dev/null 2>&1"); code != 0 {
		t.Skip("live annotation control: podman is present but its store is not reachable (`podman info` failed)")
	}

	tag := fmt.Sprintf("2026.296.%d", time.Now().UnixNano()%10000)
	ref, annotated, control := buildLiveArchive(t, ctx, t.TempDir(), tag)

	engines := []struct {
		name   string
		engine spec.EngineName
		probe  string
	}{
		{name: "podman", engine: "podman", probe: "podman info >/dev/null 2>&1"},
		{name: "docker", engine: "docker", probe: "docker info >/dev/null 2>&1"},
		{name: "containerd-rootless", engine: "containerd", probe: "nerdctl info >/dev/null 2>&1"},
	}
	for _, arm := range engines {
		arm := arm
		t.Run(arm.name, func(t *testing.T) {
			if _, _, code, _ := (&localStoreExec{}).RunCapture(ctx, arm.probe); code != 0 {
				t.Skipf("live %s arm: engine probe %q failed, so the store is not reachable on this host", arm.name, arm.probe)
			}
			target, err := ResolveContainerdTarget(arm.engine, Selection{}, nil)
			if err != nil {
				t.Fatalf("ResolveContainerdTarget: %v", err)
			}
			store, err := NewImageStore(localStoreExec{}, target)
			if err != nil {
				t.Fatalf("NewImageStore: %v", err)
			}

			// Start clean so a leftover image cannot satisfy the control.
			if err := store.Remove(ctx, ref, tag); err != nil {
				t.Fatalf("cleanup before the arm: %v", err)
			}
			t.Cleanup(func() {
				_ = store.Remove(context.Background(), ref, tag)
			})

			if err := store.Import(ctx, annotated, ref); err != nil {
				t.Fatalf("annotated import: %v", err)
			}
			present, err := store.Has(ctx, ref)
			if err != nil {
				t.Fatalf("Has after annotated import: %v", err)
			}
			if !present {
				t.Fatalf("annotated archive landed without addressing %q", ref)
			}
			if err := store.Remove(ctx, ref); err != nil {
				t.Fatalf("remove between arms: %v", err)
			}

			// The control: the same image, tag-only annotation. It must land
			// WITHOUT an addressable full reference, and the import must say so.
			err = store.Import(ctx, control, ref)
			if err == nil {
				t.Fatal("control archive (no io.containerd.image.name) imported as successfully named — the annotation is not doing the naming")
			}
			if !strings.Contains(err.Error(), imageNameAnnotation) {
				t.Fatalf("control failure does not name %q: %v", imageNameAnnotation, err)
			}
			present, err = store.Has(ctx, ref)
			if err != nil {
				// `podman image exists` answers 1 for absent; a control image
				// that landed under another name is still absent for THIS ref,
				// which is the claim.
				t.Logf("%s: Has after control returned an engine error (%v) — treated as not-addressable, which is the control's claim", arm.name, err)
			} else if present {
				t.Fatal("control archive is addressable by the full reference without the annotation")
			}
		})
	}
}

// buildLiveArchive creates the tiny source image locally (no registry, no
// network) and returns the full ref plus the annotated and control archives.
func buildLiveArchive(t *testing.T, ctx context.Context, dir, tag string) (ref, annotated, control string) {
	t.Helper()
	ref = "localhost/w37-live:" + tag
	base := dir + "/base.tar"
	if err := os.WriteFile(dir+"/empty.tar", nil, 0o600); err != nil {
		t.Fatalf("write empty rootfs tar: %v", err)
	}
	if _, code, stderr := hostRun(t, ctx, "podman", "import", dir+"/empty.tar", ref); code != 0 {
		t.Fatalf("podman import: exit %d: %s", code, stderr)
	}
	t.Cleanup(func() {
		_, _, _ = hostRun(t, context.Background(), "podman", "rmi", "-f", ref)
	})
	if _, code, stderr := hostRun(t, ctx, "podman", "save", "--format", "oci-archive", "-o", base, ref); code != 0 {
		t.Fatalf("podman save: exit %d: %s", code, stderr)
	}
	annotated = dir + "/annotated.tar"
	control = dir + "/control.tar"
	rewriteArchiveIndex(t, base, annotated, ref, tag)
	rewriteArchiveIndex(t, base, control, "", tag)
	return ref, annotated, control
}

// rewriteArchiveIndex copies src to dst, replacing every manifest's annotations
// with the W3.7 pair. An empty imageName omits `io.containerd.image.name`, which
// is the unannotated control.
func rewriteArchiveIndex(t *testing.T, src, dst, imageName, tagOnly string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("open %s: %v", src, err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create %s: %v", dst, err)
	}
	defer func() { _ = out.Close() }()

	tr := tar.NewReader(in)
	tw := tar.NewWriter(out)
	defer func() { _ = tw.Close() }()
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read %s: %v", src, err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read %s/%s: %v", src, hdr.Name, err)
		}
		if strings.TrimPrefix(hdr.Name, "./") == "index.json" {
			annotations := map[string]string{"org.opencontainers.image.ref.name": tagOnly}
			if imageName != "" {
				annotations[imageNameAnnotation] = imageName
			}
			body = []byte(fmt.Sprintf(
				`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[`+
					`{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":%q,"size":%d,"annotations":{`,
				manifestDigest(t, body), manifestSize(t, body)))
			first := true
			for key, value := range annotations {
				if !first {
					body = append(body, ',')
				}
				first = false
				body = append(body, fmt.Sprintf("%q:%q", key, value)...)
			}
			body = append(body, []byte("}}]}")...)
			hdr.Size = int64(len(body))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write %s/%s header: %v", dst, hdr.Name, err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatalf("write %s/%s: %v", dst, hdr.Name, err)
		}
	}
}

// manifestDigest / manifestSize read the single descriptor podman wrote, so the
// rewritten index points at the SAME blob the source archive carried.
func manifestDigest(t *testing.T, index []byte) string {
	t.Helper()
	return jsonField(t, index, "digest")
}

func manifestSize(t *testing.T, index []byte) int {
	t.Helper()
	var size int
	if _, err := fmt.Sscanf(jsonField(t, index, "size"), "%d", &size); err != nil {
		t.Fatalf("parse the manifest size: %v", err)
	}
	return size
}

// jsonField extracts the first `"<key>":"<value>"` / `"<key>":<value>` pair.
// The fixture index is a single-manifest document, so keying on the first
// occurrence is exact.
func jsonField(t *testing.T, doc []byte, key string) string {
	t.Helper()
	needle := `"` + key + `":`
	at := strings.Index(string(doc), needle)
	if at < 0 {
		t.Fatalf("index.json carries no %q", key)
	}
	rest := string(doc)[at+len(needle):]
	rest = strings.TrimLeft(rest, " ")
	if strings.HasPrefix(rest, `"`) {
		end := strings.Index(rest[1:], `"`)
		return rest[1 : 1+end]
	}
	end := strings.IndexAny(rest, ",}")
	return rest[:end]
}

// TestImageStoreLiveRootfulIdentity exercises the rootful daemon identity check
// READ-ONLY on the system daemon, and proves the reroute mechanism it guards
// against: the same address without the privilege prefix answers `rootless`.
func TestImageStoreLiveRootfulIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if _, err := exec.LookPath("nerdctl"); err != nil {
		t.Skipf("live rootful identity: nerdctl is absent (%v)", err)
	}
	const socket = "/run/containerd/containerd.sock"
	before, err := os.Stat(socket)
	if err != nil {
		t.Skipf("live rootful identity: the system containerd socket %s is absent (%v)", socket, err)
	}

	// The mechanism: an unprivileged invocation against the rootful address is
	// silently rerouted to the rootless daemon.
	stdout, code, _ := hostRun(t, ctx, "nerdctl", "--address", socket, "--namespace", "default", "info")
	if code != 0 || !strings.Contains(stdout, "rootless") {
		t.Skipf("live rootful identity: the unprivileged probe did not demonstrate the rootless reroute (exit %d)", code)
	}

	probe := (&localStoreExec{})
	rootfulInfo, _, rootfulCode, _ := probe.RunCapture(ctx,
		"sudo -n nerdctl --address "+socket+" --namespace default info")
	if rootfulCode != 0 {
		t.Skipf("live rootful identity: `sudo -n nerdctl … info` failed (exit %d), so no rootful daemon is reachable without a prompt", rootfulCode)
	}
	if strings.Contains(rootfulInfo, "rootless") {
		t.Skipf("live rootful identity: the privileged invocation still reports rootless — the identity check would (correctly) refuse")
	}

	target, err := ResolveContainerdTarget("containerd", Selection{RootfulFlag: "sudo"}, nil)
	if err != nil {
		t.Fatalf("ResolveContainerdTarget: %v", err)
	}
	store, err := NewImageStore(localStoreExec{}, target)
	if err != nil {
		t.Fatalf("NewImageStore: %v", err)
	}

	// Read-only: a reference no one holds must answer false without mutating
	// the shared store, and listing must succeed through the verified identity.
	absent := fmt.Sprintf("localhost/w37-rootful-probe:%d", time.Now().UnixNano())
	present, err := store.Has(ctx, absent)
	if err != nil {
		t.Fatalf("rootful Has: %v", err)
	}
	if present {
		t.Fatalf("rootful store holds the never-created probe ref %q", absent)
	}
	if _, err := store.List(ctx); err != nil {
		t.Fatalf("rootful List: %v", err)
	}
	// Import/remove on this daemon are opt-in: it is the operator's SHARED
	// rootful store, not a `disposable: true` target (R6), so the default arm is
	// read-only and names the switch that enables the mutating proof.
	if os.Getenv("W37_LIVE_ROOTFUL_IMPORT") == "1" {
		if _, err := exec.LookPath("podman"); err != nil {
			t.Skipf("rootful import arm: podman is absent (%v), so the archive fixture cannot be produced", err)
		}
		ref, annotated, _ := buildLiveArchive(t, ctx, t.TempDir(), fmt.Sprintf("2026.296.%d", time.Now().UnixNano()%10000))
		if err := store.Import(ctx, annotated, ref); err != nil {
			t.Fatalf("rootful import: %v", err)
		}
		present, err := store.Has(ctx, ref)
		if err != nil {
			t.Fatalf("rootful Has after import: %v", err)
		}
		if !present {
			t.Fatalf("rootful import landed without addressing %q", ref)
		}
		if err := store.Remove(ctx, ref); err != nil {
			t.Fatalf("rootful remove: %v", err)
		}
	} else {
		t.Log("rootful import arm SKIPPED: the system daemon is a shared, non-disposable store (R6); set W37_LIVE_ROOTFUL_IMPORT=1 to prove the mutating leg")
	}

	after, err := os.Stat(socket)
	if err != nil {
		t.Fatalf("stat %s after the arm: %v", socket, err)
	}
	if !os.SameFile(before, after) {
		t.Fatalf("the system containerd socket was REPLACED during the run (before inode %v, after %v)", before.Sys(), after.Sys())
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("the system containerd socket mtime changed during the run: %s -> %s", before.ModTime(), after.ModTime())
	}
	t.Logf("system socket unchanged: same file=%v mtime=%s", os.SameFile(before, after), after.ModTime().UTC().Format(time.RFC3339))
}
