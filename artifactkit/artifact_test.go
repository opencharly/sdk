package artifactkit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
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
	// The view adds directory entries and NO bytes.
	naive, unique := treeBytes(t, st.Dir())
	linked := int64(len(fixtureBinary) + len(fixtureProviders) + len(fixtureCandyYML))
	if naive-unique != linked {
		t.Fatalf("naive(%d) - unique(%d) = %d, want exactly the three linked payloads (%d)", naive, unique, naive-unique, linked)
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

// treeBytes is (naive total, unique total) — their difference is the shared-byte set. Identity,
// not name, decides "unique", so it is portable across the CI matrix.
func treeBytes(t *testing.T, dir string) (int64, int64) {
	t.Helper()
	var naive, unique int64
	var seen []os.FileInfo
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		naive += info.Size()
		for _, s := range seen {
			if os.SameFile(s, info) {
				return nil
			}
		}
		seen = append(seen, info)
		unique += info.Size()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return naive, unique
}

// --- the CONSERVATIVE VARIANT RULE and the multi-artifact SELECTOR, which the single-manifest
// fixtures above could not exercise: every earlier fixture writes ONE manifest with no variant and
// every earlier test passes selector == "", so deleting the preference loop, the selector filter or
// the multi-artifact refusal would leave the whole suite green. The three tests below are written to
// FAIL on exactly that mutation, which is the only thing that makes them coverage.

// writeTaggedMultiPlatformStore writes a store holding several platform entries that share ONE tag —
// the normal multi-platform artifact shape, and the only shape in which the variant-preference loop
// is reachable: an unqualified request then matches more than one descriptor.
func writeTaggedMultiPlatformStore(t *testing.T, variants ...string) (*storekit.Store, map[string]string) {
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
	byVariant := map[string]string{}
	descs := make([]map[string]any, 0, len(variants))
	for _, v := range variants {
		md := fixtureMetadata(func(m map[string]any) {
			p := m["platform"].(map[string]any)
			if v == "" {
				delete(p, "variant")
			} else {
				p["variant"] = v
			}
		})
		cfg := put(md)
		bin := put([]byte(fixtureBinary + v))
		mf := put([]byte(fixtureCandyYML))
		pv := put([]byte(fixtureProviders))
		man := map[string]any{
			"schemaVersion": 2,
			"mediaType":     spec.MediaTypePluginArtifactManifest,
			"artifactType":  spec.ArtifactTypePlugin,
			"config":        map[string]any{"mediaType": spec.MediaTypePluginConfig, "digest": cfg},
			"layers": []map[string]any{
				{"mediaType": spec.MediaTypePluginBinary, "digest": bin},
				{"mediaType": spec.MediaTypePluginManifest, "digest": mf},
				{"mediaType": spec.MediaTypePluginProviders, "digest": pv},
			},
		}
		raw, err := json.Marshal(man)
		if err != nil {
			t.Fatal(err)
		}
		manDigest := put(raw)
		byVariant[v] = manDigest
		platform := map[string]any{"os": "linux", "architecture": "amd64"}
		if v != "" {
			platform["variant"] = v
		}
		descs = append(descs, map[string]any{
			"mediaType": spec.MediaTypePluginArtifactManifest,
			"digest":    manDigest,
			"platform":  platform,
			// ONE tag for every platform entry, as a multi-platform artifact is published.
			"annotations": map[string]string{"org.opencontainers.image.ref.name": "test"},
		})
	}
	idx := map[string]any{
		"schemaVersion": 2,
		"mediaType":     spec.MediaTypePluginArtifactIndex,
		"artifactType":  spec.ArtifactTypePlugin,
		"manifests":     descs,
	}
	raw, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st.Dir(), "index.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return st, byVariant
}

// TestSelectArtifactTakesTheBaselineForAnUnqualifiedRequest pins the DOCUMENTED rule for the common
// case: an unqualified request that matches both a baseline entry and the host's declared level is
// answered by the BASELINE — always safe, and the reason the exact-variant branch is keyed on the
// REQUEST rather than on the resolved host platform. If someone "improves" this by resolving the
// request through HostPlatform and preferring the tuned entry, this test fails: that is a design
// decision, not an accident, and it is now pinned.
func TestSelectArtifactTakesTheBaselineForAnUnqualifiedRequest(t *testing.T) {
	t.Setenv("GOAMD64", "v4")
	st, byVariant := writeTaggedMultiPlatformStore(t, "", "v4")
	idx, err := ReadIndex(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Manifests) != 2 || platformString(idx.Manifests[0].Platform) != "linux/amd64" {
		t.Fatalf("fixture must list the baseline first among two manifests, got %v", idx.Manifests)
	}
	sel, _, err := SelectArtifact(idx, "test", "linux/amd64")
	if err != nil {
		t.Fatalf("SelectArtifact: %v", err)
	}
	if got := idx.Manifests[sel].Digest; got != byVariant[""] {
		t.Fatalf("unqualified request selected %s, want the BASELINE %s", got, byVariant[""])
	}
}

// TestSelectArtifactPrefersTheExactVariantForAQualifiedRequest kills the exact-variant branch: this is
// the ONLY reachable path for it, because an unqualified request matches the baseline too and the rule
// above answers the baseline. Here the caller asks for linux/amd64/v4 explicitly, both entries match,
// the baseline is listed FIRST, and the exact-variant entry must win — delete the branch and the first
// match (the baseline) is returned instead.
func TestSelectArtifactPrefersTheExactVariantForAQualifiedRequest(t *testing.T) {
	st, byVariant := writeTaggedMultiPlatformStore(t, "", "v4")
	idx, err := ReadIndex(st)
	if err != nil {
		t.Fatal(err)
	}
	if platformString(idx.Manifests[0].Platform) != "linux/amd64" {
		t.Fatalf("fixture must list the baseline first, got %s", platformString(idx.Manifests[0].Platform))
	}
	sel, _, err := SelectArtifact(idx, "test", "linux/amd64/v4")
	if err != nil {
		t.Fatalf("SelectArtifact(qualified): %v", err)
	}
	if got := idx.Manifests[sel].Digest; got != byVariant["v4"] {
		t.Fatalf("qualified v4 request selected %s (the first match), want the exact-variant entry %s — the exact-variant branch is not deciding",
			got, byVariant["v4"])
	}
}

// TestSelectArtifactByDigestSelectsExactlyOnePlatform kills the SELECTOR filter: with the same tag on
// both entries, a digest selector must resolve to that one platform, not to the variant-preferred one.
func TestSelectArtifactByDigestSelectsExactlyOnePlatform(t *testing.T) {
	t.Setenv("GOAMD64", "v4")
	st, byVariant := writeTaggedMultiPlatformStore(t, "", "v4")
	idx, err := ReadIndex(st)
	if err != nil {
		t.Fatal(err)
	}
	sel, _, err := SelectArtifact(idx, byVariant[""], "linux/amd64")
	if err != nil {
		t.Fatalf("SelectArtifact by digest: %v", err)
	}
	if got := idx.Manifests[sel].Digest; got != byVariant[""] {
		t.Fatalf("digest selector %s resolved to %s — the selector is not filtering", byVariant[""], got)
	}
}

// TestSelectArtifactRefusesAnUnqualifiedMultiArtifactStore kills the refusal in SelectArtifact: with
// no selector and more than one artifact, "the first match" could serve a DIFFERENT plugin's bytes.
func TestSelectArtifactRefusesAnUnqualifiedMultiArtifactStore(t *testing.T) {
	t.Setenv("GOAMD64", "v4")
	st, _ := writeTaggedMultiPlatformStore(t, "", "v4")
	idx, err := ReadIndex(st)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = SelectArtifact(idx, "", "linux/amd64")
	if err == nil {
		t.Fatal("an unqualified request was accepted against a 2-artifact store: the refusal is gone and 'the first match' can serve another plugin's bytes")
	}
	if !strings.Contains(err.Error(), "2 artifacts") {
		t.Errorf("refusal %q must name the artifact count", err)
	}
}

// TestDescriptorMatchesNeverAcceptsAHigherLevelSilently kills the host-level branch of
// descriptorMatches: v9 is a level GOAMD64 CANNOT declare, so an unqualified request against a store
// holding only v9 must be refused rather than picking bytes built for a higher level — that is a
// SIGILL, strictly worse than a refusal. (GOAMD64 is cleared here so "host level" is empty.)
func TestDescriptorMatchesNeverAcceptsAHigherLevelSilently(t *testing.T) {
	t.Setenv("GOAMD64", "")
	v9 := &struct {
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
		Variant      string `json:"variant"`
	}{OS: "linux", Architecture: "amd64", Variant: "v9"}
	if descriptorMatches(v9, "linux/amd64") {
		t.Fatal("an unqualified request accepted a v9 entry on a host with no declared level — that is the silent-SIGILL path")
	}
	if !descriptorMatches(v9, "linux/amd64/v9") {
		t.Fatal("an explicit v9 request must accept the v9 entry")
	}
	if descriptorMatches(v9, "linux/amd64/v2") {
		t.Fatal("an explicit v2 request must not accept a v9 entry")
	}
	st, _ := writeTaggedMultiPlatformStore(t, "v9")
	idx, err := ReadIndex(st)
	if err != nil {
		t.Fatal(err)
	}
	_, platforms, err := SelectArtifact(idx, "test", "linux/amd64")
	if err == nil {
		t.Fatal("a store holding only a v9 entry served an unqualified request")
	}
	if len(platforms) != 1 || platforms[0] != "linux/amd64/v9" {
		t.Errorf("refusal must enumerate the platforms it does carry, got %v", platforms)
	}
}

// TestArtifactsAndOpenStoreDir proves the two exported pieces of the reader that had no test that
// could fail: Artifacts() enumerating the index, and OpenStoreDir() reopening a store from a path so a
// caller can ReadTree/Materialize through it without knowing the cache root.
func TestArtifactsAndOpenStoreDir(t *testing.T) {
	st := writeArtifactStore(t, fixtureMetadata(nil))
	arts, err := Artifacts(st)
	if err != nil {
		t.Fatalf("Artifacts: %v", err)
	}
	if len(arts) != 1 {
		t.Fatalf("Artifacts returned %d entries, want 1", len(arts))
	}
	if arts[0].Tag != "test" {
		t.Errorf("Tag = %q, want the index annotation's tag", arts[0].Tag)
	}
	if arts[0].Platform != "linux/amd64" {
		t.Errorf("Platform = %q, want linux/amd64", arts[0].Platform)
	}
	if arts[0].Digest == "" {
		t.Error("Digest is empty: Artifacts must report the manifest digest, which is the selector a caller uses")
	}

	// OpenStoreDir reopens the SAME tree by path — the caller-side route that does not resolve roots.
	reopened := storekit.OpenStoreDir(st.Dir())
	if reopened.Dir() != st.Dir() {
		t.Fatalf("OpenStoreDir().Dir() = %q, want %q", reopened.Dir(), st.Dir())
	}
	idx, err := ReadIndex(reopened)
	if err != nil {
		t.Fatalf("ReadIndex via the reopened store: %v", err)
	}
	if len(idx.Manifests) != 1 || idx.Manifests[0].Digest != arts[0].Digest {
		t.Fatalf("the reopened store sees a different index: %v", idx.Manifests)
	}
	v, err := Verify(reopened, arts[0].Digest, "linux/amd64", "glibc")
	if err != nil {
		t.Fatalf("Verify by the digest Artifacts reported must work end to end: %v", err)
	}
	if v.Metadata.Module == "" {
		t.Fatal("verification through the reopened store decoded no metadata")
	}
}

// TestExportedTypesExist pins the three exported DATA types of this package at COMPILE time, which is
// the pairing the export enumeration asks for: a composite literal of the named type cannot be
// inferred away, so renaming or removing ArtifactRef, Verified or View fails this test's build rather
// than passing silently. Their FIELDS are asserted behaviourally elsewhere (Tag/Platform/Digest in
// TestArtifactsAndOpenStoreDir, Metadata in TestVerifyAcceptsAWellFormedArtifact, Name/Dir/Binary in
// TestMaterializeCreatesAHardlinkedView); this is only the surface pin, deliberately not dressed up as
// behavioural coverage.
func TestExportedTypesExist(t *testing.T) {
	_ = ArtifactRef{}
	_ = Verified{}
	_ = View{}
}
