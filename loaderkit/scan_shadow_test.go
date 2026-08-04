package loaderkit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// scan_shadow_test.go — the shadow-arbitration gate for ScanCandyFromLocal.
//
// A candy name is globally unique, so a local candy and a pinned remote ref that both declare it
// are ONE logical candy reachable under TWO map keys (the bare name, and the full remote ref).
// ScanCandyFromLocal announces "local candy X shadows remote candy Y" for that case; these tests
// hold it to that announcement — BOTH keys must materialize the LOCAL body. Before the fix the
// remote body stayed under the ref key, so a map-iterating consumer saw two rival bodies for one
// name and Go's map order decided which it acted on. That is what made the plugin loader
// host-build a shadowed plugin candy from either the local tree or the OLD pinned remote at
// random, surfacing as an intermittent go-plugin "incompatible API version" handshake warning.

const (
	shadowCandyName = "twin-candy"
	shadowRemoteRef = "github.com/opencharly/charly/candy/twin-candy"
	shadowLocalDir  = "/local/candy/twin-candy"
	shadowRemoteDir = "/cache/repos/charly@v1/candy/twin-candy"
)

// scanSeamsForTwin builds ScanSeams serving exactly one remote materialization of the named candy,
// so the fix-point runs its real collect → fetch → scan → arbitrate path with no host or network.
func scanSeamsForTwin(remoteVersion string) spec.ScanSeams {
	return spec.ScanSeams{
		CollectRemoteRefs: func(map[string]spec.ScannedCandy) ([]spec.RemoteDownload, error) {
			return []spec.RemoteDownload{{
				RepoPath: "github.com/opencharly/charly",
				Version:  "v2026.183.1359",
				Refs:     []string{shadowRemoteRef},
			}}, nil
		},
		EnsureRepo: func(repoPath, version string) (string, error) {
			return "/cache/repos/charly@v1", nil
		},
		ScanRemote: func(cacheDir, repoPath string, wantRefs map[string]bool) (map[string]spec.ScannedCandy, error) {
			out := make(map[string]spec.ScannedCandy, len(wantRefs))
			for ref := range wantRefs {
				out[ref] = spec.ScannedCandy{
					Model: spec.CandyModel{
						Name:      shadowCandyName,
						Version:   remoteVersion,
						SourceDir: shadowRemoteDir,
					},
					View: spec.CandyView{Name: shadowCandyName},
				}
			}
			return out, nil
		},
	}
}

func localTwinScan() map[string]spec.ScannedCandy {
	return map[string]spec.ScannedCandy{
		shadowCandyName: {
			Model: spec.CandyModel{
				Name:      shadowCandyName,
				Version:   "2026.216.0900",
				SourceDir: shadowLocalDir,
			},
			View: spec.CandyView{Name: shadowCandyName},
		},
	}
}

// TestScanCandyFromLocal_ShadowedRemoteResolvesToLocalSource is the deterministic-preference gate:
// a local+remote twin pair serving one candy name must resolve the LOCAL SourceDir under BOTH
// keys, on every run. The full-ref key is the one that regressed — it carried the remote
// SourceDir, which is what the plugin loader then host-built.
func TestScanCandyFromLocal_ShadowedRemoteResolvesToLocalSource(t *testing.T) {
	// Loop so a passing run cannot be Go map order being kind once. Each call rebuilds the map,
	// and Go randomizes range order per iteration, so a per-key body that depended on order
	// would show up here rather than in one lucky run.
	for i := 0; i < 64; i++ {
		got, err := ScanCandyFromLocal(localTwinScan(), nil, scanSeamsForTwin("2026.183.1359"))
		if err != nil {
			t.Fatalf("ScanCandyFromLocal: %v", err)
		}

		bare, ok := got[shadowCandyName]
		if !ok {
			t.Fatalf("iteration %d: bare name %q missing from scan result", i, shadowCandyName)
		}
		if bare.GetSourceDir() != shadowLocalDir {
			t.Fatalf("iteration %d: bare key SourceDir = %q, want the local tree %q",
				i, bare.GetSourceDir(), shadowLocalDir)
		}

		ref, ok := got[shadowRemoteRef]
		if !ok {
			t.Fatalf("iteration %d: full remote ref %q missing from scan result — a shadowed "+
				"ref must still RESOLVE (consumers reach it by full ref), it must merely "+
				"resolve to the local body", i, shadowRemoteRef)
		}
		if ref.GetSourceDir() != shadowLocalDir {
			t.Fatalf("iteration %d: full-ref key SourceDir = %q, want the local tree %q — the "+
				"local candy shadows the remote, so building this ref's plugin from %q "+
				"would run the OLD pinned remote's source",
				i, ref.GetSourceDir(), shadowLocalDir, shadowRemoteDir)
		}
	}
}

// TestScanCandyFromLocal_ShadowWinsOverNewerRemoteVersion pins the shadow's PRECEDENCE: local wins
// on identity, not on version arbitration. PickCandyVersion arbitrates among REMOTE candidates;
// once a local candy claims the name, a remote materialization cannot outrank it by declaring a
// newer version:. Without this, the fix could be mistaken for "newest wins" and silently invert
// the moment a remote repo is tagged ahead of the working tree.
func TestScanCandyFromLocal_ShadowWinsOverNewerRemoteVersion(t *testing.T) {
	got, err := ScanCandyFromLocal(localTwinScan(), nil, scanSeamsForTwin("2099.001.0000"))
	if err != nil {
		t.Fatalf("ScanCandyFromLocal: %v", err)
	}
	for _, key := range []string{shadowCandyName, shadowRemoteRef} {
		c, ok := got[key]
		if !ok {
			t.Fatalf("key %q missing from scan result", key)
		}
		if c.GetSourceDir() != shadowLocalDir {
			t.Errorf("key %q SourceDir = %q, want the local tree %q even though the remote "+
				"declares a newer version:", key, c.GetSourceDir(), shadowLocalDir)
		}
	}
}

// TestScanCandyFromLocal_UnshadowedRemoteKeepsRemoteSource is the negative control: with no local
// candy of that name there is no shadow, so the remote body must still land under its ref key.
// This is what keeps the fix from degenerating into "drop remote candies".
func TestScanCandyFromLocal_UnshadowedRemoteKeepsRemoteSource(t *testing.T) {
	got, err := ScanCandyFromLocal(map[string]spec.ScannedCandy{}, nil, scanSeamsForTwin("2026.183.1359"))
	if err != nil {
		t.Fatalf("ScanCandyFromLocal: %v", err)
	}
	ref, ok := got[shadowRemoteRef]
	if !ok {
		t.Fatalf("unshadowed remote ref %q missing from scan result", shadowRemoteRef)
	}
	if ref.GetSourceDir() != shadowRemoteDir {
		t.Errorf("unshadowed remote SourceDir = %q, want %q", ref.GetSourceDir(), shadowRemoteDir)
	}
}
