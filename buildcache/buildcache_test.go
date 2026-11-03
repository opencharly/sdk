package buildcache

// buildcache_test.go — the PACKAGE half of phase 1's two-claim proof.
//
// These are mechanism claims, so a test is their instrument (R7): each one is written to FAIL
// against the pre-cutover behaviour it replaces, and the ones that matter most are named as such
// in their comments. The claims, in order:
//
//   1. The key COVERS ITS INPUTS — a change to the module source, to go.sum, to the toolchain, or
//      to any build-env axis changes the key. (This is the property the deleted path-key could not
//      have: it was unchanged by every one of those, which is why a stamp sidecar had to exist.)
//   2. A HIT PERFORMS NO BUILD — observable as the absence of work, never as a success exit.
//   3. A MISS ON A CHANGED INPUT — an edit to the source rekeys and rebuilds.
//   4. THE ENTRY SHAPE — the store holds a keyed entry naming the payload, the payload is
//      content-addressed, and the view SHARES the payload's inode (bytes exist once).
//   5. NO SIDECAR — there is no `.stamp` beside the view and no `.lock` under the store tree. This
//      is the R5 half of the claim, asserted positively so a future regression cannot reintroduce
//      the bespoke format unnoticed.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/opencharly/sdk/storekit"
)

// testModule writes a minimal, dependency-free Go module at dir and returns its source dir. It is
// deliberately NOT a plugin SDK module: buildcache's subject is the build-and-store layer, and a
// self-contained `package main` keeps the test hermetic (no module proxy, no network) while still
// exercising a real `go build`.
func testModule(t *testing.T, dir string) string {
	t.Helper()
	src := filepath.Join(dir, "candy", "plugin-test")
	if err := os.MkdirAll(filepath.Join(src, "cmd", "serve"), 0o755); err != nil {
		t.Fatalf("mkdir module: %v", err)
	}
	write(t, filepath.Join(src, "go.mod"), "module example.com/plugin-test\n\ngo 1.26\n")
	write(t, filepath.Join(src, "main.go"), "package main\n\nfunc main() { println(\"v1\") }\n")
	write(t, filepath.Join(src, "cmd", "serve", "main.go"), "package main\n\nfunc main() { println(\"v1\") }\n")
	write(t, filepath.Join(src, "go.sum"), "")
	return src
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// isolate points both roots buildcache touches at temporary directories, so the test writes
// nothing outside its own temp tree (D-STORE-8). The cache root carries the store; the runtime root
// carries the build lock.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("CHARLY_CACHE_DIR", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
}

// buildCountDir installs a PATH shim in front of the real `go` that appends one line per `go build`
// invocation, and returns the log path. This is the test-side form of the instrument the R10 bed
// uses: it measures WORK, so a cache that does nothing cannot pass by exiting zero.
func buildCountDir(t *testing.T) string {
	t.Helper()
	shimDir := t.TempDir()
	logPath := filepath.Join(shimDir, "gobuilds.log")
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("locate go: %v", err)
	}
	script := fmt.Sprintf(`#!/bin/sh
for a in "$@"; do
  if [ "$a" = "build" ]; then
    echo "go build" >> %q
    break
  fi
done
exec %q "$@"
`, logPath, realGo)
	write(t, filepath.Join(shimDir, "go"), script)
	if err := os.Chmod(filepath.Join(shimDir, "go"), 0o755); err != nil {
		t.Fatalf("chmod shim: %v", err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read %s: %v", path, err)
	}
	return len(strings.Split(strings.TrimSpace(string(data)), "\n")) - boolToInt(strings.TrimSpace(string(data)) == "")
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// TestKey_CoversItsInputs is claim 1, and it is the phase's core: D-STORE-4's key is over the
// module source, go.sum, the toolchain and the platform. Each sub-case changes EXACTLY ONE input
// and requires the key to move — the property the deleted path-key lacked, and the reason the
// deleted `.stamp` had to exist.
func TestKey_CoversItsInputs(t *testing.T) {
	dir := t.TempDir()
	src := testModule(t, dir)

	base, err := ComputeKey(src, "plugin-test")
	if err != nil {
		t.Fatalf("ComputeKey: %v", err)
	}
	baseStr := base.String()
	if baseStr == "" {
		t.Fatal("the key rendered as the empty string")
	}
	if base.Toolchain != runtime.Version() {
		t.Fatalf("key toolchain = %q, want the building toolchain %q", base.Toolchain, runtime.Version())
	}

	// A source EDIT must rekey — this is `TestKey_CoversItsInputs`'s most important arm, because a
	// path-keyed cache served a stale binary after an edit and that is the bug class #76 named.
	write(t, filepath.Join(src, "main.go"), "package main\n\nfunc main() { println(\"v2\") }\n")
	edited, err := ComputeKey(src, "plugin-test")
	if err != nil {
		t.Fatalf("ComputeKey after edit: %v", err)
	}
	if edited.String() == baseStr {
		t.Fatal("a source edit did NOT change the key — the key does not cover the module source")
	}

	// A go.sum change must rekey (D-STORE-4 names it explicitly).
	dir2 := t.TempDir()
	src2 := testModule(t, dir2)
	write(t, filepath.Join(src2, "go.sum"), "example.com/dep v1.0.0 h1:deadbeef\n")
	summed, err := ComputeKey(src2, "plugin-test")
	if err != nil {
		t.Fatalf("ComputeKey with go.sum: %v", err)
	}
	plain, err := ComputeKey(testModule(t, t.TempDir()), "plugin-test")
	if err != nil {
		t.Fatalf("ComputeKey plain: %v", err)
	}
	if summed.GoSum == plain.GoSum {
		t.Fatal("a go.sum change did NOT change the key's go_sum component")
	}

	// A build-env change must rekey (the platform axis). Asserted on the composition rather than by
	// rebuilding, because the env is read from the process.
	k := plain
	k.BuildEnv = map[string]string{}
	for name, v := range plain.BuildEnv {
		k.BuildEnv[name] = v
	}
	k.BuildEnv["GOARCH"] = "arm64"
	if k.String() == plain.String() {
		t.Fatal("a GOARCH change did NOT change the key — the platform axis is not covered")
	}

	// key_version is the cache-wide invalidation lever: it participates, so bumping it rekeys
	// everything by construction. Asserted by observing the version string in the composition.
	if !strings.Contains(baseStr, "key_version="+keyVersion) {
		t.Fatalf("the key does not carry key_version=%s, so bumping it would NOT invalidate the cache", keyVersion)
	}
}

// TestBuild_HitPerformsNoBuild is claim 2 — the observable the R10 bed also keys on. A second Build
// of an unchanged plugin must issue NO `go build`, measured through the PATH shim.
func TestBuild_HitPerformsNoBuild(t *testing.T) {
	isolate(t)
	logPath := buildCountDir(t)
	src := testModule(t, t.TempDir())
	req := Request{SrcDir: src, Name: "plugin-test"}

	first, err := Build(context.Background(), req)
	if err != nil {
		t.Fatalf("first Build: %v", err)
	}
	if first.Hit {
		t.Fatal("the first Build reported a hit on an empty store")
	}
	if got := countLines(t, logPath); got != 1 {
		t.Fatalf("the first Build issued %d `go build` calls, want exactly 1", got)
	}

	second, err := Build(context.Background(), req)
	if err != nil {
		t.Fatalf("second Build: %v", err)
	}
	if !second.Hit {
		t.Fatal("the second Build of UNCHANGED source did not hit the cache")
	}
	if got := countLines(t, logPath); got != 1 {
		t.Fatalf("the second Build issued a further `go build` (%d total) — a hit must perform NO build", got)
	}
	if second.Digest != first.Digest {
		t.Fatalf("the hit returned digest %s, want the first build's %s", second.Digest, first.Digest)
	}
	if second.ViewPath != first.ViewPath {
		t.Fatalf("the hit returned view %s, want %s", second.ViewPath, first.ViewPath)
	}
}

// TestBuild_MissOnChangedInput is claim 3: an edit rekeys, so the next Build rebuilds.
func TestBuild_MissOnChangedInput(t *testing.T) {
	isolate(t)
	logPath := buildCountDir(t)
	src := testModule(t, t.TempDir())
	req := Request{SrcDir: src, Name: "plugin-test"}

	if _, err := Build(context.Background(), req); err != nil {
		t.Fatalf("first Build: %v", err)
	}
	before := countLines(t, logPath)

	write(t, filepath.Join(src, "main.go"), "package main\n\nfunc main() { println(\"changed\") }\n")

	res, err := Build(context.Background(), req)
	if err != nil {
		t.Fatalf("Build after edit: %v", err)
	}
	if res.Hit {
		t.Fatal("Build reported a HIT after the source was edited — a changed input must rekey and rebuild")
	}
	if got := countLines(t, logPath); got <= before {
		t.Fatalf("Build after an edit issued no further `go build` (%d total) — the cache served a stale binary", got)
	}
}

// TestBuild_EntryShapeAndSharedView is claim 4: the store holds a keyed entry naming the payload,
// the payload is content-addressed, and the view SHARES the payload's inode — so the bytes exist
// once (the operator's "cached only once" mandate, asserted rather than assumed).
func TestBuild_EntryShapeAndSharedView(t *testing.T) {
	isolate(t)
	src := testModule(t, t.TempDir())
	req := Request{SrcDir: src, Name: "plugin-test"}

	res, err := Build(context.Background(), req)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if res.Key == "" {
		t.Fatal("Build returned no key, so the entry cannot be addressed")
	}

	store, err := storekit.OpenStore(StoreName)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}

	entry, ok, err := store.LookupEntry(res.Key)
	if err != nil {
		t.Fatalf("LookupEntry: %v", err)
	}
	if !ok {
		t.Fatalf("no entry is addressable by the key Build returned (%q)", res.Key)
	}
	if entry.MediaType != MediaTypePluginBinary {
		t.Fatalf("entry media type = %q, want %q", entry.MediaType, MediaTypePluginBinary)
	}
	if entry.Payload != res.Digest {
		t.Fatalf("entry payload = %q, want the built digest %q", entry.Payload, res.Digest)
	}
	// D-STORE-6: the payload is content-addressed and carries no path, and the entry's key is the
	// INPUT identity rather than the payload digest (they are deliberately different facts).
	if entry.Key == entry.Payload {
		t.Fatal("the entry's key equals its payload digest — the key addresses ENTRY identity, the digest addresses BYTES")
	}
	if !strings.HasPrefix(entry.Payload, "sha256:") {
		t.Fatalf("payload %q is not a sha256 content address", entry.Payload)
	}
	if _, err := store.ReadBlob(entry.Payload); err != nil {
		t.Fatalf("the entry's payload is not readable from the store: %v", err)
	}

	// The view IS the payload blob's bytes — the same inode, not a copy.
	shared, err := SharedView(store, res.Digest, res.ViewPath)
	if err != nil {
		t.Fatalf("SharedView: %v", err)
	}
	if !shared {
		t.Fatal("the view does not share the payload blob's inode — the bytes were copied, so they do not exist once")
	}
	// And the view is executable, which is what lets the loader exec it.
	st, err := os.Stat(res.ViewPath)
	if err != nil {
		t.Fatalf("stat view: %v", err)
	}
	if st.Mode().Perm()&0o111 == 0 {
		t.Fatalf("view %s is not executable (mode %v)", res.ViewPath, st.Mode().Perm())
	}
}

// TestBuild_NoBespokeSidecars is claim 5, the R5 half: the format this phase deletes must not come
// back. Asserted positively over the real trees, so a regression is caught rather than assumed away.
func TestBuild_NoBespokeSidecars(t *testing.T) {
	isolate(t)
	src := testModule(t, t.TempDir())

	res, err := Build(context.Background(), Request{SrcDir: src, Name: "plugin-test"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// No `.stamp` beside the view: the key is the freshness answer, so no sidecar digest exists.
	if _, err := os.Stat(res.ViewPath + ".stamp"); err == nil {
		t.Fatalf("a `.stamp` sidecar exists at %s.stamp — the deleted format has been reintroduced", res.ViewPath)
	}
	// No stale build temp left behind either.
	if _, err := os.Stat(res.ViewPath + ".build"); err == nil {
		t.Fatalf("a build temp file was left behind at %s.build", res.ViewPath)
	}

	// A store tree contains NO locks (homes rule). Walk the store and fail on any lock artifact.
	store, err := storekit.OpenStore(StoreName)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	var offenders []string
	if walkErr := filepath.WalkDir(store.Dir(), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if name == "locks" && d.IsDir() {
			offenders = append(offenders, path)
			return filepath.SkipDir
		}
		if strings.HasSuffix(name, ".lock") {
			offenders = append(offenders, path)
		}
		return nil
	}); walkErr != nil {
		t.Fatalf("walk store: %v", walkErr)
	}
	if len(offenders) > 0 {
		t.Fatalf("a store tree contains lock artifacts (%v) — locks belong to the runtime/state root", offenders)
	}
}

// TestBuild_ConcurrentDistinctKeysLoseNoEntries — THE RULE-25 QUESTION: "what happens if two of me run
// at once?"
//
// This is the shape that cost the mechanism a round: `storekit`'s index is a read-modify-write, and an
// earlier fix made one STEP atomic (the file write) while nobody asked whether the OPERATION was safe —
// so two writers for DIFFERENT keys both read the same snapshot, each appended, and the last rename
// silently DROPPED the other's entry: six of twelve lost, a data-loss bug a single-key test cannot see.
//
// buildcache has the same shape one layer up — a key lookup plus a publish — so this test asks the same
// question of THIS layer, with distinct keys deliberately (the case that collides), and asserts the
// inventory afterwards. It depends on `storekit` serializing the index through its runtime-root file
// lock; if that protection ever regresses, this test fails by COUNT rather than by error.
func TestBuild_ConcurrentDistinctKeysLoseNoEntries(t *testing.T) {
	isolate(t)
	src := testModule(t, t.TempDir())

	// Distinct NAMES → distinct view paths and distinct entries, with the SAME module source (so the
	// expensive `go build` result is shared and the test measures the INDEX, not the compiler).
	const n = 8
	names := make([]string, 0, n)
	for i := range n {
		names = append(names, fmt.Sprintf("plugin-concurrent-%d", i))
	}

	var wg sync.WaitGroup
	errs := make([]error, n)
	res := make([]Result, n)
	start := make(chan struct{})
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // release them together, so the writes actually overlap
			res[i], errs[i] = Build(context.Background(), Request{SrcDir: src, Name: name})
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent Build for %s: %v", names[i], err)
		}
	}

	// Every one of the N distinct keys must be addressable AND visible in the inventory. A dropped
	// index append shows up here as a missing key — the data-loss signature, not an error.
	store, err := storekit.OpenStore(StoreName)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	for i, r := range res {
		if r.Key == "" {
			t.Fatalf("%s produced no key", names[i])
		}
		if _, ok, lerr := store.LookupEntry(r.Key); lerr != nil || !ok {
			t.Fatalf("entry for %s is NOT addressable after concurrent writes (ok=%t err=%v) — an index append was lost", names[i], ok, lerr)
		}
		if _, serr := store.ReadBlob(r.Digest); serr != nil {
			t.Fatalf("payload for %s is not readable: %v", names[i], serr)
		}
	}
	entries, err := store.Entries()
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != n {
		t.Fatalf("the store holds %d entries after %d concurrent distinct-key builds — %d were silently LOST (the operation was not serialized)", len(entries), n, n-len(entries))
	}

	// And every published view is independently executable — no cross-talk between concurrent builds.
	for i, r := range res {
		st, serr := os.Stat(r.ViewPath)
		if serr != nil {
			t.Fatalf("view for %s missing: %v", names[i], serr)
		}
		if st.Size() == 0 || st.Mode().Perm()&0o111 == 0 {
			t.Fatalf("view for %s is unusable (size=%d mode=%v)", names[i], st.Size(), st.Mode().Perm())
		}
	}
}

// TestBuild_ConcurrentSameKeySerializes — the OTHER half of the rule-25 question: what happens when two
// of me publish the SAME key at once?
//
// The per-KEY build lock serializes them, so the store never gets two racing publishers: exactly ONE of
// the N concurrent builds compiles, the rest take the lock in turn and observe a HIT. Two properties are
// checked, and the second is the one that matters:
//
//  1. every caller gets a usable view and the same digest (nobody is served a torn or empty state);
//  2. the number of `go build` invocations is EXACTLY ONE — measured through the PATH shim, so a lock
//     that silently stopped serializing shows up as extra builds rather than as an error.
//
// Every INTERMEDIATE state is deliberately a MISS, never a wrong hit: the payload blob is published
// before the view is linked, and the entry after both, so a reader catching the window sees "no view" or
// "no entry" and rebuilds — the conservative direction (a needless rebuild, never a corrupt hit).
func TestBuild_ConcurrentSameKeySerializes(t *testing.T) {
	isolate(t)
	logPath := buildCountDir(t)
	src := testModule(t, t.TempDir())
	// ONE name → ONE key → one view path: the collision case for WRITERS.
	req := Request{SrcDir: src, Name: "plugin-same"}

	const n = 6
	var wg sync.WaitGroup
	errs := make([]error, n)
	res := make([]Result, n)
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			res[i], errs[i] = Build(context.Background(), req)
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent same-key Build %d: %v", i, err)
		}
	}
	// (2) EXACTLY ONE build — the lock serialized them.
	if got := countLines(t, logPath); got != 1 {
		t.Fatalf("%d concurrent same-key builds issued %d `go build` calls, want exactly 1 — the per-key lock is not serializing publishers", n, got)
	}
	// (1) every caller got the same usable binary, not a torn state.
	for i, r := range res {
		if r.Digest != res[0].Digest {
			t.Fatalf("caller %d got digest %s, want the single published %s", i, r.Digest, res[0].Digest)
		}
		if r.Key != res[0].Key {
			t.Fatalf("caller %d got key %s, want %s", i, r.Key, res[0].Key)
		}
		if r.ViewPath != res[0].ViewPath {
			t.Fatalf("caller %d got view %s, want %s", i, r.ViewPath, res[0].ViewPath)
		}
	}
	// Exactly one build means exactly n-1 hits.
	hits := 0
	for _, r := range res {
		if r.Hit {
			hits++
		}
	}
	if hits != n-1 {
		t.Fatalf("%d of %d concurrent same-key builds reported a hit, want exactly %d (one builds, the rest reuse)", hits, n, n-1)
	}
	// The store holds exactly ONE entry for that key.
	store, err := storekit.OpenStore(StoreName)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	entries, err := store.Entries()
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("the store holds %d entries for ONE key — concurrent same-key publishers did not converge", len(entries))
	}
	if _, serr := store.ReadBlob(entries[0].Payload); serr != nil {
		t.Fatalf("the single entry's payload is unreadable: %v", serr)
	}
}

// TestKey_GOWORKIsContentNotPath — the GOWORK axis, split out so each key axis is its own
// test (a single 300-line assertion chain is both unreadable and, as CI found, over the
// complexity gate).
func TestKey_GOWORKIsContentNotPath(t *testing.T) {
	// GOWORK must be a CONTENT identity, never a path: the same workspace content at two different
	// paths must key IDENTICALLY (otherwise "cached once" degrades to "cached once per path"), while
	// different workspace content must key differently (a workspace changes dependency resolution).
	//
	// ⚑ These assertions go through envComponents with an EXPLICIT environment, and they have to:
	// an earlier version of this block called goworkIdentity directly while the claim was about what
	// the KEY records, and a mutation that reverted the wiring to the raw env value did NOT fail it —
	// a false green. The instrument must exercise the same path the claim is about (the composition),
	// not merely the helper the claim names.
	//
	// NOTE for a reader, CORRECTED in round 2: an earlier version of this comment said the running
	// environment carried a local, uncommitted throwaway `go.work` "created only to compile against an
	// unpublished sibling package". That was TRUE while this change was developed — `storekit` lived on
	// a sibling branch and a workspace bridged it — and it is now FALSE and misleading: the bridge is
	// gone, `storekit` is a package of THIS module, and the committed tree compiles with the committed
	// `go.mod` alone. Verified: no `go.work` in the tree, `GOWORK=off go build ./...` → rc=0, and
	// `github.com/opencharly/spec v0.2026287.1529` is the only spec dependency. No workspace is needed
	// to build or test this package, so the note is replaced rather than left implying one is.
	wsA, wsB := t.TempDir(), t.TempDir()
	write(t, filepath.Join(wsA, "go.work"), "go 1.26.4\n\nuse ./a\n")
	write(t, filepath.Join(wsB, "go.work"), "go 1.26.4\n\nuse ./a\n")
	composed := func(gowork string) string {
		return envComponents([]string{"GOWORK=" + gowork})["GOWORK"]
	}
	if a, b := composed(wsA), composed(wsB); a != b {
		t.Fatalf("the SAME workspace content at two paths composed differently (%q vs %q) — the key embeds a path", a, b)
	}
	if strings.Contains(composed(wsA), wsA) {
		t.Fatalf("the composed GOWORK component contains its path (%q) — a machine-specific key", composed(wsA))
	}
	write(t, filepath.Join(wsB, "go.work"), "go 1.26.4\n\nuse ./a\n./different\n")
	if composed(wsA) == composed(wsB) {
		t.Fatal("DIFFERENT workspace content composed identically — a workspace change would serve a stale binary")
	}
	// A directory-valued GOWORK resolves to the go.work inside it.
	if composed(wsA) != composed(filepath.Join(wsA, "go.work")) {
		t.Fatal("a directory-valued GOWORK and its go.work file composed differently — the same workspace keyed twice")
	}
	// "off" and an unset value are the same input, and both must differ from a real workspace.
	if composed("") != "off" || composed("off") != "off" {
		t.Fatal(`an unset/off workspace must compose to the literal "off"`)
	}
	if composed(wsA) == "off" {
		t.Fatal(`a real workspace composed to "off" — its input would be silently dropped`)
	}
	if !strings.HasPrefix(composed(filepath.Join(wsA, "missing-go.work")), "unreadable:") {
		t.Fatal("an unreadable workspace was silently dropped rather than named")
	}
}

// TestKey_GOFLAGSIsMechanical — the GOFLAGS axis: the rule must be a RULE, not a list.
func TestKey_GOFLAGSIsMechanical(t *testing.T) {
	// GOFLAGS is the SAME problem and gets the SAME rule: a path-valued input contributes its CONTENT,
	// never its location — applied mechanically to ANY `-flag=<existing path>`, not to a list of the
	// flags we happen to know. Proved by composition with an EXPLICIT environment, like GOWORK (a
	// direct call to the helper would not exercise the wiring the claim is about — the false green
	// this block's GOWORK sibling was corrected for).
	flagA, flagB := t.TempDir(), t.TempDir()
	write(t, filepath.Join(flagA, "modfile"), "module m\n")
	write(t, filepath.Join(flagB, "modfile"), "module m\n")
	composedFlags := func(goflags string) string {
		return envComponents([]string{"GOFLAGS=" + goflags})["GOFLAGS"]
	}
	fa := composedFlags("-modfile=" + filepath.Join(flagA, "modfile") + " -trimpath")
	fb := composedFlags("-modfile=" + filepath.Join(flagB, "modfile") + " -trimpath")
	if fa != fb {
		t.Fatalf("the SAME GOFLAGS content at two paths composed differently (%q vs %q) — the key embeds a path", fa, fb)
	}
	if strings.Contains(fa, flagA) {
		t.Fatalf("the composed GOFLAGS contains a path (%q) — a machine-specific key", fa)
	}
	if !strings.Contains(fa, "-trimpath") {
		t.Fatalf("a non-path flag was dropped from the identity (%q) — GOFLAGS is an input and must be kept", fa)
	}
	// The rule is mechanical, so a flag nobody listed behaves the same way.
	write(t, filepath.Join(flagA, "overlay.json"), "{}\n")
	if strings.Contains(composedFlags("-overlay="+filepath.Join(flagA, "overlay.json")), flagA) {
		t.Fatal("an unlisted path-valued flag kept its path — the rule is not mechanical")
	}
	// A DIRECTORY-valued flag is the same defect one step out: `go build` accepts flags that point at
	// a directory and change the build (`-pkgdir` holds compiled packages), so a directory left
	// verbatim would re-embed a path THROUGH the rule meant to remove them. A directory contributes a
	// tree digest: the same content at two paths composes identically, and no path is in it.
	pkgA, pkgB := filepath.Join(flagA, "pkg"), filepath.Join(flagB, "pkg")
	write(t, filepath.Join(pkgA, "example.com", "dep", "dep.a"), "compiled-v1")
	write(t, filepath.Join(pkgB, "example.com", "dep", "dep.a"), "compiled-v1")
	da := composedFlags("-pkgdir=" + pkgA)
	db := composedFlags("-pkgdir=" + pkgB)
	if da != db {
		t.Fatalf("the SAME directory content at two paths composed differently (%q vs %q) — a directory path is in the key", da, db)
	}
	if strings.Contains(da, pkgA) {
		t.Fatalf("the composed GOFLAGS contains a directory path (%q)", da)
	}
	if !strings.Contains(da, "tree:") {
		t.Fatalf("a directory value did not compose to a tree identity (%q)", da)
	}
	// Different directory CONTENT must still rekey.
	write(t, filepath.Join(pkgB, "example.com", "dep", "dep.a"), "compiled-v2")
	if composedFlags("-pkgdir="+pkgA) == composedFlags("-pkgdir="+pkgB) {
		t.Fatal("DIFFERENT directory content composed identically — a real change would serve a stale binary")
	}
	// And a directory's internal STRUCTURE is part of its identity, not just its bytes.
	write(t, filepath.Join(pkgA, "extra", "x.a"), "extra")
	if composedFlags("-pkgdir="+pkgA) == da {
		t.Fatal("adding an entry to a directory did not change its tree identity — the structure is not covered")
	}
	// The THIRD axis, and the one the identity's shape can silently leave open: the NAME must not
	// participate. Identical trees under DIFFERENT basenames must compose identically — otherwise a
	// name, which is location and not content, is in the key, and `goworkIdentity` (content-only) and
	// this rule would disagree on whether a non-content name may enter the identity.
	nameX, nameY := filepath.Join(t.TempDir(), "foo"), filepath.Join(t.TempDir(), "bar")
	write(t, filepath.Join(nameX, "example.com", "dep", "dep.a"), "same")
	write(t, filepath.Join(nameY, "example.com", "dep", "dep.a"), "same")
	nx, ny := composedFlags("-pkgdir="+nameX), composedFlags("-pkgdir="+nameY)
	if nx != ny {
		t.Fatalf("identical trees under DIFFERENT basenames composed differently (%q vs %q) — a NAME is in the key, which is location not content", nx, ny)
	}
	if strings.Contains(nx, "foo") || strings.Contains(nx, "bar") {
		t.Fatalf("a basename is in the composed identity (%q) — it must be diagnostics-only", nx)
	}
	// Different CONTENT at the same kind of flag must still rekey.
	write(t, filepath.Join(flagB, "modfile"), "module m\n\nrequire x v1.0.0\n")
	if composedFlags("-modfile="+filepath.Join(flagA, "modfile")) == composedFlags("-modfile="+filepath.Join(flagB, "modfile")) {
		t.Fatal("DIFFERENT modfile content composed identically — a real change would serve a stale binary")
	}
	// A path that does not exist is left verbatim (it cannot differ by location), and an empty value
	// composes empty rather than inventing a component.
	missing := filepath.Join(flagA, "does-not-exist")
	if got := composedFlags("-modfile=" + missing); !strings.Contains(got, missing) {
		t.Fatalf("a non-existent path value was rewritten (%q) — it is not a location-dependent input", got)
	}
	if composedFlags("") != "" {
		t.Fatal("an empty GOFLAGS composed to something — the component must stay empty")
	}
}
