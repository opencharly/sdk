package kit

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestFetchArtifact_ExtensionIsHonoured proves the ONE behaviour that differs
// between FetchArtifact and the FetchQcow2 it was extracted from: the cached
// file's suffix. Two artifacts fetched from the SAME url under two extensions
// must land on two distinct paths, both content-addressed by sha256(url) —
// which is what stops an installer ISO and a cloud image from sharing a cache
// entry, and what makes a `file` on a cache directory during an incident mean
// something.
func TestFetchArtifact_ExtensionIsHonoured(t *testing.T) {
	content := []byte(strings.Repeat("INSTALLER-ISO-BYTES ", 64))
	sum := sha256.Sum256(content)
	srv := qcow2RangeServer(t, content)
	defer srv.Close()

	cacheDir := t.TempDir()
	src := spec.VmSource{URL: srv.URL + "/omarchy.iso", Cache: cacheDir}
	src.Checksum.Value = hex.EncodeToString(sum[:])

	got, err := FetchArtifact(src, ".iso")
	if err != nil {
		t.Fatalf("FetchArtifact(.iso): %v", err)
	}
	if filepath.Ext(got.Path) != ".iso" {
		t.Fatalf("cached path %q does not end in .iso", got.Path)
	}
	urlHash := sha256.Sum256([]byte(src.URL))
	wantPath := filepath.Join(cacheDir, hex.EncodeToString(urlHash[:])+".iso")
	if got.Path != wantPath {
		t.Fatalf("cache path\n got: %s\nwant: %s", got.Path, wantPath)
	}
	if got.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha256 got %s want %s", got.SHA256, hex.EncodeToString(sum[:]))
	}
	body, err := os.ReadFile(got.Path)
	if err != nil {
		t.Fatalf("reading cached artifact: %v", err)
	}
	if string(body) != string(content) {
		t.Fatalf("cached bytes differ from served bytes")
	}

	// The SAME url under a different extension is a DIFFERENT cache entry, not a
	// hit on the .iso one. A shared entry would hand a qcow2 consumer an ISO.
	q, err := FetchArtifact(src, ".qcow2")
	if err != nil {
		t.Fatalf("FetchArtifact(.qcow2): %v", err)
	}
	if q.Path == got.Path {
		t.Fatalf("two extensions collided on one cache path: %s", q.Path)
	}
}

// TestFetchQcow2_StillWritesQcow2Suffix pins the face: the extraction must not
// have changed where an EXISTING cloud-image consumer finds its cached file.
// Every VM built from a cloud_image source has a populated ~/.cache/charly/
// vm-images with *.qcow2 entries; a silent suffix change would re-download
// every one of them.
func TestFetchQcow2_StillWritesQcow2Suffix(t *testing.T) {
	content := []byte(strings.Repeat("CLOUD-IMAGE-BYTES ", 64))
	sum := sha256.Sum256(content)
	srv := qcow2RangeServer(t, content)
	defer srv.Close()

	cacheDir := t.TempDir()
	src := spec.VmSource{URL: srv.URL + "/cloud.img", Cache: cacheDir}
	src.Checksum.Value = hex.EncodeToString(sum[:])

	got, err := FetchQcow2(src)
	if err != nil {
		t.Fatalf("FetchQcow2: %v", err)
	}
	urlHash := sha256.Sum256([]byte(src.URL))
	wantPath := filepath.Join(cacheDir, hex.EncodeToString(urlHash[:])+".qcow2")
	if got.Path != wantPath {
		t.Fatalf("FetchQcow2 cache path changed\n got: %s\nwant: %s", got.Path, wantPath)
	}
}

// TestFetchArtifact_RejectsEmptyExt proves the guard is real. An empty ext would
// content-address onto a bare hash with no suffix — reachable only by a caller
// bug, but silently poisoning the shared cache directory if it were allowed.
//
// The source is a LIVE test server that would otherwise fetch successfully, so
// the guard is the only thing that can produce the error. An unreachable URL
// would make this test pass with the guard deleted — it did, in a negative
// control, which is how this shape was chosen.
func TestFetchArtifact_RejectsEmptyExt(t *testing.T) {
	content := []byte("WOULD-FETCH-FINE")
	sum := sha256.Sum256(content)
	srv := qcow2RangeServer(t, content)
	defer srv.Close()

	src := spec.VmSource{URL: srv.URL + "/x.iso", Cache: t.TempDir()}
	src.Checksum.Value = hex.EncodeToString(sum[:])

	// Control: the same source WITH an ext fetches without error.
	if _, err := FetchArtifact(src, ".iso"); err != nil {
		t.Fatalf("control fetch must succeed, got: %v", err)
	}
	if _, err := FetchArtifact(src, ""); err == nil {
		t.Fatal("FetchArtifact with an empty ext must be an error")
	}
}
