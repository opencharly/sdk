package loaderkit

import (
	"slices"
	"testing"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/spec/spec"
)

// init_depends_seed_test.go — the init-runtime FETCH SEED for ScanCandyFromLocal.
//
// The defect these tests pin: a candy declaring a `service:` SELECTS an init (supervisord for a
// container), and the init vocabulary names the candy that INSTALLS that init's runtime via
// `depends_candy:` — but the fetch fix-point never materialized it. InjectInitDependsCandy
// resolves `depends_candy` only against the already-scanned set, so a project that never happened
// to compose the init candy built clean, stamped ai.opencharly.init="supervisord", and died inside
// podman on the render stage's COPY of the init candy's own template.
//
// The seed is a SUPERSET: it fetches the init runtime whenever any scanned candy triggers that
// init. Test 1 pins the FULLY-LOCAL case (the early return must not skip the seed). Test 2 pins
// the REMOTE case (a remote service candy's trigger is only visible after its round has scanned
// it, so the seed must be recomputed after every fix-point round). Test 3 pins termination.

const (
	initRuntimeRepo   = "github.com/opencharly/layer-supervisord"
	initRuntimeRef    = "github.com/opencharly/layer-supervisord"
	initRuntimeVer    = "v2026.271.1817"
	initRuntimeRemote = "@github.com/opencharly/layer-supervisord:v2026.271.1817"

	podSshdRepo = "github.com/opencharly/pod-sshd"
	podSshdRef  = "github.com/opencharly/pod-sshd"
	podSshdVer  = "v2026.272.0324"
)

// supervisordInitCfg is the one-init vocabulary the seed must serve: supervisord declares its
// runtime candy as a pinned REMOTE ref (the post-cutover shape) and a non-empty service_template,
// so a non-packaged `exec:` service entry triggers it.
func supervisordInitCfg() *buildkit.InitConfig {
	return &buildkit.InitConfig{
		Init: map[string]*spec.ResolvedInit{
			"supervisord": {
				CandyFields:   []string{"service"},
				DependsCandy:  initRuntimeRemote,
				ServiceSchema: &spec.InitServiceSchema{ServiceTemplate: "x"},
			},
		},
	}
}

// scannedServiceCandy is a local candy whose single `service:` entry is a non-packaged `exec:`
// daemon — the entry that triggers every init carrying a service_template (supervisord included).
// SourceDir is a real (empty) tree so the candy_file glob arm finds nothing.
func scannedServiceCandy(name, sourceDir string) spec.ScannedCandy {
	return spec.ScannedCandy{
		Model: spec.CandyModel{
			Name:      name,
			SourceDir: sourceDir,
			Service:   []spec.CandyService{{Exec: "/usr/local/bin/sshd-wrapper"}},
		},
		View: spec.CandyView{Name: name},
	}
}

// remoteScanStub records which repos were ensured (with version) and scanned, and materializes
// the two shapes the tests need: a pod-sshd service candy, and the layer-supervisord runtime.
type remoteScanStub struct {
	ensured []string // "repo@ver"
	scanned []string // repo paths
}

func (s *remoteScanStub) seams(collect func(map[string]spec.ScannedCandy) ([]spec.RemoteDownload, error)) spec.ScanSeams {
	return spec.ScanSeams{
		CollectRemoteRefs: collect,
		EnsureRepo: func(repoPath, version string) (string, error) {
			s.ensured = append(s.ensured, repoPath+"@"+version)
			return "/cache/repos/" + repoPath, nil
		},
		ScanRemote: func(cacheDir, repoPath string, wantRefs map[string]bool) (map[string]spec.ScannedCandy, error) {
			s.scanned = append(s.scanned, repoPath)
			out := make(map[string]spec.ScannedCandy, len(wantRefs))
			for ref := range wantRefs {
				switch repoPath {
				case podSshdRepo:
					out[ref] = scannedServiceCandy("sshd", cacheDir)
				case initRuntimeRepo:
					out[ref] = spec.ScannedCandy{
						Model: spec.CandyModel{Name: "supervisord", SourceDir: cacheDir},
						View:  spec.CandyView{Name: "supervisord"},
					}
				}
			}
			return out, nil
		},
	}
}

// TestInitDependsSeeds_LocalOnlyProjectFetchesInitRuntime is the FULLY-LOCAL case: the project has
// no @-refs at all, so CollectRemoteRefs returns nothing. Before the seed, ScanCandyFromLocal took
// its `len(downloads) == 0` early return and the init runtime was never fetched — the local candy
// that triggers supervisord got an image declaring init "supervisord" with no supervisord binary.
func TestInitDependsSeeds_LocalOnlyProjectFetchesInitRuntime(t *testing.T) {
	localScanned := map[string]spec.ScannedCandy{"svc": scannedServiceCandy("svc", t.TempDir())}
	stub := &remoteScanStub{}
	seams := stub.seams(func(map[string]spec.ScannedCandy) ([]spec.RemoteDownload, error) {
		return nil, nil
	})

	got, err := ScanCandyFromLocal(localScanned, supervisordInitCfg(), seams)
	if err != nil {
		t.Fatalf("ScanCandyFromLocal: %v", err)
	}
	if _, ok := got[initRuntimeRef]; !ok {
		t.Fatalf("init-runtime candy %q was not fetched from a fully-local project", initRuntimeRef)
	}
	if !slices.Contains(stub.scanned, initRuntimeRepo) {
		t.Fatalf("ScanRemote never saw repo %q; saw %v", initRuntimeRepo, stub.scanned)
	}
	if want := initRuntimeRepo + "@" + initRuntimeVer; !slices.Contains(stub.ensured, want) {
		t.Fatalf("init runtime fetched at the wrong coordinate; ensured %v, want %q", stub.ensured, want)
	}
}

// TestInitDependsSeeds_RemoteTriggerSeedsOnTheNextRound is the shape every distro repo has: the
// service candy is itself REMOTE, so its `service:` entry is only visible AFTER round 1 has
// fetched and scanned it. A seed computed over localScanned alone would never fire here — the fix
// would be inert in exactly the projects it exists for.
func TestInitDependsSeeds_RemoteTriggerSeedsOnTheNextRound(t *testing.T) {
	stub := &remoteScanStub{}
	seams := stub.seams(func(map[string]spec.ScannedCandy) ([]spec.RemoteDownload, error) {
		return []spec.RemoteDownload{{
			RepoPath: podSshdRepo,
			Version:  podSshdVer,
			Refs:     []string{podSshdRef},
		}}, nil
	})

	got, err := ScanCandyFromLocal(nil, supervisordInitCfg(), seams)
	if err != nil {
		t.Fatalf("ScanCandyFromLocal: %v", err)
	}
	if !slices.Contains(stub.scanned, initRuntimeRepo) {
		t.Fatalf("init runtime was not fetched after the remote service candy was scanned; scanned %v", stub.scanned)
	}
	if _, ok := got[initRuntimeRef]; !ok {
		t.Fatalf("init-runtime candy %q absent from the scanned set", initRuntimeRef)
	}
}

// TestInitDependsSeeds_Terminates pins the fix-point's termination: the seed is idempotent across
// rounds, so the loop drains after exactly one round per fetched repo. A re-adding seed would hang
// this test (go test -timeout).
func TestInitDependsSeeds_Terminates(t *testing.T) {
	stub := &remoteScanStub{}
	seams := stub.seams(func(map[string]spec.ScannedCandy) ([]spec.RemoteDownload, error) {
		return []spec.RemoteDownload{{
			RepoPath: podSshdRepo,
			Version:  podSshdVer,
			Refs:     []string{podSshdRef},
		}}, nil
	})

	if _, err := ScanCandyFromLocal(nil, supervisordInitCfg(), seams); err != nil {
		t.Fatalf("ScanCandyFromLocal: %v", err)
	}
	// Exactly two materializations: pod-sshd (round 1), then the seeded layer-supervisord (round 2).
	if len(stub.ensured) != 2 {
		t.Fatalf("expected exactly 2 EnsureRepo calls, got %d: %v", len(stub.ensured), stub.ensured)
	}
}

// TestInitDependsSeeds_NoDependsCandyNoFetch: systemd declares no depends_candy (it installs with
// the OS), so a composition that only triggers systemd must fetch nothing. Guards the seed against
// firing for an init that names no runtime candy.
func TestInitDependsSeeds_NoDependsCandyNoFetch(t *testing.T) {
	initCfg := &buildkit.InitConfig{
		Init: map[string]*spec.ResolvedInit{
			"systemd": {
				CandyFields:   []string{"service"},
				CandyFiles:    []string{"*.service"},
				ServiceSchema: &spec.InitServiceSchema{SupportsPackaged: true, ServiceTemplate: "[Service]"},
			},
		},
	}
	localScanned := map[string]spec.ScannedCandy{"svc": scannedServiceCandy("svc", t.TempDir())}
	stub := &remoteScanStub{}
	seams := stub.seams(func(map[string]spec.ScannedCandy) ([]spec.RemoteDownload, error) {
		return nil, nil
	})

	got, err := ScanCandyFromLocal(localScanned, initCfg, seams)
	if err != nil {
		t.Fatalf("ScanCandyFromLocal: %v", err)
	}
	if len(stub.scanned) != 0 {
		t.Fatalf("an init with no depends_candy must fetch nothing; scanned %v", stub.scanned)
	}
	if len(got) != 1 {
		t.Fatalf("local-only set must be returned unchanged, got %d entries", len(got))
	}
}

// TestInitDependsSeeds_PackagedServiceDoesNotTriggerSupervisord guards the routing rule: a
// `use_packaged:` entry binds only to inits with supports_packaged (systemd), and supervisord sets
// it false — so a packaged-only candy must NOT seed the supervisord runtime.
func TestInitDependsSeeds_PackagedServiceDoesNotTriggerSupervisord(t *testing.T) {
	initCfg := &buildkit.InitConfig{
		Init: map[string]*spec.ResolvedInit{
			"supervisord": {
				CandyFields:   []string{"service"},
				DependsCandy:  initRuntimeRemote,
				ServiceSchema: &spec.InitServiceSchema{ServiceTemplate: "x", SupportsPackaged: false},
			},
			"systemd": {
				CandyFields:   []string{"service"},
				ServiceSchema: &spec.InitServiceSchema{SupportsPackaged: true, ServiceTemplate: "[Service]"},
			},
		},
	}
	packaged := spec.ScannedCandy{
		Model: spec.CandyModel{
			Name:      "svc",
			SourceDir: t.TempDir(),
			Service:   []spec.CandyService{{UsePackaged: "openssh-server"}},
		},
		View: spec.CandyView{Name: "svc"},
	}
	localScanned := map[string]spec.ScannedCandy{"svc": packaged}
	stub := &remoteScanStub{}
	seams := stub.seams(func(map[string]spec.ScannedCandy) ([]spec.RemoteDownload, error) {
		return nil, nil
	})

	if _, err := ScanCandyFromLocal(localScanned, initCfg, seams); err != nil {
		t.Fatalf("ScanCandyFromLocal: %v", err)
	}
	if len(stub.scanned) != 0 {
		t.Fatalf("a use_packaged service must not seed supervisord; scanned %v", stub.scanned)
	}
}

// TestInitDependsSeeds_BareDependsCandyIsNotFetched is the regression guard for the pre-existing
// shape: a bare `depends_candy: supervisord` names no repo, so the seed must leave it to the scan
// set exactly as before rather than inventing a fetch coordinate.
func TestInitDependsSeeds_BareDependsCandyIsNotFetched(t *testing.T) {
	initCfg := &buildkit.InitConfig{
		Init: map[string]*spec.ResolvedInit{
			"supervisord": {
				CandyFields:   []string{"service"},
				DependsCandy:  "supervisord",
				ServiceSchema: &spec.InitServiceSchema{ServiceTemplate: "x"},
			},
		},
	}
	localScanned := map[string]spec.ScannedCandy{"svc": scannedServiceCandy("svc", t.TempDir())}
	stub := &remoteScanStub{}
	seams := stub.seams(func(map[string]spec.ScannedCandy) ([]spec.RemoteDownload, error) {
		return nil, nil
	})

	if _, err := ScanCandyFromLocal(localScanned, initCfg, seams); err != nil {
		t.Fatalf("ScanCandyFromLocal: %v", err)
	}
	if len(stub.scanned) != 0 {
		t.Fatalf("a bare depends_candy names no repo and must not be fetched; scanned %v", stub.scanned)
	}
}

// TestInitDependsSeeds_ProjectPinIsNotOverridden pins the seed's FALLBACK contract: a project
// that already names the init runtime — here at an OLDER tag than the vocabulary's — must keep
// its own pin. The seed must add nothing, so the arbiter never sees a second candidate and
// PickCandyVersion's newest-wins rule cannot move the project's tag.
//
// Both shapes a project uses to name that runtime are covered: the root-level repo (the
// post-cutover form, matched by the seed's own bare ref) and the pre-cutover sub-path (matched by
// last path segment — the seed's ref is the vocabulary's bare `github.com/opencharly/layer-supervisord`,
// which path.Base-suffix-collides with `.../candy/layer-supervisord`).
func TestInitDependsSeeds_ProjectPinIsNotOverridden(t *testing.T) {
	const projectPin = "v2026.240.0121"

	for _, projectRef := range []string{
		initRuntimeRepo,
		initRuntimeRepo + "/candy/layer-supervisord",
	} {
		t.Run(projectRef, func(t *testing.T) {
			localScanned := map[string]spec.ScannedCandy{"svc": scannedServiceCandy("svc", t.TempDir())}
			stub := &remoteScanStub{}
			seams := stub.seams(func(map[string]spec.ScannedCandy) ([]spec.RemoteDownload, error) {
				return []spec.RemoteDownload{{
					RepoPath: initRuntimeRepo,
					Version:  projectPin,
					Refs:     []string{projectRef},
				}}, nil
			})

			got, err := ScanCandyFromLocal(localScanned, supervisordInitCfg(), seams)
			if err != nil {
				t.Fatalf("ScanCandyFromLocal: %v", err)
			}

			// Exactly ONE materialization, at the PROJECT's tag — the vocabulary tag
			// (%s) must never be seeded over a project's own pin.
			want := []string{initRuntimeRepo + "@" + projectPin}
			if !slices.Equal(stub.ensured, want) {
				t.Fatalf("EnsureRepo calls = %v, want exactly %v (vocabulary tag %s must not be seeded over a project pin)", stub.ensured, want, initRuntimeVer)
			}
			// And the resolved body keeps the project's version.
			sc, ok := got[projectRef]
			if !ok {
				t.Fatalf("init-runtime candy %q missing from the resolved set", projectRef)
			}
			if v := sc.GetVersion(); v != projectPin {
				t.Fatalf("resolved init-runtime version = %q, want the project's own pin %q", v, projectPin)
			}
		})
	}
}
