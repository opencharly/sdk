package storekit

// roots_test.go — the RE-EXPORT's per-root coverage.
//
// `Dir` and `CacheDir` no longer compute anything: they delegate to the contract module's ONE
// resolver (`github.com/opencharly/spec/storeroot`), so that the plugin HOST — which may import
// only `spec/*` — and every plugin reach the same four roots from the same code.
//
// The package's pre-existing tests pin the cache root's default and its override, the four root
// NAMES, the runtime→state fallback and that an unknown root errors — so a `Dir` that stopped
// distinguishing roots AT ALL is caught there (measured: `Dir` returning `CacheDir()` for every
// root fails `TestExportedConstantsAndRootsAreContract` on "Dir accepted an unknown root name").
// What none of them pins is each root's RESOLVED VALUE: they accept any error-free path for
// config and state and only compare runtime against state. Measured, with these arms removed: a
// `Dir` that maps `RootConfig` onto `RootState` leaves the whole package GREEN. After the
// delegation, `Dir`'s per-root answer IS the contract, so these arms pin it: each root's exact
// path, and — root by root — that the re-export agrees with the contract resolver.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/opencharly/spec/storeroot"
)

// pinRootsEnv pins every input the resolver reads, so no arm here can observe the developer
// host's real directories. Each XDG variable gets a DISTINCT directory, so a root that returns
// another root's path cannot coincidentally match its expectation.
func pinRootsEnv(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg-config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "xdg-cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "xdg-state"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(home, "xdg-run"))
	t.Setenv("CHARLY_CACHE_DIR", "")
}

// TestDirReExportPinsEveryRoot pins all four roots by their EXPECTED path, and requires the
// re-export to agree with the contract resolver for each one. Both halves matter: the table is
// an independent statement of where the roots are, and the agreement is the claim that this
// package re-exports rather than reimplements.
func TestDirReExportPinsEveryRoot(t *testing.T) {
	home := t.TempDir()
	pinRootsEnv(t, home)

	want := map[Root]string{
		RootConfig:  filepath.Join(home, "xdg-config", "charly"),
		RootCache:   filepath.Join(home, "xdg-cache", "charly"),
		RootState:   filepath.Join(home, "xdg-state", "charly"),
		RootRuntime: filepath.Join(home, "xdg-run", "charly"),
	}
	for root, wantDir := range want {
		got, err := Dir(root)
		if err != nil {
			t.Fatalf("Dir(%s): %v", root, err)
		}
		if got != wantDir {
			t.Errorf("Dir(%s) = %q, want %q", root, got, wantDir)
		}
		// No conversion: `root` IS a storeroot.Root (the type is an alias), so this call
		// compiles only while the two packages really are the same type.
		viaContract, err := storeroot.Dir(root)
		if err != nil {
			t.Fatalf("storeroot.Dir(%s): %v", root, err)
		}
		if got != viaContract {
			t.Errorf("Dir(%s) = %q but the contract resolver returns %q — this package must re-export ONE implementation, not keep its own", root, got, viaContract)
		}
	}

	cache, err := CacheDir()
	if err != nil {
		t.Fatalf("CacheDir: %v", err)
	}
	viaContract, err := storeroot.CacheDir()
	if err != nil {
		t.Fatalf("storeroot.CacheDir: %v", err)
	}
	if cache != want[RootCache] || cache != viaContract {
		t.Errorf("CacheDir() = %q, want the cache root %q via the contract resolver %q", cache, want[RootCache], viaContract)
	}
}

// TestCacheDirReExportHonoursTheOneOverride pins the override THROUGH the re-export: a plugin
// that relocates its store with `CHARLY_CACHE_DIR` must reach the relocated root, and no other
// root may follow it.
func TestCacheDirReExportHonoursTheOneOverride(t *testing.T) {
	home := t.TempDir()
	pinRootsEnv(t, home)
	relocated := filepath.Join(home, "relocated-store")
	t.Setenv("CHARLY_CACHE_DIR", relocated)

	for _, name := range []string{"CacheDir", "Dir(RootCache)"} {
		var got string
		var err error
		if name == "CacheDir" {
			got, err = CacheDir()
		} else {
			got, err = Dir(RootCache)
		}
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != relocated {
			t.Errorf("%s = %q, want the CHARLY_CACHE_DIR override %q", name, got, relocated)
		}
	}

	for root, wantDir := range map[Root]string{
		RootConfig:  filepath.Join(home, "xdg-config", "charly"),
		RootState:   filepath.Join(home, "xdg-state", "charly"),
		RootRuntime: filepath.Join(home, "xdg-run", "charly"),
	} {
		if got, err := Dir(root); err != nil {
			t.Fatalf("Dir(%s): %v", root, err)
		} else if got != wantDir {
			t.Errorf("Dir(%s) = %q with CHARLY_CACHE_DIR set, want %q — the override relocates the cache root ONLY", root, got, wantDir)
		}
	}
}

// TestRootConstsAreTheContractRoots pins the re-export's IDENTITY, not only its values: because
// `Root` is an alias and the four consts ARE the contract's, a plugin can pass a value across
// the boundary in either direction with no conversion. A future "helpful" local re-declaration
// would break this without changing any path.
func TestRootConstsAreTheContractRoots(t *testing.T) {
	pairs := []struct {
		ours      Root
		contracts storeroot.Root
	}{
		{RootConfig, storeroot.RootConfig},
		{RootCache, storeroot.RootCache},
		{RootState, storeroot.RootState},
		{RootRuntime, storeroot.RootRuntime},
	}
	for _, p := range pairs {
		if p.ours != p.contracts {
			t.Errorf("storekit root %q != contract root %q — these must be the same value", p.ours, p.contracts)
		}
	}
	// The alias is the property that makes cross-boundary use conversion-free: `inferred` takes
	// the type from RootCache and is then compared to the contract's own constant, which
	// type-checks only while both names denote ONE type.
	inferred := RootCache
	if inferred != storeroot.RootCache {
		t.Errorf("the type inferred from storekit.RootCache does not compare to storeroot.RootCache (%q vs %q) — the re-export must be an ALIAS", inferred, storeroot.RootCache)
	}
}

// TestRuntimeRootFallsBackToTheStateRoot pins the ONE inherited fallback through the re-export: a
// process with no `XDG_RUNTIME_DIR` (a container, a cron job) gets the state root rather than a
// path under the shared temp directory. os.TempDir() is compared DIRECTLY — a prefix test would
// confuse "the operator's home is under /tmp" with "the resolver chose /tmp".
func TestRuntimeRootFallsBackToTheStateRoot(t *testing.T) {
	home := t.TempDir()
	pinRootsEnv(t, home)
	t.Setenv("XDG_RUNTIME_DIR", "")

	state, err := Dir(RootState)
	if err != nil {
		t.Fatalf("Dir(RootState): %v", err)
	}
	runtime, err := Dir(RootRuntime)
	if err != nil {
		t.Fatalf("Dir(RootRuntime): %v", err)
	}
	if runtime != state {
		t.Errorf("Dir(RootRuntime) = %q with XDG_RUNTIME_DIR unset, want the state root %q", runtime, state)
	}
	for _, shared := range []string{os.TempDir(), filepath.Join(os.TempDir(), "charly")} {
		if runtime == shared {
			t.Errorf("Dir(RootRuntime) = %q — the fallback must be the state root, never the shared temp directory", runtime)
		}
	}
}
