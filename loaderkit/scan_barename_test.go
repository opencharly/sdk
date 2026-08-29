package loaderkit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// scan_barename_test.go — the bare-name keying gate for ScanCandyFromLocal.
//
// A remote candy is registered under its @github ref key, but a transitive bare-name
// dep (a plain-name require:/candy: entry inside a pinned tag, e.g. `versatiles-style`
// inside pod-versatiles-frontend's list) resolves by NAME. ScanCandyFromLocal must
// ALSO key the remote candy by its Model.Name so the build's global candy order
// (ResolveCandyOrder looks up layers[name]) finds it. Before the fix the refs list
// registered only the @github path key, and every transitive bare-name dep failed
// with "unknown candy".

const (
	bareNameCandyName = "versatiles-style"
	bareNameRef       = "github.com/opencharly/layer-versatiles-style/candy/layer-versatiles-style"
)

// scanSeamsForBareName serves exactly one remote materialization whose Model.Name
// (the manifest node key) differs from its @github ref key — the root-level
// standalone candy repo shape.
func scanSeamsForBareName() spec.ScanSeams {
	return spec.ScanSeams{
		CollectRemoteRefs: func(map[string]spec.ScannedCandy) ([]spec.RemoteDownload, error) {
			return []spec.RemoteDownload{{
				RepoPath: "github.com/opencharly/layer-versatiles-style",
				Version:  "v2026.240.0001",
				Refs:     []string{bareNameRef},
			}}, nil
		},
		EnsureRepo: func(repoPath, version string) (string, error) {
			return "/cache/repos/layer-versatiles-style@v1", nil
		},
		ScanRemote: func(cacheDir, repoPath string, wantRefs map[string]bool) (map[string]spec.ScannedCandy, error) {
			out := make(map[string]spec.ScannedCandy, len(wantRefs))
			for ref := range wantRefs {
				out[ref] = spec.ScannedCandy{
					Model: spec.CandyModel{
						Name:      bareNameCandyName,
						Version:   "2026.240.0001",
						SourceDir: "/cache/repos/layer-versatiles-style@v1",
					},
					View: spec.CandyView{Name: bareNameCandyName},
				}
			}
			return out, nil
		},
	}
}

// TestScanCandyFromLocal_BareNameKeying proves a remote candy whose Model.Name differs
// from its @github ref key is reachable under BOTH keys: the ref key (the refs list
// registration) AND the bare name (the transitive bare-name dep resolution).
func TestScanCandyFromLocal_BareNameKeying(t *testing.T) {
	got, err := ScanCandyFromLocal(nil, nil, scanSeamsForBareName())
	if err != nil {
		t.Fatalf("ScanCandyFromLocal: %v", err)
	}

	// The @github ref key must materialize the remote body.
	if _, ok := got[bareNameRef]; !ok {
		t.Fatalf("remote candy not registered under its ref key %q", bareNameRef)
	}
	// The bare name must ALSO materialize the same body — a transitive bare-name dep
	// (`versatiles-style` in a sibling's list) resolves through it.
	byName, ok := got[bareNameCandyName]
	if !ok {
		t.Fatalf("remote candy not keyed by its bare name %q — transitive bare-name deps would fail with 'unknown candy'", bareNameCandyName)
	}
	if byName.GetName() != bareNameCandyName {
		t.Fatalf("bare-name key materializes name %q, want %q", byName.GetName(), bareNameCandyName)
	}
}
