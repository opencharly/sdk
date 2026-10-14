package buildkitdrive

import (
	"archive/tar"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner records the exact argv of every call, so a test can assert the SEQUENCE a
// bridge step performs rather than only that it returned nil.
type fakeRunner struct {
	calls   [][]string
	failOn  string // a binary name (or "tag") that must fail
	failErr error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	joined := strings.Join(append([]string{name}, args...), " ")
	if f.failOn != "" && strings.Contains(joined, f.failOn) {
		return "", f.failErr
	}
	return "", nil
}

func (f *fakeRunner) joined() string {
	var b strings.Builder
	for _, c := range f.calls {
		b.WriteString(strings.Join(c, " "))
		b.WriteString("\n")
	}
	return b.String()
}

// writeLayout builds a minimal OCI layout archive and returns its path.
func writeLayout(t *testing.T, manifestDigest, configDigest string, canonicalSpelling bool) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "image.oci.tar")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	tw := tar.NewWriter(f)
	add := func(name string, body string) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	add("index.json", `{"schemaVersion":2,"manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"`+manifestDigest+`","size":123,"annotations":{"org.opencontainers.image.created":"2026-10-14T21:00:00Z"}}]}`)
	add("oci-layout", `{"imageLayoutVersion":"1.0.0"}`)
	manifest := `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + configDigest + `","size":2}}`
	hex := strings.TrimPrefix(manifestDigest, "sha256:")
	if canonicalSpelling {
		add("blobs/sha256/"+hex, manifest)
	} else {
		add(hex+".json", manifest)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestBuildArgs_ExportShape pins the argv the drive actually runs, including the export
// that makes the result reachable: with no bridge the daemon's own store is invisible to
// the run engine, so the OUTPUT TYPE is not a caller option.
func TestBuildArgs_ExportShape(t *testing.T) {
	b := Build{
		Containerfile: "/ctx/Containerfile",
		Context:       "/ctx",
		Ref:           "localhost/charly-box:2026.287.1200",
		Addr:          "unix:///run/1000/x.sock",
		Platforms:     []string{"linux/amd64", "linux/arm64"},
		BuildArgs:     map[string]string{"ZED": "2", "ALPHA": "1"},
		NoCache:       true,
	}
	args, err := b.Args("/stage/out.tar")
	if err != nil {
		t.Fatalf("Args: %v", err)
	}
	got := strings.Join(args, " ")
	for _, want := range []string{
		"build --frontend dockerfile.v0",
		"--addr unix:///run/1000/x.sock",
		"--local context=/ctx",
		"--local dockerfile=/ctx",
		"--opt filename=Containerfile",
		"--opt platform=linux/amd64,linux/arm64",
		"--opt build-arg:ALPHA=1",
		"--opt build-arg:ZED=2",
		"--no-cache",
		"--output type=oci,dest=/stage/out.tar",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("argv missing %q:\n%s", want, got)
		}
	}
	// build-args are emitted in sorted order so one build is one argv.
	if strings.Index(got, "ALPHA") > strings.Index(got, "ZED") {
		t.Errorf("build-args are not sorted:\n%s", got)
	}
}

// TestBuildValidate_RefusesUnnameableBuilds: every one of these would build something the
// caller cannot then name or find, so each is an error rather than a default.
func TestBuildValidate_RefusesUnnameableBuilds(t *testing.T) {
	good := Build{Containerfile: "/c/Containerfile", Context: "/c", Ref: "r:1"}
	if err := good.Validate(); err != nil {
		t.Fatalf("the control case must validate: %v", err)
	}
	for name, b := range map[string]Build{
		"relative Containerfile": {Containerfile: "c/Containerfile", Context: "/c", Ref: "r:1"},
		"relative context":       {Containerfile: "/c/Containerfile", Context: "c", Ref: "r:1"},
		"no Containerfile":       {Context: "/c", Ref: "r:1"},
		"no context":             {Containerfile: "/c/Containerfile", Ref: "r:1"},
		"no ref":                 {Containerfile: "/c/Containerfile", Context: "/c"},
	} {
		if err := b.Validate(); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

// TestImageID_ReadsTheConfigDigestFromTheLayout: the ID a runtime computes is the CONFIG
// digest, and it is read from the archive because that is the only place it exists before
// the load. Both blob spellings are accepted.
func TestImageID_ReadsTheConfigDigestFromTheLayout(t *testing.T) {
	const want = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	for _, canonical := range []bool{true, false} {
		path := writeLayout(t, "sha256:deadbeef", want, canonical)
		got, err := ImageID(path)
		if err != nil {
			t.Fatalf("canonical=%v: %v", canonical, err)
		}
		if got != want {
			t.Errorf("canonical=%v: got %s want %s", canonical, got, want)
		}
	}
}

// TestLoadExport_LoadThenTagThenVerify — the NAMED DUTY as an executed sequence.
//
// Without the tag step the image lands untagged and the CalVer reference resolves to
// nothing, so this asserts the tag call happens (the fail-without arm: drop the tag step
// or the verify step from LoadExport and this test fails) and that the verify is LAST.
func TestLoadExport_LoadThenTagThenVerify(t *testing.T) {
	const configDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	path := writeLayout(t, "sha256:cafe", configDigest, true)
	r := &fakeRunner{}
	ref := "localhost/charly-box:2026.287.1200"
	if err := LoadExport(context.Background(), r, "podman", path, ref); err != nil {
		t.Fatalf("LoadExport: %v", err)
	}
	if len(r.calls) != 3 {
		t.Fatalf("expected load+tag+verify, got %d calls:\n%s", len(r.calls), r.joined())
	}
	if got := strings.Join(r.calls[0], " "); got != "podman load -i "+path {
		t.Errorf("step 1 must load the export, got %q", got)
	}
	if got := strings.Join(r.calls[1], " "); got != "podman tag "+configDigest+" "+ref {
		t.Errorf("step 2 must re-apply the reference BY the archive's image ID, got %q", got)
	}
	if got := strings.Join(r.calls[2], " "); got != "podman image inspect --format {{.Id}} "+ref {
		t.Errorf("step 3 must verify the reference resolves, got %q", got)
	}
}

// TestLoadExport_TagFailureIsNotSwallowed: a failing tag must fail the bridge, or a build
// whose image is unaddressable would be reported as a success.
func TestLoadExport_TagFailureIsNotSwallowed(t *testing.T) {
	path := writeLayout(t, "sha256:cafe", "sha256:3333333333333333333333333333333333333333333333333333333333333333", true)
	r := &fakeRunner{failOn: "tag", failErr: errors.New("tag: no such image")}
	if err := LoadExport(context.Background(), r, "podman", path, "r:1"); err == nil {
		t.Fatal("a failing tag must fail the bridge")
	}
	if !strings.Contains(r.joined(), "tag") {
		t.Errorf("the tag step must have been attempted:\n%s", r.joined())
	}
}

// TestLoadExport_RefusesNoImage: an archive that carries no image is an error, not a
// silent success.
func TestLoadExport_RefusesNoImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.oci.tar")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	_ = tw.WriteHeader(&tar.Header{Name: "index.json", Mode: 0o644, Size: 2})
	_, _ = tw.Write([]byte("{}"))
	_ = tw.Close()
	_ = f.Close()
	r := &fakeRunner{}
	err = LoadExport(context.Background(), r, "podman", path, "r:1")
	if !errors.Is(err, ErrNoImageInLayout) {
		t.Fatalf("want ErrNoImageInLayout, got %v", err)
	}
	if len(r.calls) != 0 {
		t.Errorf("nothing may be loaded when the archive carries no image:\n%s", r.joined())
	}
}

// TestPaths_ComeFromTheResolver: both roots are derived from the resolver, never from a
// literal. The uid-1000-hardcoded /run/user/1000/buildkit path is the deviation this ends.
func TestPaths_ComeFromTheResolver(t *testing.T) {
	rt, cache := t.TempDir(), t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", rt)
	t.Setenv("XDG_CACHE_HOME", cache)
	sock, err := SocketPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(rt, "charly", "buildkitd.sock"); sock != want {
		t.Errorf("socket: got %s want %s", sock, want)
	}
	if strings.Contains(sock, "/run/user/") {
		t.Errorf("socket must not be a uid-hardcoded literal: %s", sock)
	}
	root, err := StateRoot()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(cache, "charly", "buildkitd"); root != want {
		t.Errorf("state root: got %s want %s", root, want)
	}
	cfg, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(cfg) != root {
		t.Errorf("config %s must sit beside the state root %s", cfg, root)
	}
}

// TestDaemonConfig_SetsOnlyTheMeasuredShape: the config carries the OCI worker's sandbox
// process mode and NOTHING venue-specific. In particular no snapshotter: the
// native-vs-overlayfs measurement came from a doubly-nested user namespace, so writing
// either value here would promote a venue result into a universal rule.
func TestDaemonConfig_SetsOnlyTheMeasuredShape(t *testing.T) {
	cfg := DaemonConfig()
	if !strings.Contains(cfg, `process-mode = "sandbox"`) {
		t.Errorf("the OCI worker must run in sandbox process mode:\n%s", cfg)
	}
	for _, forbidden := range []string{"snapshotter", "overlayfs", "native", "gckeepstorage"} {
		if strings.Contains(cfg, forbidden) {
			t.Errorf("config must not pin %q (unmeasured here):\n%s", forbidden, cfg)
		}
	}
}

// TestDaemonArgs_AddressIsTheSocketNotItsDirectory — THE WIRING CONTROL, and it exists
// because its absence cost a red gate.
//
// The unit tests above asserted the pure helpers and never the value Ensure actually hands
// the daemon, so binding EnsureDirs' first return (a DIRECTORY) as the socket passed every
// one of them and the daemon then refused outright — measured:
//
//	buildkitd: failed to create unix socket on /run/user/1000/charly: is a directory
//
// Four live e2e tests caught it. This asserts the exact token after --addr, not a
// substring: the correct value STARTS WITH the directory, so a substring test could not
// tell the two apart.
func TestDaemonArgs_AddressIsTheSocketNotItsDirectory(t *testing.T) {
	rt, cache := t.TempDir(), t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", rt)
	t.Setenv("XDG_CACHE_HOME", cache)
	sock, stateRoot, err := EnsureDirs()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(sock, string(filepath.Separator)+SocketName) {
		t.Fatalf("EnsureDirs must return the SOCKET PATH, got %q", sock)
	}
	cfg, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	args := DaemonArgs(sock, stateRoot, cfg)
	var addr string
	for i, a := range args {
		if a == "--addr" && i+1 < len(args) {
			addr = args[i+1]
		}
	}
	if want := "unix://" + sock; addr != want {
		t.Errorf("--addr must be exactly %q, got %q (the socket's directory would fail the daemon)", want, addr)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"--net=host", "buildkitd --rootless", "--root " + stateRoot, "--config " + cfg} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q:\n%s", want, joined)
		}
	}
}
