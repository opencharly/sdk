package deploykit

// image_store_test.go — the per-engine argv arms, the rootful daemon-identity
// verification and the import annotation control.
//
// Every assertion here fails if the behaviour it names is removed: the argv
// cases read the EXACT command the executor received, the daemon cases assert
// the store op was never issued behind a rootless reroute, and the import cases
// assert a dangling load is refused rather than reported successful.

import (
	"context"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// storeRecExec is a DeployExecutor that records every command and answers from
// canned tables — no process is spawned. Every reply is keyed by the EXACT
// command string, so an argv regression is a visible miss rather than a
// coincidental hit.
type storeRecExec struct {
	calls  []string
	stdout map[string]string
	stderr map[string]string
	exit   map[string]int
	runErr map[string]error
}

func newStoreRecExec() *storeRecExec {
	return &storeRecExec{
		stdout: map[string]string{},
		stderr: map[string]string{},
		exit:   map[string]int{},
		runErr: map[string]error{},
	}
}

func (e *storeRecExec) Venue() string { return "store-rec://test" }
func (e *storeRecExec) RunCapture(_ context.Context, script string) (string, string, int, error) {
	e.calls = append(e.calls, script)
	return e.stdout[script], e.stderr[script], e.exit[script], e.runErr[script]
}
func (e *storeRecExec) RunSystem(_ context.Context, script string, _ EmitOpts) error {
	e.calls = append(e.calls, "SYSTEM "+script)
	return nil
}
func (e *storeRecExec) RunUser(_ context.Context, script string, _ EmitOpts) error {
	e.calls = append(e.calls, "USER "+script)
	return nil
}
func (e *storeRecExec) RunBuilder(context.Context, BuilderRunOpts) ([]byte, error) { return nil, nil }
func (e *storeRecExec) PutFile(context.Context, string, string, uint32, bool, EmitOpts) error {
	return nil
}
func (e *storeRecExec) GetFile(context.Context, string, bool, EmitOpts) ([]byte, error) {
	return nil, nil
}
func (e *storeRecExec) RunInteractive(context.Context, string) (int, error) {
	return -1, spec.ErrNotSupported
}
func (e *storeRecExec) RunStream(context.Context, string) (int, error) {
	return -1, spec.ErrNotSupported
}
func (e *storeRecExec) Kind() string { return "store-rec" }
func (e *storeRecExec) ResolveHome(context.Context, string) (string, error) {
	return "/home/guest", nil
}

func (e *storeRecExec) infoCalls() int {
	count := 0
	for _, call := range e.calls {
		if strings.HasSuffix(call, " info") {
			count++
		}
	}
	return count
}

// resolvedTarget drives the REAL resolver so the capability facts under test are
// the ones production uses.
func resolvedTarget(t *testing.T, engine spec.EngineName, selection Selection) ContainerdTarget {
	t.Helper()
	target, err := ResolveContainerdTarget(engine, selection, nil)
	if err != nil {
		t.Fatalf("ResolveContainerdTarget(%s): %v", engine, err)
	}
	return target
}

func TestImageStoreArgvPerEngine(t *testing.T) {
	ctx := context.Background()
	const ref = "ghcr.io/opencharly/box:2026.296.1200"
	const archive = "/tmp/box-2026.296.1200.oci.tar"

	cases := []struct {
		name        string
		engine      spec.EngineName
		selection   Selection
		wantBinary  string
		wantHasArgv string
		wantRootful bool
	}{
		{
			name: "podman", engine: "podman", selection: Selection{},
			wantBinary:  "podman",
			wantHasArgv: "image exists",
		},
		{
			name: "docker", engine: "docker", selection: Selection{},
			wantBinary:  "docker",
			wantHasArgv: "image inspect",
		},
		{
			name: "containerd rootless", engine: "containerd", selection: Selection{},
			wantBinary:  "nerdctl --address '/run/containerd/containerd.sock' --namespace 'default'",
			wantHasArgv: "image inspect",
		},
		{
			name: "containerd rootful sudo", engine: "containerd",
			selection:   Selection{RootfulFlag: "sudo"},
			wantBinary:  "sudo -n nerdctl --address '/run/containerd/containerd.sock' --namespace 'default'",
			wantHasArgv: "image inspect",
			wantRootful: true,
		},
		{
			name: "containerd rootful native", engine: "containerd",
			selection:   Selection{RootfulFlag: "native"},
			wantBinary:  "nerdctl --address '/run/containerd/containerd.sock' --namespace 'default'",
			wantHasArgv: "image inspect",
			wantRootful: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := newStoreRecExec()
			target := resolvedTarget(t, tc.engine, tc.selection)
			if target.Rootful != tc.wantRootful {
				t.Fatalf("resolved rootful = %v, want %v", target.Rootful, tc.wantRootful)
			}
			store, err := NewImageStore(rec, target)
			if err != nil {
				t.Fatalf("NewImageStore: %v", err)
			}

			// Rootful identity: a rootful store answers the probe with a non-rootless
			// SecurityOptions section.
			infoCmd := tc.wantBinary + " info"
			rec.stdout[infoCmd] = "Server:\nServer Version: v2.4.2\nSecurity Options:\n  name=seccomp,profile=builtin\n"

			// Has
			hasCmd := tc.wantBinary + " " + tc.wantHasArgv + " '" + ref + "'"
			present, err := store.Has(ctx, ref)
			if err != nil {
				t.Fatalf("Has: %v", err)
			}
			if !present {
				t.Fatal("Has = false on a zero exit, want true")
			}
			assertLastCall(t, rec, hasCmd)

			// Import: load, then the naming proof.
			importCmd := tc.wantBinary + " load -i '" + archive + "'"
			if err := store.Import(ctx, archive, ref); err != nil {
				t.Fatalf("Import: %v", err)
			}
			assertCall(t, rec, importCmd)
			assertLastCall(t, rec, hasCmd)

			// Pull
			pullCmd := tc.wantBinary + " pull '" + ref + "'"
			if err := store.Pull(ctx, ref); err != nil {
				t.Fatalf("Pull: %v", err)
			}
			assertLastCall(t, rec, pullCmd)

			// Tag
			tagCmd := tc.wantBinary + " tag 'src:1' 'dst:1'"
			if err := store.Tag(ctx, "src:1", "dst:1"); err != nil {
				t.Fatalf("Tag: %v", err)
			}
			assertLastCall(t, rec, tagCmd)

			// Remove is one best-effort invocation per reference, so a missing
			// ref cannot stop the rest.
			if err := store.Remove(ctx, "one:1", "two:2"); err != nil {
				t.Fatalf("Remove: %v", err)
			}
			assertLastCall(t, rec, tc.wantBinary+" rmi -f 'two:2'")
			assertCall(t, rec, tc.wantBinary+" rmi -f 'one:1'")

			// List
			listCmd := tc.wantBinary + " images --format '{{.Repository}}:{{.Tag}}'"
			rec.stdout[listCmd] = ref + "\n<none>:<none>\n"
			refs, err := store.List(ctx)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(refs) != 1 || refs[0] != ref {
				t.Fatalf("List = %v, want [%s] with untagged entries dropped", refs, ref)
			}
			assertLastCall(t, rec, listCmd)

			// Identity was verified before EVERY operation: Has, Import's
			// load + its post-load Has, Pull, Tag, Remove, List.
			if tc.wantRootful {
				if got, want := rec.infoCalls(), 7; got != want {
					t.Fatalf("rootful daemon probes = %d, want %d (one before every store op): %v",
						got, want, rec.calls)
				}
			} else if rec.infoCalls() != 0 {
				t.Fatalf("identity probed on a non-rootful store: %v", rec.calls)
			}
		})
	}
}

func assertCall(t *testing.T, rec *storeRecExec, want string) {
	t.Helper()
	for _, call := range rec.calls {
		if call == want {
			return
		}
	}
	t.Fatalf("command %q was never issued; calls=%v", want, rec.calls)
}

func assertLastCall(t *testing.T, rec *storeRecExec, want string) {
	t.Helper()
	if len(rec.calls) == 0 {
		t.Fatalf("no command was issued, want %q", want)
	}
	if got := rec.calls[len(rec.calls)-1]; got != want {
		t.Fatalf("last command = %q, want %q", got, want)
	}
}

// TestImageStoreRootfulDaemonIdentity is the mode verification: a rootful target
// whose daemon answers `rootless` MUST refuse before issuing the store op, and
// the same target with a genuine rootful daemon MUST proceed.
func TestImageStoreRootfulDaemonIdentity(t *testing.T) {
	ctx := context.Background()
	const ref = "ghcr.io/opencharly/box:1"
	target := resolvedTarget(t, "containerd", Selection{RootfulFlag: "sudo"})
	const base = "sudo -n nerdctl --address '/run/containerd/containerd.sock' --namespace 'default'"
	const info = base + " info"
	const has = base + " image inspect '" + ref + "'"

	t.Run("rootless reroute refused", func(t *testing.T) {
		rec := newStoreRecExec()
		rec.stdout[info] = "Server:\nServer Version: v2.3.5\nSecurity Options:\n  rootless\n"
		store, err := NewImageStore(rec, target)
		if err != nil {
			t.Fatalf("NewImageStore: %v", err)
		}
		_, err = store.Has(ctx, ref)
		if err == nil {
			t.Fatal("Has succeeded against a rootless daemon on a rootful target")
		}
		if !strings.Contains(err.Error(), "ROOTLESS") || !strings.Contains(err.Error(), "reroutes") {
			t.Fatalf("error does not name the reroute mechanism: %v", err)
		}
		assertNoCall(t, rec, has)
	})

	t.Run("rootful daemon proceeds", func(t *testing.T) {
		rec := newStoreRecExec()
		rec.stdout[info] = "Server:\nServer Version: v2.4.2\nSecurity Options:\n  name=seccomp,profile=builtin\n"
		rec.exit[has] = 0
		store, err := NewImageStore(rec, target)
		if err != nil {
			t.Fatalf("NewImageStore: %v", err)
		}
		present, err := store.Has(ctx, ref)
		if err != nil {
			t.Fatalf("Has: %v", err)
		}
		if !present {
			t.Fatal("Has = false on a rootful daemon zero exit")
		}
		assertCall(t, rec, has)
	})
}

func assertNoCall(t *testing.T, rec *storeRecExec, want string) {
	t.Helper()
	for _, call := range rec.calls {
		if call == want {
			t.Fatalf("store op %q was issued despite the refused identity; calls=%v", want, rec.calls)
		}
	}
}

func TestImageStoreImportAnnotationControl(t *testing.T) {
	ctx := context.Background()
	const ref = "ghcr.io/opencharly/box:1"
	const archive = "/tmp/unnamed.oci.tar"
	target := resolvedTarget(t, "podman", Selection{})
	const load = "podman load -i '" + archive + "'"
	const has = "podman image exists '" + ref + "'"

	t.Run("dangling import refused", func(t *testing.T) {
		rec := newStoreRecExec()
		rec.exit[load] = 0
		rec.exit[has] = imageAbsentExit // the archive landed, but no name points at it
		store, err := NewImageStore(rec, target)
		if err != nil {
			t.Fatalf("NewImageStore: %v", err)
		}
		err = store.Import(ctx, archive, ref)
		if err == nil {
			t.Fatal("Import reported success for an archive that left the store untagged")
		}
		if !strings.Contains(err.Error(), imageNameAnnotation) {
			t.Fatalf("error does not name the missing annotation %q: %v", imageNameAnnotation, err)
		}
	})

	t.Run("annotated import accepted", func(t *testing.T) {
		rec := newStoreRecExec()
		rec.exit[load] = 0
		rec.exit[has] = 0
		store, err := NewImageStore(rec, target)
		if err != nil {
			t.Fatalf("NewImageStore: %v", err)
		}
		if err := store.Import(ctx, archive, ref); err != nil {
			t.Fatalf("Import: %v", err)
		}
	})

	t.Run("empty archive source refused before any engine call", func(t *testing.T) {
		rec := newStoreRecExec()
		store, err := NewImageStore(rec, target)
		if err != nil {
			t.Fatalf("NewImageStore: %v", err)
		}
		if err := store.Import(ctx, "", ref); err == nil {
			t.Fatal("Import accepted an empty archive source")
		}
		if len(rec.calls) != 0 {
			t.Fatalf("engine was called for a malformed import: %v", rec.calls)
		}
	})
}

func TestSecurityOptions(t *testing.T) {
	cases := []struct {
		name string
		info string
		want []string
	}{
		{
			name: "rootless",
			info: "Server Version: v2.3.5\nSecurity Options:\n  rootless\nNamespace: default\n",
			want: []string{"rootless"},
		},
		{
			name: "rootful entries",
			info: "Security Options:\n  name=seccomp,profile=builtin\n",
			want: []string{"name=seccomp,profile=builtin"},
		},
		{
			name: "no section",
			info: "Server Version: v2.4.2\n",
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := securityOptions(tc.info)
			if len(got) != len(tc.want) {
				t.Fatalf("securityOptions = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("securityOptions = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestImageStoreHasDoesNotHideDaemonFailure(t *testing.T) {
	ctx := context.Background()
	const ref = "ghcr.io/opencharly/box:1"
	target := resolvedTarget(t, "docker", Selection{})
	const has = "docker image inspect '" + ref + "'"
	const info = "docker info"

	t.Run("unreachable daemon is an error, not absence", func(t *testing.T) {
		rec := newStoreRecExec()
		rec.exit[has] = imageAbsentExit
		rec.exit[info] = imageAbsentExit
		rec.stderr[info] = "Cannot connect to the Docker daemon"
		store, err := NewImageStore(rec, target)
		if err != nil {
			t.Fatalf("NewImageStore: %v", err)
		}
		present, err := store.Has(ctx, ref)
		if err == nil {
			t.Fatal("Has reported absence for an unreachable daemon")
		}
		if present {
			t.Fatal("Has returned true for an unreachable daemon")
		}
		if !strings.Contains(err.Error(), "daemon is not reachable") {
			t.Fatalf("error does not name the unreachable daemon: %v", err)
		}
	})

	t.Run("reachable daemon with a missing image is absence", func(t *testing.T) {
		rec := newStoreRecExec()
		rec.exit[has] = imageAbsentExit
		store, err := NewImageStore(rec, target)
		if err != nil {
			t.Fatalf("NewImageStore: %v", err)
		}
		present, err := store.Has(ctx, ref)
		if err != nil {
			t.Fatalf("Has: %v", err)
		}
		if present {
			t.Fatal("Has = true for a missing image")
		}
		assertCall(t, rec, info)
	})
}

func TestNewImageStoreRejectsUnresolvedEngine(t *testing.T) {
	for _, engine := range []spec.EngineName{"", "auto", "nerdctl"} {
		if _, err := NewImageStore(newStoreRecExec(), ContainerdTarget{Engine: engine}); err == nil {
			t.Fatalf("NewImageStore accepted engine %q", engine)
		}
	}
	if _, err := NewImageStore(nil, resolvedTarget(t, "podman", Selection{})); err == nil {
		t.Fatal("NewImageStore accepted a nil executor")
	}
}
