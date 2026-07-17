package kit

import (
	"testing"

	"github.com/opencharly/sdk/spec"
)

// local_image_test.go — P12a: relocated from charly/local_image_test.go.

// TestParseLocalImagesJSON_DedupByID covers the root fix for the keep_images
// over-removal bug: podman emits ONE ROW PER TAG (each row's Names already lists
// every tag on that id), so the parser must collapse rows to ONE entry per
// distinct image id with the tag refs merged — not N near-identical entries.
func TestParseLocalImagesJSON_DedupByID(t *testing.T) {
	// Two rows for one id (id "ccc", two tags), each row carrying BOTH tags in
	// Names — exactly podman's row-per-tag shape. Plus a distinct id "ddd".
	js := []byte(`[
		{"Id":"ccc","Names":["ghcr/check-pod:2026.150.0916","ghcr/check-pod:2026.150.0836"],"Labels":{"ai.opencharly.image":"check-pod","ai.opencharly.version":"2026.155.1801"}},
		{"Id":"ccc","Names":["ghcr/check-pod:2026.150.0916","ghcr/check-pod:2026.150.0836"],"Labels":{"ai.opencharly.image":"check-pod","ai.opencharly.version":"2026.155.1801"}},
		{"Id":"ddd","Names":["ghcr/other:2026.001.0001"],"Labels":{"ai.opencharly.image":"other"}}
	]`)
	imgs, err := ParseLocalImagesJSON(js)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(imgs) != 2 {
		t.Fatalf("got %d entries, want 2 (one per distinct id): %+v", len(imgs), imgs)
	}
	// id ccc: the two duplicate rows collapse to one entry with BOTH tags
	// (deduped, not 4 copies), labels preserved.
	if imgs[0].ID != "ccc" || len(imgs[0].Names) != 2 {
		t.Fatalf("entry 0 = %+v, want id ccc with 2 merged tags", imgs[0])
	}
	if imgs[0].Labels["ai.opencharly.image"] != "check-pod" || imgs[0].Labels["ai.opencharly.version"] != "2026.155.1801" {
		t.Fatalf("entry 0 labels not preserved: %+v", imgs[0].Labels)
	}
	if imgs[1].ID != "ddd" || len(imgs[1].Names) != 1 {
		t.Fatalf("entry 1 = %+v, want id ddd with 1 tag", imgs[1])
	}
}

// TestParseLocalImagesJSON_DockerRepoTags covers the docker shape (RepoTags
// instead of Names) and that distinct untagged (empty-id) rows do NOT merge.
func TestParseLocalImagesJSON_DockerRepoTags(t *testing.T) {
	js := []byte(`[
		{"ID":"aaa","RepoTags":["ghcr/foo:2026.001.0001"],"Labels":{"ai.opencharly.image":"foo"}},
		{"Id":"","Names":["<none>:<none>"]},
		{"Id":"","Names":["<none>:<none>"]}
	]`)
	imgs, err := ParseLocalImagesJSON(js)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// 1 foo (RepoTags) + 2 distinct empty-id rows kept separate = 3 entries.
	if len(imgs) != 3 {
		t.Fatalf("got %d entries, want 3 (docker RepoTags + 2 unmerged empty-id): %+v", len(imgs), imgs)
	}
	if imgs[0].ID != "aaa" || len(imgs[0].Names) != 1 || imgs[0].Names[0] != "ghcr/foo:2026.001.0001" {
		t.Fatalf("entry 0 = %+v, want id aaa with RepoTags ref", imgs[0])
	}
}

// TestShortNameMatchesRef — P12a: relocated from charly/checkrun_charly_verbs_test.go
// (it tests this package's unexported shortNameMatchesRef).
func TestShortNameMatchesRef(t *testing.T) {
	cases := []struct {
		fullRef string
		short   string
		want    bool
	}{
		{"ghcr.io/opencharly/jupyter:latest", "jupyter", true},
		{"ghcr.io/opencharly/jupyter", "jupyter", true}, // no tag
		{"localhost/jupyter:v2", "jupyter", true},
		{"jupyter:latest", "jupyter", true}, // no registry
		{"ghcr.io/opencharly/jupyter:latest", "filebrowser", false},
		{"ghcr.io/opencharly/something-jupyter:latest", "jupyter", false}, // not a trailing match
	}
	for _, tc := range cases {
		got := shortNameMatchesRef(tc.fullRef, tc.short)
		if got != tc.want {
			t.Errorf("shortNameMatchesRef(%q, %q) = %v, want %v", tc.fullRef, tc.short, got, tc.want)
		}
	}
}

// ResolveShellImageRef branch coverage (sdk#68 review round — the helper shipped
// with none; these five cases FAIL without the function's branch logic).
// ListLocalImages is stubbed via its package-level var (same pattern as
// LocalImageExists/DetectGPU testability notes on the var itself).
func TestResolveShellImageRef_TagEmptyLocalHit(t *testing.T) {
	orig := ListLocalImages
	defer func() { ListLocalImages = orig }()
	ListLocalImages = func(engine string) ([]LocalImageInfo, error) {
		return []LocalImageInfo{{
			ID:    "sha256:aa",
			Names: []string{"localhost/jupyter:2026.190.1200", "localhost/jupyter:2026.191.0800"},
			Labels: map[string]string{
				spec.LabelBox:     "jupyter",
				spec.LabelVersion: "2026.185.0000",
			},
		}}, nil
	}
	got := ResolveShellImageRef("ghcr.io/opencharly", "jupyter", "")
	// Local CalVer resolution wins over the registry fallback; newest tag-CalVer picked.
	if got != "localhost/jupyter:2026.191.0800" {
		t.Errorf("tag-empty local-hit: got %q, want the newest local CalVer ref", got)
	}
}

func TestResolveShellImageRef_TagEmptyLocalMissWithRegistry(t *testing.T) {
	orig := ListLocalImages
	defer func() { ListLocalImages = orig }()
	ListLocalImages = func(engine string) ([]LocalImageInfo, error) { return nil, nil }
	got := ResolveShellImageRef("ghcr.io/opencharly", "jupyter", "")
	if got != "ghcr.io/opencharly/jupyter" {
		t.Errorf("tag-empty local-miss + registry: got %q, want tagless registry/name", got)
	}
}

func TestResolveShellImageRef_TagEmptyLocalMissNoRegistry(t *testing.T) {
	orig := ListLocalImages
	defer func() { ListLocalImages = orig }()
	ListLocalImages = func(engine string) ([]LocalImageInfo, error) { return nil, nil }
	got := ResolveShellImageRef("", "jupyter", "")
	if got != "jupyter" {
		t.Errorf("tag-empty local-miss no-registry: got %q, want bare short name", got)
	}
}

func TestResolveShellImageRef_TagSetWithRegistry(t *testing.T) {
	// tag-set paths must NOT consult local storage at all.
	orig := ListLocalImages
	defer func() { ListLocalImages = orig }()
	ListLocalImages = func(engine string) ([]LocalImageInfo, error) {
		t.Fatal("tag-set branch must not list local images")
		return nil, nil
	}
	got := ResolveShellImageRef("ghcr.io/opencharly", "jupyter", "2026.198.0001")
	if got != "ghcr.io/opencharly/jupyter:2026.198.0001" {
		t.Errorf("tag-set + registry: got %q", got)
	}
}

func TestResolveShellImageRef_TagSetNoRegistry(t *testing.T) {
	orig := ListLocalImages
	defer func() { ListLocalImages = orig }()
	ListLocalImages = func(engine string) ([]LocalImageInfo, error) {
		t.Fatal("tag-set branch must not list local images")
		return nil, nil
	}
	got := ResolveShellImageRef("", "jupyter", "2026.198.0001")
	if got != "jupyter:2026.198.0001" {
		t.Errorf("tag-set no-registry: got %q", got)
	}
}
