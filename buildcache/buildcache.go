// Package buildcache is the ONE store-backed cache of host-built plugin provider binaries.
//
// WHY THIS PACKAGE EXISTS (R2). Before it, the plugin build cache had TWO live sibling
// implementations of one concept: charly/charly/plugin_loader.go's buildPluginBinary cluster and
// a copy in sdk/deploykit/bake_plugins.go. The sdk copy is LOAD-BEARING, not vestigial — the
// deferred render closure runs OUT-OF-PROCESS in candy/plugin-build, imports only the sdk, and
// therefore cannot call the package-main copy — which is exactly why the unification is
// add-before-delete: this package becomes the one home, BOTH callers repoint at it, and only
// then do the two duplicates go (R5 by sequence, not by a gap).
//
// THE KEY IS D-STORE-4's KEY (the phase's whole claim). D-STORE-4: "A plugin binary's key covers
// its module source, go.sum, the toolchain and the platform." That input set IS the store key,
// which deletes the sidecar dance BY CONSTRUCTION rather than by discipline:
//
//   - Freshness is asked of the STORE ("is this key present?"), never of a sidecar digest written
//     beside the binary. There is no ".stamp" concept in this package at all, and no way for a
//     stamp and its binary to disagree — the key addresses the binary's whole input set.
//   - The key is CONTENT, not a path. The old path key ("<safeName>-<sha256(srcDir)[:8]>") could
//     not answer "is this binary still current" — it is unchanged by a dependency bump — which is
//     exactly why a content-stamp sidecar had to exist. A key over the inputs changes on any
//     input change, so the question the stamp answered is answered by the key itself.
//   - No absolute path and no timestamp reaches a payload (D-STORE-6): roots are hashed by LABEL
//     plus content (never by their path), the key is a digest, and the only time-bearing value in
//     the key is a toolchain version string — an INPUT, not a clock reading.
//
// KEYVERSION — THE INVALIDATION LEVER, and the only sanctioned way to change the key's meaning.
// keyVersion participates in the key, so BUMPING IT INVALIDATES EVERY ENTRY in the store at once
// (each old key is simply absent, and the store's immutable blobs stay reachable by digest until
// reclamation). Add or rename a component and you MUST bump it, or every existing entry silently
// keeps answering a question the new composition no longer asks — entries stranded as
// permanently-valid-but-wrong. Nothing else belongs in the key without that consequence in mind.
//
// WHAT DIES WITH THIS CHANGE (R5, claim-keyed grep in the PR): pluginBuildCacheDir,
// pluginSourceTag, pluginBinaryIsFresh, pluginStampPath, writePluginStamp, pluginBuildStamp,
// pluginSourceRoots, localReplaceDirs, hashDirContent, envValue, all of plugin_build_stamp.go,
// the ".stamp" and ".lock" sidecars, and — after the repoint is proven live — the two duplicate
// clusters.
//
// THE LOCK MOVED OUT OF THE CACHE (the homes rule). A store tree contains NO locks: a lock is
// transient runtime state with no content address, and a digest-keyed lock could even be shared
// between two processes that must not share it. The per-key build lock therefore lives under the
// RUNTIME root, and storekit guards the store side with a test that walks a full store lifecycle
// and fails on any ".lock"/"locks/" artifact. This package must never re-introduce one.
//
// THE VIEW IS A HARDLINK. What the loader execs is a hardlink of the payload blob, never a second
// copy (LinkBlob has deliberately NO cross-device fallback, because a silent copy is how "bytes
// exist once per digest" would stop being true unnoticed). SameFile lets a caller ASSERT the
// sharing, and the view lives under the store tree so the hardlink is same-filesystem by
// construction.
package buildcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/opencharly/sdk/storekit"
)

// Media type of a plugin-build payload. RFC 6838 conformant and opencharly-namespaced, so an
// entry is self-describing without claiming to be a runnable container image.
const MediaTypePluginBinary = "application/vnd.opencharly.plugin.binary.v1"

// StoreName is the named store this cache lives in, under storekit's ONE cache root. It is
// deliberately its own store: the plugin-BUILD cache (host-compiled provider binaries) is a
// different thing from the pulled-plugin artifact store, and fusing them would give two
// lifecycles one name.
const StoreName = "plugin-build"

// keyVersion is the deliberate CACHE-WIDE INVALIDATION LEVER — see the package doc. Bump it when
// the key's composition changes, and only then.
const keyVersion = "charly-plugin-build-key-v1"

// buildEnvKeys are the environment variables that change the produced binary, folded into the key
// so a build under a different platform/libc/CGO setting can never reuse another's output. GOWORK
// is included because the out-of-process build forces it off (an out-of-tree plugin builds
// STANDALONE in its own module; a repo-root go.work lists only the compiled-in plugin candies and
// would reject a non-member candy dir), so a future change to that decision must invalidate.
var buildEnvKeys = []string{
	"GOOS", "GOARCH", "GOARM", "GOAMD64", "CGO_ENABLED", "GOFLAGS", "GOEXPERIMENT", "GOWORK",
}

// Key is D-STORE-4's input set for one plugin binary. Its String() IS the store key.
type Key struct {
	// Plugin is WHICH plugin this binary is for, derived from the candy's LEAF name — never a path.
	//
	// It is in the key because D-STORE-4's inputs identify the BYTES, not the BUILT UNIT: two
	// DIFFERENT plugins with byte-identical source (the same candy content installed under two
	// names, a shared module built per-plugin, a test fixture reused across names) key identically
	// without it, so one would OVERWRITE the other's entry. Measured: eight concurrent
	// distinct-NAME builds sharing one module source produced ONE key and the store kept a single
	// entry — the collision is real, and a same-source test cannot see it because in production two
	// plugins usually differ in source too.
	//
	// The leaf name is used rather than the full candy key (which may be an @github ref with
	// slashes) and rather than the source PATH (which would put a location in the identity): the
	// leaf is the same keying a baked plugin binary's filename uses, so the cache and the baked
	// route agree on what a plugin is called.
	Plugin string
	// ModuleSource digests the candy's own source tree PLUS every module it reaches through a
	// local `replace` directive, transitively. CONTENT, not a path: two worktrees with
	// byte-identical trees key alike, and an uncommitted edit inside a replace target
	// invalidates — which a git-HEAD key would miss, and that is the case that matters most day
	// to day.
	ModuleSource string
	// GoSum digests the module's go.sum. It is inside ModuleSource's traversal too, but D-STORE-4
	// names it explicitly, so it is carried as its own labelled component: a reader auditing the
	// key against the design should not have to infer it from a directory hash.
	GoSum string
	// Toolchain is runtime.Version() of the building toolchain.
	Toolchain string
	// BuildEnv is the platform/build-env axis, one "KEY=VALUE" per buildEnvKeys entry.
	BuildEnv map[string]string
}

// Components returns the key's components in a STABLE, deterministic order, so the canonical key
// string is reproducible and can be audited against the design without recomputing anything.
func (k Key) Components() []string {
	comps := []string{
		"key_version=" + keyVersion,
		"plugin=" + k.Plugin,
		"module_source=" + k.ModuleSource,
		"go_sum=" + k.GoSum,
		"toolchain=" + k.Toolchain,
	}
	for _, name := range buildEnvKeys {
		comps = append(comps, "env_"+strings.ToLower(name)+"="+k.BuildEnv[name])
	}
	return comps
}

// String renders the store key: the canonical, deterministic encoding of the components.
//
// The store treats a key as OPAQUE — canonicalizing a set of build inputs into a key is the
// CALLER's semantic, and if storekit owned it the store would have to know what a build key is,
// which is the boundary we agreed against. So the composition lives here and the store only
// addresses it.
func (k Key) String() string {
	return strings.Join(k.Components(), "\n")
}

// Request is one build-cache operation.
type Request struct {
	// SrcDir is the plugin candy's resolved source directory — its own Go module (go.mod plus a
	// main serving via the plugin SDK).
	SrcDir string
	// Name is the plugin candy key, for diagnostics and the entry's identity. It may be an
	// @github ref with slashes, so it is never used as a path component; the view filename uses
	// its LEAF name, the same keying a baked plugin binary uses.
	Name string
}

// Result is the outcome of Build.
type Result struct {
	// ViewPath is what the caller execs: a hardlink of the payload blob, same inode.
	ViewPath string
	// Digest is the payload blob's content address.
	Digest string
	// Hit reports that the key was already present, so NO `go build` ran. This is the observable
	// a caller — and the R10 bed — keys on: a hit is the ABSENCE of work, never a success exit,
	// because a success exit is also produced by a cache that does nothing at all.
	Hit bool
	// Key is the store key this build was published under ("" only when the key could not be
	// computed, in which case Build degrades to an unconditional rebuild).
	Key string
}

// Build resolves D-STORE-4's key for req, serves the entry from the store on a HIT without
// running `go build`, and on a MISS compiles the provider binary, publishes it as a
// content-addressed payload blob, records the keyed entry, and returns a hardlink view.
//
// A key-computation error is NEVER fatal to the caller's build: it degrades to an unconditional
// rebuild published under no key — correct, just not reusable — mirroring the old stamp's failure
// contract (a stamping error was never fatal either). A cache must never fail a build.
func Build(ctx context.Context, req Request) (Result, error) {
	store, err := storekit.OpenStore(StoreName)
	if err != nil {
		return Result{}, fmt.Errorf("plugin %q: opening the build store: %w", req.Name, err)
	}

	key, kerr := ComputeKey(req.SrcDir, req.Name)
	keyStr := ""
	if kerr == nil {
		keyStr = key.String()
	}

	viewPath, verr := viewPathFor(store.Dir(), req.Name, keyStr)
	if verr != nil {
		return Result{}, verr
	}

	// Serialize concurrent builds of the SAME key, under the RUNTIME root — never inside the
	// store tree (homes rule; storekit guards the store side). Two builders of the same input set
	// serialize here; different input sets never contend.
	release, lockErr := acquireBuildLock(keyStr, req.Name)
	if lockErr != nil {
		return Result{}, fmt.Errorf("plugin %q: acquire build lock: %w", req.Name, lockErr)
	}
	defer func() { _ = release() }()

	// HIT — under the lock, so a concurrent builder of the same key cannot observe a
	// half-published view. No `go build` on this path, and the entry lookup is ONE index read
	// (LookupEntry), not a directory walk.
	if verifyView(viewPath) == nil {
		if entry, ok, lerr := store.LookupEntry(keyStr); lerr == nil && ok && entry.Payload != "" && store.HasBlob(entry.Payload) {
			return Result{ViewPath: viewPath, Digest: entry.Payload, Hit: true, Key: keyStr}, nil
		}
		// The view exists but its entry is missing, unreadable, or names an absent payload: fall
		// through and rebuild, which republishes both. Never silently serve a view the cache
		// surface cannot see or evict — that would hide a store state from `charly cache status`
		// and `charly clean`.
	}

	target, buildDir := buildTarget(req.SrcDir)
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("plugin %q: build dir %s: %w", req.Name, buildDir, err)
	}
	tmp := viewPath + ".build"
	cmd := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-o", tmp, target)
	cmd.Dir = req.SrcDir
	cmd.Env = buildEnvFor(os.Environ())
	if out, berr := cmd.CombinedOutput(); berr != nil {
		_ = os.Remove(tmp) // never leave a half-written temp binary behind
		return Result{}, fmt.Errorf("plugin %q: go build in %s: %w\n%s", req.Name, req.SrcDir, berr, out)
	}

	binary, rerr := os.ReadFile(tmp)
	if rerr != nil {
		_ = os.Remove(tmp)
		return Result{}, fmt.Errorf("plugin %q: read built binary %s: %w", req.Name, tmp, rerr)
	}
	_ = os.Remove(tmp) // the blob is the cache's content; the temp file was only a build product

	payloadDigest, perr := store.PutBlob(binary)
	if perr != nil {
		return Result{}, fmt.Errorf("plugin %q: publish payload blob: %w", req.Name, perr)
	}

	// Publish the view as a HARDLINK of the payload blob, then make it executable. A link failure
	// is a hard error by design (no copy fallback).
	if err := linkView(store, payloadDigest, viewPath); err != nil {
		return Result{}, fmt.Errorf("plugin %q: link view %s: %w", req.Name, viewPath, err)
	}
	// The view is a HARDLINK, so this chmod lands on the shared inode — the mode is applied once
	// to the blob's one instance. Mode is not part of a digest, which is exactly why this is the
	// consumer's concern and not a store call (and why no ChmodBlob exists).
	if err := os.Chmod(viewPath, 0o755); err != nil {
		return Result{}, fmt.Errorf("plugin %q: make view %s executable: %w", req.Name, viewPath, err)
	}

	if kerr == nil {
		// PutEntry writes the manifest blob itself (marshalled entry + index pointer), so this
		// package never hand-rolls one. Annotations are the RESOLUTION RECORD (D-STORE-5) and sit
		// deliberately outside every payload digest — a payload must not carry a timestamp, and
		// the key's provenance belongs beside the key, not inside the bytes it addresses.
		if _, eerr := store.PutEntry(storekit.Entry{
			Key:       keyStr,
			MediaType: MediaTypePluginBinary,
			Payload:   payloadDigest,
			Annotations: map[string]string{
				"ai.opencharly.plugin.name":       filepath.Base(req.SrcDir),
				"ai.opencharly.build.toolchain":   runtime.Version(),
				"ai.opencharly.build.go_sum":      key.GoSum,
				"ai.opencharly.build.key_version": keyVersion,
			},
		}); eerr != nil {
			return Result{}, fmt.Errorf("plugin %q: record entry: %w", req.Name, eerr)
		}
	}

	return Result{ViewPath: viewPath, Digest: payloadDigest, Hit: false, Key: keyStr}, nil
}

// ComputeKey derives D-STORE-4's key from a plugin candy's resolved source directory AND the
// plugin's identity.
//
// The name is REQUIRED, not optional, because D-STORE-4's inputs alone identify the BYTES rather than
// the BUILT UNIT: two different plugins whose source is byte-identical would key alike and one would
// overwrite the other's entry. Measured — eight distinct-NAME builds sharing one module source and one
// key, with the store keeping a single entry.
func ComputeKey(srcDir, name string) (Key, error) {
	roots, err := sourceRoots(srcDir)
	if err != nil {
		return Key{}, err
	}
	h := sha256.New()
	fmt.Fprintf(h, "%s\n", keyVersion)
	for _, root := range roots {
		// The root's own PATH is deliberately NOT hashed — only its LABEL and content. Two
		// worktrees with byte-identical trees key alike, and no absolute path can leak into a
		// payload (D-STORE-6).
		fmt.Fprintf(h, "root:%s\n", filepath.Base(root))
		if herr := hashDirContent(h, root); herr != nil {
			return Key{}, fmt.Errorf("hashing %s: %w", root, herr)
		}
	}
	goSum, err := hashFile(filepath.Join(srcDir, "go.sum"))
	if err != nil {
		return Key{}, err
	}
	return Key{
		Plugin:       SafePluginBinName(filepath.Base(name)),
		ModuleSource: hex.EncodeToString(h.Sum(nil)),
		GoSum:        goSum,
		Toolchain:    runtime.Version(),
		BuildEnv:     envComponents(os.Environ()),
	}, nil
}

// sourceRoots returns srcDir plus every local-path `replace` target reachable from it,
// transitively and deduplicated, in a stable order. A local replace target under development
// carries uncommitted edits, so hashing its CONTENT (not its VCS state) is what catches the
// staleness that matters most day to day.
func sourceRoots(srcDir string) ([]string, error) {
	abs, err := filepath.Abs(srcDir)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var order []string
	var walk func(dir string) error
	walk = func(dir string) error {
		dir = filepath.Clean(dir)
		if seen[dir] {
			return nil
		}
		seen[dir] = true
		order = append(order, dir)
		for _, rep := range localReplaceDirs(dir) {
			if werr := walk(rep); werr != nil {
				return werr
			}
		}
		return nil
	}
	if err := walk(abs); err != nil {
		return nil, err
	}
	sort.Strings(order)
	return order, nil
}

// localReplaceDirs parses dir/go.mod and returns the absolute directories of its LOCAL-path
// replace targets (e.g. `replace example.com/kit => ../vendored/kit`). A version-replace
// (`=> other/mod v1.2.3`) is not a local path and is already pinned by go.sum, which is hashed
// with dir.
//
// A deliberately small hand parser rather than a modfile dependency: the only construct it must
// understand is a replace arrow whose right-hand side starts with "./", "../" or "/", in both the
// single-line and parenthesized-block forms.
func localReplaceDirs(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return nil
	}
	var out []string
	inBlock := false
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		switch {
		case strings.HasPrefix(line, "replace ("):
			inBlock = true
			continue
		case inBlock && line == ")":
			inBlock = false
			continue
		case strings.HasPrefix(line, "replace "):
			line = strings.TrimSpace(strings.TrimPrefix(line, "replace "))
		case !inBlock:
			continue
		}
		_, rhs, ok := strings.Cut(line, "=>")
		if !ok {
			continue
		}
		rhs = strings.TrimSpace(rhs)
		if !strings.HasPrefix(rhs, "./") && !strings.HasPrefix(rhs, "../") && !strings.HasPrefix(rhs, "/") {
			continue
		}
		target := strings.Fields(rhs)[0]
		if !filepath.IsAbs(target) {
			target = filepath.Join(dir, target)
		}
		out = append(out, filepath.Clean(target))
	}
	return out
}

// hashDirContent feeds every file under root into h: its root-relative path, its executable bit,
// and its bytes. Traversal is lexical (filepath.WalkDir), so the digest is order-stable.
//
// .git is skipped — it is huge, it churns on every index refresh, and it says nothing about the
// bytes the compiler reads. A submodule bump still invalidates, because it rewrites the WORKING
// TREE this walk hashes.
func hashDirContent(h io.Writer, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		// A symlink's target bytes are not read here; hash its link text instead, so a retargeted
		// link invalidates.
		if info.Mode()&os.ModeSymlink != 0 {
			dest, lerr := os.Readlink(path)
			if lerr != nil {
				return lerr
			}
			fmt.Fprintf(h, "L %s %s\n", rel, dest)
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, oerr := os.Open(path)
		if oerr != nil {
			return oerr
		}
		defer func() { _ = f.Close() }()
		fh := sha256.New()
		if _, cerr := io.Copy(fh, f); cerr != nil {
			return cerr
		}
		fmt.Fprintf(h, "F %s %o %s\n", rel, info.Mode().Perm()&0o111, hex.EncodeToString(fh.Sum(nil)))
		return nil
	})
}

// hashFile digests one file's bytes; a missing file hashes as empty rather than erroring, so a
// plugin module without a go.sum still keys deterministically.
func hashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// envComponents reads the build-relevant environment into the key's platform axis.
//
// TWO settings are special-cased, for ONE rule: GOWORK and GOFLAGS are both genuinely build inputs
// (dropping either would be an incorrect cache — the worse error), but both routinely carry PATHS, and
// a key embedding a machine-specific path makes the same input at two locations key differently, so
// "cached once" degrades to "cached once per path". Neither is normalized to a path.
func envComponents(env []string) map[string]string {
	out := make(map[string]string, len(buildEnvKeys))
	for _, key := range buildEnvKeys {
		switch key {
		case "GOWORK":
			out[key] = goworkIdentity(envValue(env, key))
		case "GOFLAGS":
			out[key] = goflagsIdentity(envValue(env, key))
		default:
			out[key] = envValue(env, key)
		}
	}
	return out
}

// goflagsIdentity renders GOFLAGS as a CONTENT identity: every flag and value is kept verbatim,
// EXCEPT that a flag value which resolves to an existing FILESYSTEM ENTRY is replaced by that entry's
// content identity.
//
// The rule is "a path-valued input contributes its content, never its location", applied MECHANICALLY
// to any flag — not to the handful of flags we happen to know about today (`-overlay`, `-modfile`,
// `-pkgdir`). A future Go release adding another path-valued flag is therefore covered without an edit
// here, which is the whole point of stating it as a rule rather than a list.
//
// A FILE contributes its content digest; a DIRECTORY contributes a TREE digest (the entryPath/mode/
// content idiom hashDirContent already uses for the module source, with no absolute path in it) —
// because `go build` accepts directory-valued flags that change the build (`-pkgdir` holds compiled
// packages), and a directory left verbatim would re-embed a path through the very rule meant to remove
// them.
//
// The trigger is deliberately conservative: only a `-flag=value` form whose value EXISTS ON DISK is
// rewritten. A value that does not exist is left verbatim — it cannot differ by location, and
// rewriting it would invent a distinction the cache does not need; the failure direction there is a
// false MISS (a needless rebuild), never a wrong hit, which is the conservative side. A value that
// exists but cannot be read is named (`unreadable:<digest>`), never silently dropped.
func goflagsIdentity(value string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return ""
	}
	fields := strings.Fields(v)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		name, val, ok := strings.Cut(f, "=")
		if !ok || !strings.HasPrefix(name, "-") || val == "" {
			out = append(out, f)
			continue
		}
		st, err := os.Stat(val)
		if err != nil {
			out = append(out, f)
			continue
		}
		identity, ierr := pathContentIdentity(val, st.IsDir())
		if ierr != nil {
			sum := sha256.Sum256([]byte(val))
			out = append(out, name+"=unreadable:"+hex.EncodeToString(sum[:8]))
			continue
		}
		out = append(out, name+"="+identity)
	}
	return strings.Join(out, " ")
}

// pathContentIdentity renders an existing filesystem entry as a CONTENT identity with nothing but
// content in it: a file by its content digest, a directory by a TREE digest over its entries (relative
// path, mode and content — the same idiom hashDirContent uses).
//
// NO NAME PARTICIPATES, not even the basename. A directory's name is location, not content: including
// it would make `-pkgdir=/a/foo` and `-pkgdir=/b/bar` with byte-identical trees key differently — a
// false miss — and it would put this normalization out of step with goworkIdentity, which is
// content-only. Keeping the name out also makes the rule the Lead ruled literally true: *content
// identity, never location*. A caller wanting the name for diagnostics can take it from the path it
// already holds; it must never enter the composed key.
//
// It does NOT go through the store to reuse its tree idiom, deliberately: computing a cache KEY must
// have NO SIDE EFFECT, and the store's tree primitive PUBLISHES into the store. A key computation that
// wrote entries nothing asked for would populate the store from a pure lookup — invisible to any test
// that only inspects the returned key.
func pathContentIdentity(path string, isDir bool) (string, error) {
	if !isDir {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		return "content:" + hex.EncodeToString(sum[:16]), nil
	}
	h := sha256.New()
	if err := hashDirContent(h, path); err != nil {
		return "", err
	}
	return "tree:" + hex.EncodeToString(h.Sum(nil)[:16]), nil
}

// goworkIdentity renders a workspace setting as a CONTENT identity, never a path.
//
// GOWORK is a special case in the platform axis. A workspace genuinely changes dependency
// resolution and therefore the build, so the input must stay in the key — dropping it would be an
// incorrect cache, which is the worse error. But its value is normally a PATH ("/path/to/go.work"),
// and a key embedding a machine-specific path is the same class of defect as a path-keyed cache one
// layer up: the same workspace CONTENT at two paths would key differently, so the entry would never
// be shared across venues or hosts and the operator's "cached once" would degrade into "cached once
// per path". So the component records "off", or a digest of the resolved go.work's content: a
// different workspace still yields a different key, and no path enters the identity.
func goworkIdentity(value string) string {
	v := strings.TrimSpace(value)
	if v == "" || v == "off" {
		return "off"
	}
	path := v
	if st, err := os.Stat(v); err == nil && st.IsDir() {
		path = filepath.Join(v, "go.work")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		// Named, not silently dropped: the setting is still an input even when it cannot be read,
		// so an unreadable workspace must key differently from "off".
		sum := sha256.Sum256([]byte(v))
		return "unreadable:" + hex.EncodeToString(sum[:8])
	}
	sum := sha256.Sum256(data)
	return "content:" + hex.EncodeToString(sum[:16])
}

// buildEnvFor is the environment a plugin build runs under. It forces GOWORK=off: an
// out-of-process plugin binary builds STANDALONE in the candy's own module, never in a repo
// workspace, so a repo-root go.work listing only the COMPILED-IN plugin candies cannot reject a
// non-member candy directory. It is also the environment the key's platform axis is read from, so
// the key and the build cannot disagree about which environment produced the bytes.
func buildEnvFor(env []string) []string {
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		if strings.HasPrefix(e, "GOWORK=") {
			continue
		}
		out = append(out, e)
	}
	return append(out, "GOWORK=off")
}

// buildTarget picks the build target: the conventional ./cmd/serve shim when the candy has one
// (the importable provider package sits at the candy root for the in-proc placement; the shim
// wraps it for the out-of-process one), else the candy root for a candy that has not adopted the
// shim layout.
func buildTarget(srcDir string) (target, buildDir string) {
	if st, err := os.Stat(filepath.Join(srcDir, "cmd", "serve")); err == nil && st.IsDir() {
		return "./cmd/serve", srcDir
	}
	return ".", srcDir
}

// SafePluginBinName flattens a candy key (which may be an @github ref with slashes/colons) to a
// single filesystem-safe component. Exported because the deferred render closure
// (sdk/deploykit/bake_plugins.go) needs the same flattening for the baked binary's filename, and
// one implementation is the point of this package.
func SafePluginBinName(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-':
			return r
		}
		return '_'
	}, name)
}

// viewPathFor returns the deterministic view path for a key, under the STORE TREE so the hardlink
// is same-filesystem by construction. The filename uses the plugin's LEAF name — the same keying a
// baked plugin binary uses — because a candy key may be an @github ref with slashes, and a slash
// would imply non-existent nested directories.
func viewPathFor(storeDir, name, key string) (string, error) {
	if storeDir == "" {
		return "", fmt.Errorf("plugin %q: the build store has no directory (an inert store cannot hold a view)", name)
	}
	leaf := SafePluginBinName(filepath.Base(name))
	suffix := "unkeyed"
	if key != "" {
		sum := sha256.Sum256([]byte(key))
		suffix = hex.EncodeToString(sum[:8])
	}
	return filepath.Join(storeDir, "views", leaf+"-"+suffix), nil
}

// verifyView reports whether an existing view is usable. Any doubt answers with an error, so the
// caller rebuilds — the safe direction, since a rebuild is correct and merely slower.
func verifyView(viewPath string) error {
	st, err := os.Stat(viewPath)
	if err != nil {
		return err
	}
	if st.IsDir() || st.Size() == 0 {
		return fmt.Errorf("view %s is not a usable binary (%d bytes, dir=%t)", viewPath, st.Size(), st.IsDir())
	}
	return nil
}

// envValue reads key out of an exec-style environment slice ("K=V"), last-wins like exec does.
func envValue(env []string, key string) string {
	prefix := key + "="
	val := ""
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			val = e[len(prefix):]
		}
	}
	return val
}
