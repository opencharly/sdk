package deploykit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// header_copy_remote_test.go — the unresolvable-header-COPY direction of
// materializeBuildConfigAsset.
//
// `dir` is the build context the emitted `COPY <src>` resolves against: the Containerfile is
// written to buildDir/<box>/Containerfile and the build runs with `dir` as its context (which is
// why the materialized branch returns a `.build/_buildconfig/...` path relative to `dir`). Both
// probes the function makes therefore test the very path a `COPY <relPath>` would resolve to. The
// old fallthrough returned `relPath` after BOTH had missed — a directive that could only fail
// inside the engine, with a `copier: stat: "<relPath>": no such file or directory` naming neither
// the candy nor the cache. It must fail at generate time instead.

// remoteCandyFixture builds the CandyReader view the cache-root search reads: a REMOTE candy whose
// repo@version cache lives at sourceDir. The empty SubPathPrefix is the de-submodule cutover shape
// (a root-level standalone candy), so sourceDir IS the cache root — nothing is stripped.
func remoteCandyFixture(name, sourceDir string) CandyModel {
	return NewSpecCandyModel(
		spec.CandyModel{Name: name, SourceDir: sourceDir},
		spec.CandyView{Name: name, Remote: true},
	)
}

const headerRelPath = "templates/supervisord.header.conf"

// TestMaterializeBuildConfigAsset_MissingAssetIsAHardError is the regression guard: an asset that
// is in NO resolved candy's cache root and NOT in the project tree must be a generate-time error
// naming the asset and the roots searched, never a silently emitted COPY that cannot resolve.
func TestMaterializeBuildConfigAsset_MissingAssetIsAHardError(t *testing.T) {
	dir := t.TempDir()       // the project tree — also the build context
	cacheRoot := t.TempDir() // a populated repo@version cache root, but WITHOUT this asset
	buildDir := filepath.Join(dir, ".build")

	candies := map[string]CandyModel{
		"github.com/opencharly/layer-supervisord": remoteCandyFixture("supervisord", cacheRoot),
	}

	got, err := materializeBuildConfigAsset(candies, dir, buildDir, headerRelPath)
	if err == nil {
		t.Fatalf("an asset present in no cache root and not in the project tree must be a hard error, "+
			"not `COPY %s` (which resolves to %s, just proven absent); got (%q, nil)",
			headerRelPath, filepath.Join(dir, headerRelPath), got)
	}
	// The failure has to be diagnosable WITHOUT the engine log: name the asset, where it was
	// looked for locally, and every remote root that was searched.
	for _, want := range []string{headerRelPath, filepath.Join(dir, headerRelPath), cacheRoot} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must name %q; got: %v", want, err)
		}
	}
}

// TestMaterializeBuildConfigAsset_RemoteCacheRootStillMaterializes pins the search itself: an
// asset living in the remote cache root is copied into the build context and the returned source is
// a `dir`-relative path that really exists there.
func TestMaterializeBuildConfigAsset_RemoteCacheRootStillMaterializes(t *testing.T) {
	dir := t.TempDir()
	cacheRoot := t.TempDir()
	buildDir := filepath.Join(dir, ".build")

	src := filepath.Join(cacheRoot, headerRelPath)
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("supervisord header\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	candies := map[string]CandyModel{
		"github.com/opencharly/layer-supervisord": remoteCandyFixture("supervisord", cacheRoot),
	}

	got, err := materializeBuildConfigAsset(candies, dir, buildDir, headerRelPath)
	if err != nil {
		t.Fatalf("a remote cache root carrying the asset must materialize it: %v", err)
	}
	if want := ".build/_buildconfig/" + headerRelPath; got != want {
		t.Fatalf("materialized COPY source = %q, want %q", got, want)
	}
	// The returned source is documented as build-context-relative — prove it resolves.
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(got))); err != nil {
		t.Fatalf("the returned COPY source %q must exist under the build context %s: %v", got, dir, err)
	}
}

// TestMaterializeBuildConfigAsset_LocalProjectAssetIsLeftAsAuthored pins the local half: a project
// shipping its own build-config asset keeps the as-authored relPath (unchanged behaviour).
func TestMaterializeBuildConfigAsset_LocalProjectAssetIsLeftAsAuthored(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, headerRelPath)
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local, []byte("project header\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := materializeBuildConfigAsset(
		map[string]CandyModel{"local": remoteCandyFixture("local", t.TempDir())},
		dir, filepath.Join(dir, ".build"), headerRelPath)
	if err != nil {
		t.Fatalf("a locally-shipped asset must pass through, not error: %v", err)
	}
	if got != headerRelPath {
		t.Fatalf("local asset COPY source = %q, want the as-authored %q", got, headerRelPath)
	}
}

// TestMaterializeBuildConfigAsset_NoRemoteCandySaysSo: with no remote candy in the resolved set
// there is no root to search, and the error must say that rather than print an empty list.
func TestMaterializeBuildConfigAsset_NoRemoteCandySaysSo(t *testing.T) {
	dir := t.TempDir()

	_, err := materializeBuildConfigAsset(nil, dir, filepath.Join(dir, ".build"), headerRelPath)
	if err == nil {
		t.Fatal("an unresolvable asset must error even with no remote candy in the resolved set")
	}
	if !strings.Contains(err.Error(), "no remote candy is in the resolved set") {
		t.Errorf("error must explain that nothing was searchable, not print an empty root list; got: %v", err)
	}
}

// TestRewriteHeaderCopyForRemote_UnresolvableSourceIsAnError: the wrapper must propagate, not
// swallow, the hard error — this is the directive that actually reaches the Containerfile.
func TestRewriteHeaderCopyForRemote_UnresolvableSourceIsAnError(t *testing.T) {
	dir := t.TempDir()
	candies := map[string]CandyModel{
		"github.com/opencharly/layer-supervisord": remoteCandyFixture("supervisord", t.TempDir()),
	}

	got, err := rewriteHeaderCopyForRemote(candies, dir, filepath.Join(dir, ".build"),
		"COPY "+headerRelPath+" /etc/supervisord.header.conf")
	if err == nil {
		t.Fatalf("a COPY whose source resolves nowhere must not be emitted as-authored; got (%q, nil)", got)
	}
	if !strings.Contains(err.Error(), headerRelPath) {
		t.Errorf("propagated error must still name the asset; got: %v", err)
	}
}

// TestRewriteHeaderCopyForRemote_NonCopyLinePassesThrough pins the passthrough so the new hard error
// can never over-fire on a line that is not a 3-field COPY.
func TestRewriteHeaderCopyForRemote_NonCopyLinePassesThrough(t *testing.T) {
	noRemote := map[string]CandyModel{"local": remoteCandyFixture("local", t.TempDir())}
	for _, line := range []string{
		"",
		"RUN true",
		"COPY one",
		"COPY a b c",
		"ADD " + headerRelPath + " /etc/x",
	} {
		got, err := rewriteHeaderCopyForRemote(noRemote, t.TempDir(), "/nonexistent/.build", line)
		if err != nil {
			t.Errorf("non-COPY line %q must pass through untouched, got error: %v", line, err)
		}
		if got != line {
			t.Errorf("non-COPY line %q must be returned verbatim, got %q", line, got)
		}
	}
}
