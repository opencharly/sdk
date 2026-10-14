package artifactkit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/opencharly/sdk/storekit"
	"github.com/opencharly/spec/spec"
)

// artifactkit_test.go — the reader, selector, verifier and materializer.
//
// The fixture writes a minimal plugin-artifact STORE (an OCI image layout — which is what the
// store is) so the REAL code path runs: index → manifest → config → layers, every blob verified
// by content address, the runtime view hard-linked out of the store's own bytes. Each rejection
// case fails if its check is removed and names its cause.

const (
	fixtureBinary    = "\x7fELF\x02\x01\x01\x00fake-provider-binary"
	fixtureCandyYML  = "plugin-bpf:\n    candy:\n        plugin:\n            providers:\n                - command:bpf\n"
	fixtureProviders = "command:bpf\nverb:bpf\n"
)

func fixtureMetadata(mutate func(map[string]any)) []byte {
	md := map[string]any{
		"module":               "github.com/opencharly/plugin-bpf/candy/plugin-bpf",
		"revision":             "deadbeef",
		"go_sum":               "h1:x=",
		"toolchain":            "go1.26.0",
		"served_schema_digest": "sha256:02cb24f7aabbccddeeff00112233445566778899aabbccddeeff001122334455",
		"runtime_requires":     []string{"zcat"},
		"providers":            []string{"command:bpf", "verb:bpf"},
		"platform":             map[string]any{"os": "linux", "architecture": "amd64", "libc": "glibc"},
	}
	if mutate != nil {
		mutate(md)
	}
	out, err := json.Marshal(md)
	if err != nil {
		panic(err)
	}
	return out
}

// writeArtifactStore returns a store holding one linux/amd64 artifact.
func writeArtifactStore(t *testing.T, metadata []byte) *storekit.Store {
	t.Helper()
	root := t.TempDir()
	t.Setenv("CHARLY_CACHE_DIR", root)
	st, err := storekit.OpenStore("artifacts")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(st.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st.Dir(), storekit.LayoutFile), []byte(`{"imageLayoutVersion":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	put := func(b []byte) string {
		dgst, err := st.PutBlob(b)
		if err != nil {
			t.Fatal(err)
		}
		return dgst
	}
	cfgDigest := put(metadata)
	binDigest := put([]byte(fixtureBinary))
	mfDigest := put([]byte(fixtureCandyYML))
	pvDigest := put([]byte(fixtureProviders))
	manifest := map[string]any{
		"schemaVersion": 2,
		"mediaType":     spec.MediaTypePluginArtifactManifest,
		"artifactType":  spec.ArtifactTypePlugin,
		"config":        map[string]any{"mediaType": spec.MediaTypePluginConfig, "digest": cfgDigest, "size": len(metadata)},
		"layers": []map[string]any{
			{"mediaType": spec.MediaTypePluginBinary, "digest": binDigest},
			{"mediaType": spec.MediaTypePluginManifest, "digest": mfDigest},
			{"mediaType": spec.MediaTypePluginProviders, "digest": pvDigest},
		},
	}
	rawManifest, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manDigest := put(rawManifest)
	index := map[string]any{
		"schemaVersion": 2,
		"mediaType":     spec.MediaTypePluginArtifactIndex,
		"artifactType":  spec.ArtifactTypePlugin,
		"manifests": []map[string]any{{
			"mediaType":   spec.MediaTypePluginArtifactManifest,
			"digest":      manDigest,
			"platform":    map[string]any{"os": "linux", "architecture": "amd64"},
			"annotations": map[string]string{"org.opencontainers.image.ref.name": "test"},
		}},
	}
	rawIndex, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st.Dir(), "index.json"), rawIndex, 0o644); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestVerifyAcceptsAWellFormedArtifact(t *testing.T) {
	st := writeArtifactStore(t, fixtureMetadata(nil))
	v, err := Verify(st, "", "linux/amd64", "glibc")
	if err != nil {
		t.Fatalf("Verify rejected a well-formed artifact: %v", err)
	}
	if v.Platform != "linux/amd64" {
		t.Fatalf("platform = %q, want linux/amd64", v.Platform)
	}
	if v.Metadata.Module == "" {
		t.Fatalf("metadata not decoded: %+v", v.Metadata)
	}
	if len(v.Platforms) != 1 || v.Platforms[0] != "linux/amd64" {
		t.Fatalf("Platforms = %v", v.Platforms)
	}
}

func TestVerifyRefusesAWrongPlatformAndNamesTheAlternatives(t *testing.T) {
	st := writeArtifactStore(t, fixtureMetadata(nil))
	_, err := Verify(st, "", "linux/s390x", "glibc")
	if err == nil {
		t.Fatal("Verify accepted a platform the artifact does not carry")
	}
	for _, want := range []string{"linux/s390x", "linux/amd64"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q must name %s", err, want)
		}
	}
}

// TestVerifyRefusesATamperedBlob: the descriptor's digest IS the identity, so one flipped byte is
// caught before anything reads the payload. (The loader's older baked identity was
// `<size>B mtime <t>`, which two different binaries can share; content addressing is the fix.)
func TestVerifyRefusesATamperedBlob(t *testing.T) {
	st := writeArtifactStore(t, fixtureMetadata(nil))
	sum := sha256.Sum256([]byte(fixtureBinary))
	blob, err := st.BlobPath("sha256:" + hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(blob)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] ^= 0xff
	if err := os.WriteFile(blob, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(st, "", "linux/amd64", "glibc"); err == nil {
		t.Fatal("Verify accepted a blob whose bytes do not match its content address")
	} else if !strings.Contains(err.Error(), "content address") {
		t.Errorf("refusal %q must name the content-address mismatch", err)
	}
}

func TestVerifyRefusesAMissingServedSchemaDigest(t *testing.T) {
	st := writeArtifactStore(t, fixtureMetadata(func(m map[string]any) { delete(m, "served_schema_digest") }))
	_, err := Verify(st, "", "linux/amd64", "glibc")
	if err == nil {
		t.Fatal("Verify accepted an artifact with no served_schema_digest")
	}
	if !strings.Contains(err.Error(), "served_schema_digest") {
		t.Errorf("refusal %q must name served_schema_digest", err)
	}
}

func TestVerifyRefusesAPlatformDisagreement(t *testing.T) {
	st := writeArtifactStore(t, fixtureMetadata(func(m map[string]any) {
		m["platform"] = map[string]any{"os": "linux", "architecture": "arm64", "libc": "glibc"}
	}))
	_, err := Verify(st, "", "linux/amd64", "glibc")
	if err == nil {
		t.Fatal("Verify accepted a config platform disagreeing with the descriptor's")
	}
	if !strings.Contains(err.Error(), "disagrees") {
		t.Errorf("refusal %q must name the disagreement", err)
	}
}

func TestVerifyRefusesALibcMismatch(t *testing.T) {
	st := writeArtifactStore(t, fixtureMetadata(nil))
	_, err := Verify(st, "", "linux/amd64", "musl")
	if err == nil {
		t.Fatal("Verify accepted a glibc artifact on a musl venue")
	}
	if !strings.Contains(err.Error(), "musl") || !strings.Contains(err.Error(), "glibc") {
		t.Errorf("refusal %q must name both libcs", err)
	}
	// An unknown venue libc cannot be compared, so verification SUCCEEDS and the declared fact
	// is the caller's to report — never silently ignored.
	v, err := Verify(st, "", "linux/amd64", "")
	if err != nil {
		t.Fatalf("Verify with an unknown venue libc must not fail: %v", err)
	}
	if v.Metadata.Platform.Libc != "glibc" {
		t.Fatalf("declared libc = %q, want glibc reported", v.Metadata.Platform.Libc)
	}
}

func TestVerifyRefusesAnAbsentRuntimeRequires(t *testing.T) {
	st := writeArtifactStore(t, fixtureMetadata(func(m map[string]any) { delete(m, "runtime_requires") }))
	_, err := Verify(st, "", "linux/amd64", "glibc")
	if err == nil {
		t.Fatal("Verify accepted an artifact that declares no runtime_requires at all")
	}
	if !strings.Contains(err.Error(), "runtime_requires") {
		t.Errorf("refusal %q must name runtime_requires", err)
	}
	empty := writeArtifactStore(t, fixtureMetadata(func(m map[string]any) { m["runtime_requires"] = []string{} }))
	v, err := Verify(empty, "", "linux/amd64", "glibc")
	if err != nil {
		t.Fatalf("an explicit empty runtime_requires is a CLAIM and must be accepted: %v", err)
	}
	if v.Metadata.Runtime_requires == nil {
		t.Fatal("an empty claim decoded as nil, which is indistinguishable from unaudited")
	}
}

// TestMaterializeCreatesAHardlinkedView is the mandate's property, measured through the real
// materializer: the three files ARE the store's own bytes (same file), the binary is executable,
// and the bytes are counted once.
func TestMaterializeCreatesAHardlinkedView(t *testing.T) {
	st := writeArtifactStore(t, fixtureMetadata(nil))
	view, err := Materialize(st, "", "linux/amd64", "glibc")
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if view.Name != "plugin-bpf" {
		t.Fatalf("leaf name = %q, want plugin-bpf (from the metadata module)", view.Name)
	}
	if !view.AllHardlinked {
		t.Fatal("the materialized view does not share the store's bytes")
	}
	for path, want := range map[string]string{
		view.Binary:    fixtureBinary,
		view.Providers: fixtureProviders,
		view.Manifest:  fixtureCandyYML,
	} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if string(got) != want {
			t.Fatalf("%s = %q, want %q", path, got, want)
		}
	}
	st2, err := os.Stat(view.Binary)
	if err != nil {
		t.Fatal(err)
	}
	if st2.Mode().Perm()&0o111 == 0 {
		t.Fatalf("materialized binary mode = %v, want it executable (mode is set on the shared blob: it is not part of a digest)", st2.Mode().Perm())
	}
	// The view adds directory entries and NO bytes: each of the three files IS its store blob (asserted
	// above through AllHardlinked), and the store holds exactly the four blobs those three payloads plus
	// the metadata need. This states the "cached once" property directly instead of through a walker —
	// the generic tree-walk helper lives in ONE place (storekit's tests) after the duplication finding.
	blobs, err := st.Blobs()
	if err != nil {
		t.Fatal(err)
	}
	// FIVE: the four payloads (binary, candy.yml, .providers, metadata config) plus the manifest blob
	// that references them. The first version of this assertion said four — I forgot the manifest, which
	// is the same "a number asserted beside a list" defect the sweep keeps finding, caught this time by
	// the command's own output.
	if len(blobs) != 5 {
		t.Fatalf("store holds %d blobs, want 5 (binary, candy.yml, .providers, metadata config, manifest): %v", len(blobs), blobs)
	}
}

func TestMaterializeWritesNothingWhenVerificationFails(t *testing.T) {
	st := writeArtifactStore(t, fixtureMetadata(func(m map[string]any) { delete(m, "runtime_requires") }))
	if _, err := Materialize(st, "", "linux/amd64", "glibc"); err == nil {
		t.Fatal("Materialize accepted an artifact Verify refuses")
	}
	entries, err := os.ReadDir(filepath.Join(st.Dir(), "views"))
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("a failed materialization left a view behind: %d entries", len(entries))
	}
}

// TestDescriptorMatchesAcceptsTheHostLevelForAnUnqualifiedRequest makes the host-level term OBSERVABLE,
// which is what round-2's finding 3 turned on: the battery claimed a kill for `p.Variant == host`
// through TestDescriptorMatchesNeverAcceptsAHigherLevelSilently, but that test CLEARS the host level
// (GOAMD64=""), which makes the term inert — removing it left every assertion passing. Here the level
// IS set, so the term is the only reason the unqualified request matches.
func TestDescriptorMatchesAcceptsTheHostLevelForAnUnqualifiedRequest(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skipf("the GOAMD64 branch is keyed on runtime.GOARCH=amd64; this host is %s", runtime.GOARCH)
	}
	t.Setenv("GOAMD64", "v4")
	v4 := &struct {
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
		Variant      string `json:"variant"`
	}{OS: "linux", Architecture: "amd64", Variant: "v4"}
	if !descriptorMatches(v4, "linux/amd64") {
		t.Fatal("an unqualified request on a v4-declaring host must accept the v4 entry: that is the `p.Variant == host` term, and this test is the only one whose inputs reach it")
	}
	t.Setenv("GOAMD64", "")
	if descriptorMatches(v4, "linux/amd64") {
		t.Fatal("with no declared level a v4 entry must NOT match an unqualified request")
	}
}
