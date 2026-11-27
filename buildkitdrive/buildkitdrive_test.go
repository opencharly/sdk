package buildkitdrive

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
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
		// The progress mode is EXPLICIT: leaving it to buildctl's `auto` default makes a build
		// report differently in a terminal and in CI, and the non-terminal caller loses the
		// failing step's own output (opencharly/.github#271).
		"build --progress plain --frontend dockerfile.v0",
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
// process mode and the GC budget, and NOTHING venue-specific. In particular no
// snapshotter by default: the native-vs-overlayfs measurement came from a doubly-nested
// user namespace, so writing either value here would promote a venue result into a
// universal rule.
func TestDaemonConfig_SetsOnlyTheMeasuredShape(t *testing.T) {
	cfg := DaemonConfig(DaemonOptions{})
	if !strings.Contains(cfg, `process-mode = "sandbox"`) {
		t.Errorf("the OCI worker must run in sandbox process mode:\n%s", cfg)
	}
	for _, forbidden := range []string{"snapshotter", "overlayfs", "native"} {
		if strings.Contains(cfg, forbidden) {
			t.Errorf("config must not pin %q (unmeasured here):\n%s", forbidden, cfg)
		}
	}
}

// TestDaemonConfig_CarriesTheGCBudget — the cache-policy control (opencharly/charly#1292).
//
// buildkitd's compiled default GC policy keeps a 373 GB free-space target (the
// `--oci-worker-gc-keepstorage 10000,373000,100000` default). On a host with less free
// space than that, the GC prunes reclaimable cache down to the ~10 GB reserve floor on
// every pass, so a repeated build of an unchanged tree re-executes its layers. The drive
// therefore writes an explicit 100 GB budget, and the default free-space target is small.
func TestDaemonConfig_CarriesTheGCBudget(t *testing.T) {
	cfg := DaemonConfig(DaemonOptions{})
	for _, want := range []string{
		"  gc = true",
		`  reservedSpace = "100GB"`,
		`  maxUsedSpace = "100GB"`,
		`  minFreeSpace = "10GB"`,
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("the default config must carry %q:\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, "373000") || strings.Contains(cfg, "keepStorage") {
		t.Errorf("the legacy keepStorage spelling must not be rendered:\n%s", cfg)
	}
}

// TestDaemonConfig_GCIsConfigurable — every GC knob the charly.yml surface exposes renders,
// and the disabled arm drops the budget keys (they are meaningless with gc = false).
func TestDaemonConfig_GCIsConfigurable(t *testing.T) {
	cfg := DaemonConfig(DaemonOptions{
		GCReservedSpace: "40GB",
		GCMaxUsedSpace:  "80GB",
		GCMinFreeSpace:  "5GB",
		GCPolicies: []GCPolicy{{
			ReservedSpace: "1GB",
			MaxUsedSpace:  "2GB",
			MinFreeSpace:  "3GB",
			KeepDuration:  "48h",
			Filters:       []string{"type==source.local", "type==exec.cachemount"},
		}},
	})
	for _, want := range []string{
		`  reservedSpace = "40GB"`,
		`  maxUsedSpace = "80GB"`,
		`  minFreeSpace = "5GB"`,
		"[[worker.oci.gcpolicy]]",
		`  keepDuration = "48h"`,
		`  filters = ["type==source.local", "type==exec.cachemount"]`,
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config must carry %q:\n%s", want, cfg)
		}
	}

	off := DaemonConfig(DaemonOptions{GCDisabled: true})
	if !strings.Contains(off, "  gc = false") {
		t.Errorf("GCDisabled must render gc = false:\n%s", off)
	}
	if strings.Contains(off, "reservedSpace") {
		t.Errorf("a disabled GC must not render a budget:\n%s", off)
	}
}

// TestConfigGC_RoundTrips — the reader that lets Ensure tell whether a RUNNING daemon was
// started with the policy now requested.
func TestConfigGC_RoundTrips(t *testing.T) {
	want := DaemonOptions{GCReservedSpace: "40GB", GCMaxUsedSpace: "80GB", GCMinFreeSpace: "5GB"}
	got := ConfigGC(DaemonConfig(want))
	if !want.SameGC(got) {
		t.Errorf("rendered GC must read back as the requested policy: got %s", got.Describe())
	}
	if (DaemonOptions{}).SameGC(got) {
		t.Errorf("the default policy must differ from a 40/80/5 budget")
	}
	off := ConfigGC(DaemonConfig(DaemonOptions{GCDisabled: true}))
	if off.Enabled {
		t.Errorf("a disabled GC must read back disabled: %s", off.Describe())
	}
}

// TestGCConfigMismatch — the running-daemon report Ensure prints when the daemon predates
// the requested GC policy. The policy is fixed at daemon start, so a mismatch is REPORTED
// (never fatal) and the operator restarts; a matching policy reports nothing.
func TestGCConfigMismatch(t *testing.T) {
	opts := DaemonOptions{GCReservedSpace: "40GB", GCMaxUsedSpace: "80GB", GCMinFreeSpace: "5GB"}

	// The pre-change rendered form: no GC section at all.
	old := "[worker.oci]\n  process-mode = \"sandbox\"\n  max-parallelism = 4\n"
	msg, mismatch := GCConfigMismatch(old, opts)
	if !mismatch {
		t.Fatal("a config without the requested GC policy must report a mismatch")
	}
	for _, want := range []string{"gc disabled", "reservedSpace=40GB", "maxUsedSpace=80GB", "minFreeSpace=5GB"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the report must name %q:\n%s", want, msg)
		}
	}

	if msg, mismatch := GCConfigMismatch(DaemonConfig(opts), opts); mismatch || msg != "" {
		t.Fatalf("a matching policy must report nothing: mismatch=%v msg=%q", mismatch, msg)
	}

	// A disabled GC is a real change from an enabled budget and vice versa.
	if _, mismatch := GCConfigMismatch(DaemonConfig(opts), DaemonOptions{GCDisabled: true}); !mismatch {
		t.Error("enabled -> disabled must report a mismatch")
	}
	if _, mismatch := GCConfigMismatch(DaemonConfig(DaemonOptions{GCDisabled: true}), opts); !mismatch {
		t.Error("disabled -> enabled must report a mismatch")
	}
}

// fakeWorkers is a Runner that answers the daemon probe. It is used ONLY by the Ensure
// config test, whose seam materialises the socket; nothing else about a daemon is faked.
type fakeWorkers struct{}

func (fakeWorkers) Run(context.Context, string, ...string) (string, error) {
	return "oci.process-mode:SANDBOX", nil
}

// TestEnsureRewritesAStaleConfigWithoutParallelism — the always-write arm, exercised on
// the REAL Ensure path.
//
// The pre-change shape skipped the write when MaxParallelism was 0 and a config file
// already existed, so a stale daemon config survived an ensure and a daemon started from
// it. This test starts from exactly that file (a pre-GC config, max-parallelism=4) and
// asserts Ensure REPLACES it with the rendered config for an options value carrying no
// parallelism at all. It FAILS on the unmodified tree (the write was skipped).
func TestEnsureRewritesAStaleConfigWithoutParallelism(t *testing.T) {
	rt, cache := t.TempDir(), t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", rt)
	t.Setenv("XDG_CACHE_HOME", cache)

	cfg, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := "[worker.oci]\n  process-mode = \"sandbox\"\n  max-parallelism = 4\n"
	if err := os.WriteFile(cfg, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	// Capture the spawn instead of launching a daemon: the assertion is about the config
	// decision that precedes it, and a real rootlesskit here would be a bed, not a gate.
	// The seam materialises the socket the readiness probe stats, so Ensure returns as
	// soon as the fake daemon "answers" rather than waiting out its deadline.
	var gotArgs []string
	var gotLog string
	old := startDaemon
	startDaemon = func(_ context.Context, logPath string, args []string) error {
		gotLog, gotArgs = logPath, args
		for i, a := range args {
			if a == "--addr" && i+1 < len(args) {
				if err := os.WriteFile(strings.TrimPrefix(args[i+1], "unix://"), nil, 0o600); err != nil {
					return err
				}
			}
		}
		return nil
	}
	t.Cleanup(func() { startDaemon = old })

	opts := DaemonOptions{} // no parallelism: the case the old branch refused to write
	if _, err := Ensure(context.Background(), fakeWorkers{}, opts); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	got, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if want := DaemonConfig(opts); string(got) != want {
		t.Fatalf("the stale config was not replaced by the rendered one:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	if !strings.Contains(string(got), "reservedSpace") {
		t.Errorf("the rendered config must carry the GC budget:\n%s", got)
	}
	if strings.Contains(string(got), "max-parallelism = 4") {
		t.Errorf("the stale parallelism survived:\n%s", got)
	}
	if gotLog == "" || len(gotArgs) == 0 {
		t.Fatalf("the daemon was not spawned through the seam: log=%q args=%v", gotLog, gotArgs)
	}
	if !strings.Contains(strings.Join(gotArgs, " "), "--config "+cfg) {
		t.Errorf("the spawn must name the rendered config: %v", gotArgs)
	}
}

// TestDaemonConfig_CarriesTheRegistryMirror — the base-input half of the local-build bridge.
//
// MEASURED (spike, buildkitd 0.34.0, a registry:2 mirror on 127.0.0.1:5000):
//   - the mirror key must be the registry HOST (`[registry."ghcr.io"]`); a host+namespace
//     key (`[registry."ghcr.io/opencharly"]`) is SILENTLY IGNORED and the resolve still
//     goes upstream to ghcr and 403s;
//   - a plain-HTTP local mirror needs its own `[registry."<mirror>"] http = true` stanza,
//     or the daemon refuses the mirror instead of using it.
//
// With both rendered, `FROM ghcr.io/opencharly/spike-base:test` resolved from the local
// registry and the build completed with no ghcr query (spike rc=0).
func TestDaemonConfig_CarriesTheRegistryMirror(t *testing.T) {
	cfg := DaemonConfig(DaemonOptions{RegistryMirror: "127.0.0.1:5000"})
	if !strings.Contains(cfg, `[registry."ghcr.io"]`) {
		t.Errorf("the mirror must be keyed by registry HOST, not host+namespace:\n%s", cfg)
	}
	if !strings.Contains(cfg, `mirrors = ["127.0.0.1:5000"]`) {
		t.Errorf("the mirror must be rendered:\n%s", cfg)
	}
	// Scoped to the stanza, not the whole rendered file: a stray "http = true" elsewhere
	// must not satisfy this. The requirement is that the MIRROR HOST's own stanza carries it.
	if !strings.Contains(cfg, "[registry.\"127.0.0.1:5000\"]\n  http = true") {
		t.Errorf("a plain-HTTP mirror needs its own http = true stanza:\n%s", cfg)
	}
	if strings.Contains(cfg, "ghcr.io/opencharly") {
		t.Errorf("a host+namespace mirror key is silently ignored and must not be rendered:\n%s", cfg)
	}
	if cfgNo := DaemonConfig(DaemonOptions{}); strings.Contains(cfgNo, "[registry.") {
		t.Errorf("an empty mirror must render no registry stanza:\n%s", cfgNo)
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

// TestDaemonConfig_CarriesTheResolvedParallelism — THE 4-pre's control.
//
// `stage_parallelism` is a DAEMON-level setting under BuildKit (the client has no per-build
// equivalent), so what makes this rename real rather than cosmetic is that the value reaches
// the rendered config. This asserts it does, that an absent value stays absent, and that the
// renderer and the reader agree — a round trip, so the restart comparison downstream cannot
// compare against a value the config never carried.
func TestDaemonConfig_CarriesTheResolvedParallelism(t *testing.T) {
	for _, want := range []int{1, 4, 8, 64} {
		cfg := DaemonConfig(DaemonOptions{MaxParallelism: want})
		if !strings.Contains(cfg, fmt.Sprintf("max-parallelism = %d", want)) {
			t.Errorf("rendered config must carry max-parallelism = %d:\n%s", want, cfg)
		}
		if got := ConfigMaxParallelism(cfg); got != want {
			t.Errorf("round trip: rendered %d, read back %d", want, got)
		}
		if !strings.Contains(cfg, `process-mode = "sandbox"`) {
			t.Errorf("adding the parallelism key must not drop the measured process mode:\n%s", cfg)
		}
	}
	// No opinion (or an unresolved 0) leaves the key OUT, which is the daemon's own default.
	for _, none := range []int{0, -1} {
		cfg := DaemonConfig(DaemonOptions{MaxParallelism: none})
		if strings.Contains(cfg, "max-parallelism") {
			t.Errorf("MaxParallelism %d must render no key:\n%s", none, cfg)
		}
		if got := ConfigMaxParallelism(cfg); got != 0 {
			t.Errorf("an absent key must read as 0, got %d", got)
		}
	}
}

// TestNeedsRestart_DistinguishesTheCases: a control that cannot tell "same" from "different"
// would pass while restarting on every call (or never), so both arms are pinned — including
// the no-opinion arm that keeps two callers from restarting each other's daemon.
func TestNeedsRestart_DistinguishesTheCases(t *testing.T) {
	cases := []struct {
		configured, want int
		restart          bool
		why              string
	}{
		{8, 8, false, "same value: nothing to apply"},
		{8, 4, true, "a different value: daemon-level, so it needs a restart"},
		{4, 8, true, "a different value, the other direction"},
		{0, 4, true, "a daemon running the default must adopt an explicit value"},
		{8, 0, false, "0 expresses NO opinion, so it must not restart another caller's daemon"},
		{0, 0, false, "no opinion on either side"},
	}
	for _, c := range cases {
		if got := NeedsRestart(c.configured, c.want); got != c.restart {
			t.Errorf("NeedsRestart(%d, %d) = %v, want %v (%s)", c.configured, c.want, got, c.restart, c.why)
		}
	}
}
