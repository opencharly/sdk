// walk.go — the K1 port of charly's unified-config loader ORCHESTRATION: the kind-blind WALK+PARSE
// half of LoadUnified (charly/unified.go) relocated out of charly core. Walk drives the import
// queue + discover + namespaced-import mounts and PARSES every document (via the existing
// loaderkit.ParseDoc, P6) into a generic spec.LoadedProject envelope — it does NO materialize
// (registry kind-decode) and NO merge (root-wins field folding) into a typed *UnifiedFile; the host
// replays materialize+merge over the returned spec.LoadedProject to reconstruct that, exactly as it
// did inline before.
//
// Everything registry-coupled or host-coupled (the parse pre-scan + connect-declared-kind-plugins
// side effects, the #NodeDoc CUE validate-before-execute gate, remote-ref resolution + the
// project-repo cache, and the repo-identity cycle-break) is a SEAM CALLBACK the host supplies via
// WalkSeams — Walk itself calls the seam and never does the coupled work directly (boundary law
// clause D: Walk consults host-threaded DATA, never the provider registry).
//
// Faithful-port mapping (see charly/unified.go + charly/ns_identity.go):
//
//	LoadUnified (the walk portion only)      → Walk
//	loadUnifiedInto                          → (*walker).walkFile
//	mergeUnifiedDocs (the parse portion)     → (*walker).parseDocs (materialize/mergeUnified excluded)
//	loadNamespaceCached                      → (*walker).walkNamespace
//	the per-project "load + discover" body   → (*walker).walkProject
//	ApplyDiscover/…                          → discover.go
//
// The kind-blind document DIRECTIVES (kit.ImportList/kit.DiscoverConfig/kit.ClassifyDoc/
// kit.FindEntityDirs/…) live in sdk/kit (loader_directives.go/loader_classify.go/
// loader_discover.go), shared with charly core
// (R3) — this package keeps only its own walk-scoped namespace-alias validation (directives.go).
package loaderkit

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
	"gopkg.in/yaml.v3"
)

// WalkSeams is the set of host-supplied callbacks Walk needs for everything registry-coupled or
// host-coupled. Walk calls each seam; it never does the coupled work itself (boundary law clause D
// — Walk consults host-threaded DATA/mechanisms, never the provider registry directly).
type WalkSeams struct {
	// Parser is the per-document parse (the host passes the registered spec.DocParser).
	Parser spec.DocParser
	// Boundary runs at each PROJECT boundary (the root file AND each namespace root — i.e. every
	// depth-0 walkFile) BEFORE that boundary's documents parse: the host does the parse pre-scan +
	// connect-declared-kind-plugins side effects (registry mutation). data = the boundary file bytes.
	Boundary func(dir string, data []byte) error
	// Threaded returns the current registry-derived kind-recognition snapshot. Called fresh per
	// document parse (the host's loaderThreaded()).
	Threaded func() spec.Threaded
	// ResolveRef resolves an import ref (local path OR remote "@host/org/repo[/sub]:ver") to a
	// stable cache KEY + a concrete on-disk PATH. The host owns remote fetch + cache + auto-migration.
	ResolveRef func(ref, baseDir string) (key, path string, err error)
	// GateDoc runs the host #NodeDoc CUE validate-before-execute gate on one raw document's bytes.
	GateDoc func(label string, raw []byte) error
	// RepoIdentity returns the canonical repo identity of an import ref (for the cycle-break), or ""
	// (the host's nsRepoIdentity). Empty → version-keyed fallback.
	RepoIdentity func(ref, baseDir string) string
}

// walker holds the seams for one Walk call. nsCache/loadingRepos are threaded explicitly (not
// walker fields) because they are per-Walk-call state shared across the WHOLE recursive tree,
// mirroring how charly/unified.go threads them as explicit loadUnifiedInto/loadNamespaceCached
// parameters rather than package globals.
type walker struct {
	seams  WalkSeams
	nextID int64 // per-walk stable-id counter for spec.LoadedProject.ID (namespace cycle/diamond dedup)
}

// newID hands out the next stable per-walk project id (starts at 1; 0 is the never-assigned zero
// value, so a RefID can never accidentally alias an unset id).
func (w *walker) newID() int64 {
	w.nextID++
	return w.nextID
}

// nsAcc is the per-project (root OR one mounted namespace) accumulator that survives across the
// recursive flat-import chain within ONE project boundary. Docs and Namespaces are NOT
// accumulated here — they are appended DIRECTLY onto the project's spec.LoadedProject (by
// pointer) as they are discovered. A namespaced cycle/diamond back-reference is NOT an inline
// value copy: walkNamespace/walkFile emit it as a symbolic id REFERENCE mount
// (spec.NamespaceMount.{Ref,RefID} → the target spec.LoadedProject.ID), so the host materialize
// shares the SAME *UnifiedFile by id (register-before-recurse) and preserves the original
// loadNamespaceCached pointer identity across the mutual import (main↔cachyos). discoverSpecs
// is collected throughout the walk but only ACTED on once, at the end (runDiscover runs after the
// whole project's imports are processed, exactly like the original's depth==0 ApplyDiscover call).
// nsByAlias supports the "namespace alias bound to two different refs" duplicate check the original
// expressed via a map (merged.Namespaces); Namespaces here is a slice (the wire shape), so the
// check needs its own companion index.
type nsAcc struct {
	discoverSpecs []kit.ScanSpec
	nsByAlias     map[string]*spec.LoadedProject
}

// Walk walks a project rooted at rootDir and returns its kind-blind parse envelope. rootData is the
// (possibly bootstrap-transformed) bytes of the root charly.yml — used for the root file instead of
// re-reading; pass nil for a namespace child (read from disk). rootIdentity is the root project's
// own repo identity (the host's rootRepoIdentity), seeded into the cycle-break so a transitive
// self-import resolves to the in-progress root — mirrors LoadUnified seeding
// loadingRepos[rootRepoIdentity(dir)] = merged BEFORE the walk starts.
func Walk(rootDir string, rootData []byte, rootIdentity string, seams WalkSeams) (spec.LoadedProject, error) {
	w := &walker{seams: seams}
	nsCache := map[string]*spec.LoadedProject{}
	loadingRepos := map[string]*spec.LoadedProject{}
	lp := &spec.LoadedProject{ID: w.newID()}
	if rootIdentity != "" {
		// Seeded BEFORE the walk and never popped, so it matches anywhere in the import graph —
		// same as LoadUnified's root registration (ns_identity.go).
		loadingRepos[rootIdentity] = lp
	}
	rootPath := filepath.Join(rootDir, kit.UnifiedFileName)
	if err := w.walkProject(lp, rootPath, rootData, nsCache, loadingRepos); err != nil {
		return spec.LoadedProject{}, err
	}
	return *lp, nil
}

// walkProject runs one full project load (the root OR a mounted namespace) into the pre-allocated
// lp: walkFile at depth 0, then discover over whatever ScanSpecs the walk collected. The CALLER
// (Walk, for the root; walkNamespace, for a mounted namespace) owns lp's identity registration in
// loadingRepos — walkProject itself is agnostic to whether this is the once-only permanent root
// seed or a stack-scoped namespace seed. Faithful port of the "seed identity; loadUnifiedInto(root,
// merged, ..., depth 0); ApplyDiscover" sequence LoadUnified/loadNamespaceCached each run.
func (w *walker) walkProject(lp *spec.LoadedProject, path string, dataOverride []byte, nsCache, loadingRepos map[string]*spec.LoadedProject) error {
	acc := &nsAcc{}
	visited := map[string]bool{}
	if err := w.walkFile(lp, path, dataOverride, 0, acc, visited, nsCache, loadingRepos); err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", path, err)
	}
	discovered, err := w.runDiscover(filepath.Dir(abs), acc.discoverSpecs)
	if err != nil {
		return err
	}
	lp.Discovered = discovered
	return nil
}

// walkFile reads one file, parses every one of its documents onto lp (APPENDING directly — see
// nsAcc doc), then processes any `import:` it declared. Flat imports recurse into the SAME lp/acc
// (root-wins doc ORDER: root file's own docs first, then each flat import in declared order, since
// they simply keep appending to the same lp.Docs); namespaced imports mount an isolated child
// spec.LoadedProject onto lp.Namespaces via the shared nsCache/loadingRepos (cycle-broken).
// Cycle-safe within a project via the visited set; across namespaces via nsCache/loadingRepos.
// Faithful port of charly/unified.go's loadUnifiedInto.
func (w *walker) walkFile(lp *spec.LoadedProject, path string, dataOverride []byte, depth int, acc *nsAcc, visited map[string]bool, nsCache, loadingRepos map[string]*spec.LoadedProject) error {
	if depth > MaxIncludeDepth {
		return fmt.Errorf("include depth exceeded %d at %s", MaxIncludeDepth, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", path, err)
	}
	if visited[abs] {
		return fmt.Errorf("include cycle: %s already visited", abs)
	}
	visited[abs] = true

	// dataOverride supplies pre-read bytes for the ROOT file ONLY (depth 0 of the project's own
	// walkProject call) — the F9 bootstrap-transformed root bytes, so a bootstrap plugin's rewrite
	// reaches the actual PARSE. Every other file (every import, every discover manifest, every
	// namespace root) reads from disk. Mirrors LoadUnified's fileOverrides map, which in practice
	// only ever holds the root's own abs path.
	var data []byte
	if depth == 0 && dataOverride != nil {
		data = dataOverride
	} else {
		data, err = os.ReadFile(abs)
		if err != nil {
			return fmt.Errorf("reading %s: %w", abs, err)
		}
	}

	// At a project boundary (depth 0 = the root file OR a namespace root), run the host's
	// pre-parse + connect-declared-kind-plugins side effects BEFORE any document parses — mirrors
	// prescanDeclaredPluginWords + connectDeclaredKindPlugins, both called only at depth 0.
	if depth == 0 {
		if err := w.seams.Boundary(filepath.Dir(abs), data); err != nil {
			return err
		}
	}

	importQueue, err := w.parseDocs(lp, data, abs, filepath.Dir(abs), acc)
	if err != nil {
		return err
	}

	// Process imports relative to this file's directory.
	base := filepath.Dir(abs)
	for _, imp := range importQueue {
		if imp.Namespace == "" {
			// Flat import — merge UNDER the root file (root wins: the root's own docs are already
			// appended to lp.Docs above; each flat import's docs append after). Shares lp/acc/visited.
			_, incPath, err := w.seams.ResolveRef(imp.Ref, base)
			if err != nil {
				return fmt.Errorf("%s: import %q: %w", abs, imp.Ref, err)
			}
			if err := w.walkFile(lp, incPath, nil, depth+1, acc, visited, nsCache, loadingRepos); err != nil {
				return err
			}
			continue
		}
		// Namespaced import — mount an isolated child spec.LoadedProject.
		if err := validateNamespaceAlias(imp.Namespace); err != nil {
			return fmt.Errorf("%s: import %q: %w", abs, imp.Ref, err)
		}
		sub, shared, err := w.walkNamespace(imp.Ref, base, nsCache, loadingRepos)
		if err != nil {
			return fmt.Errorf("%s: import %s (%q): %w", abs, imp.Namespace, imp.Ref, err)
		}
		if existing, ok := acc.nsByAlias[imp.Namespace]; ok {
			if existing != sub {
				return fmt.Errorf("%s: import namespace %q bound to two different refs", abs, imp.Namespace)
			}
			continue
		}
		if acc.nsByAlias == nil {
			acc.nsByAlias = map[string]*spec.LoadedProject{}
		}
		acc.nsByAlias[imp.Namespace] = sub
		// A DEFINITION mount (shared=false) carries the fully-walked child inline (sub.ID lets the
		// host register the *UnifiedFile it materializes). A REFERENCE mount (shared=true — a
		// repo-identity cycle-break or a diamond re-import) carries NO inline project, only the
		// target's id: the host resolves it to the SAME *UnifiedFile it already registered, so the
		// original loader's pointer identity across the mutual-import cycle (main↔cachyos) is
		// preserved — NOT a value-copy snapshot.
		if shared {
			lp.Namespaces = append(lp.Namespaces, &spec.NamespaceMount{Alias: imp.Namespace, Ref: true, RefID: sub.ID})
		} else {
			lp.Namespaces = append(lp.Namespaces, &spec.NamespaceMount{Alias: imp.Namespace, Project: *sub})
		}
	}
	return nil
}

// walkNamespace loads a namespaced import target as a fully-resolved, isolated spec.LoadedProject
// — its OWN files (flat imports for vocabulary, its own entities) plus its OWN namespaced imports.
// A fresh visited set (inside walkProject) isolates its file-cycle detection; the shared nsCache
// breaks cross-namespace cycles (including the intentional main<->cachyos mutual import) by
// recording an in-progress node BEFORE recursing. A whole-repo ref (empty sub-path) resolves to its
// charly.yml. Faithful port of charly/unified.go's loadNamespaceCached.
func (w *walker) walkNamespace(ref, baseDir string, nsCache, loadingRepos map[string]*spec.LoadedProject) (proj *spec.LoadedProject, shared bool, err error) {
	// Cycle-break by REPO IDENTITY (not pinned version), BEFORE any fetch: if this ref targets a
	// repo already being loaded up the stack (the root or an ancestor namespace), resolve to that
	// in-progress node. This terminates the intentional mutual import (main <-> cachyos) even when
	// the loop's pins diverge. shared=true → the caller emits a REFERENCE mount (refID = the target's
	// id), so the host materialize shares the SAME *UnifiedFile (pointer identity preserved).
	repoID := w.seams.RepoIdentity(ref, baseDir)
	if repoID != "" {
		if existing, ok := loadingRepos[repoID]; ok {
			return existing, true, nil
		}
	}
	key, path, rerr := w.seams.ResolveRef(ref, baseDir)
	if rerr != nil {
		return nil, false, rerr
	}
	if existing, ok := nsCache[key]; ok {
		return existing, true, nil // version-keyed diamond memo (dedup identical refs)
	}
	if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
		path = filepath.Join(path, kit.UnifiedFileName)
	}
	lp := &spec.LoadedProject{ID: w.newID()}
	nsCache[key] = lp // version-keyed memo entry (persists across the whole walk)
	if repoID != "" {
		// Stack-scoped in-progress (ancestor) marker for the identity cycle-break above: pushed
		// before recursing, popped after, so two SIBLING imports of the same repo at different
		// versions still each load — only a genuine back-edge (an ancestor still on the stack)
		// short-circuits.
		loadingRepos[repoID] = lp
		defer delete(loadingRepos, repoID)
	}
	// A namespaced import is a SEPARATE project root — it gets no root-bootstrap override (the F9
	// bootstrap transforms only the importing project's own root; dataOverride is nil here).
	if err := w.walkProject(lp, path, nil, nsCache, loadingRepos); err != nil {
		return nil, false, err
	}
	return lp, false, nil
}

// parseDocs parses `data` as a multi-document YAML stream and, for each node-form document,
// appends its parse result DIRECTLY onto lp.Docs (see nsAcc doc) — kit.ClassifyDoc to determine
// each doc's shape (skip kit.DocShapeEmpty), the host's #NodeDoc validate-before-execute gate (GateDoc),
// then the registered spec.DocParser (P6, unchanged whether it is the in-core loaderkit call or,
// later, an out-of-process loader plugin's OpLoad). Collects the concatenated `import:` queue of
// every doc (the caller resolves imports) and anchors + accumulates every doc's `discover:` onto
// acc.discoverSpecs. srcLabel labels diagnostics; srcDir anchors relative discover paths. Faithful
// port of charly/unified.go's mergeUnifiedDocs, MINUS materializeProject/mergeUnified (host
// materialize).
func (w *walker) parseDocs(lp *spec.LoadedProject, data []byte, srcLabel, srcDir string, acc *nsAcc) (kit.ImportList, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	docIdx := 0
	var importQueue kit.ImportList
	for {
		var node yaml.Node
		if err := decoder.Decode(&node); err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, fmt.Errorf("%s:doc%d: %w", srcLabel, docIdx, err)
		}
		shape, err := kit.ClassifyDoc(&node)
		if err != nil {
			return nil, fmt.Errorf("%s:doc%d: %w", srcLabel, docIdx, err)
		}
		switch shape {
		case kit.DocShapeNode:
			label := fmt.Sprintf("%s:doc%d", srcLabel, docIdx)
			// VALIDATE-BEFORE-EXECUTE: the whole node-form document against the host's #NodeDoc
			// gate (strict + closed) BEFORE anything is parsed further.
			raw, err := yaml.Marshal(&node)
			if err != nil {
				return nil, fmt.Errorf("%s: re-marshal node-form doc: %w", label, err)
			}
			if err := w.seams.GateDoc(label, raw); err != nil {
				return nil, err
			}
			// Parse the document into its reserved directives + the generic spec.ParsedProject via
			// the registered config front-end (the host's spec.DocParser). The host threads the
			// registry-derived kind-recognition DATA (Threaded); the parse itself never touches
			// the registry.
			directives, pp, err := w.seams.Parser.ParseDoc(&node, w.seams.Threaded())
			if err != nil {
				return nil, fmt.Errorf("%s: %w", label, err)
			}
			doc := spec.LoadedDoc{Project: pp, SrcDir: srcDir, SrcLabel: label}
			if len(directives) > 0 {
				// The raw reserved-directive mapping (version/repo/defaults/provides/discover;
				// import is consumed by the walk below but also carried here so the host can
				// re-decode it, e.g. for diagnostics) — key order via sorted keys for
				// determinism, mirroring the dirMap the original mergeUnifiedDocs builds.
				keys := make([]string, 0, len(directives))
				for k := range directives {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				dirMap := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				for _, k := range keys {
					dirMap.Content = append(dirMap.Content, kit.ScalarNode(k), directives[k])
				}
				// Serialize the directive mapping as YAML bytes (NOT JSON) so the host replays the
				// ORIGINAL mergeUnifiedDocs decode exactly — yaml.Unmarshal(directives, &sub) honors
				// the custom YAML unmarshalers on import/discover (ImportList / DiscoverConfig), which
				// a JSON body would break. version/repo/defaults/provides ride along unchanged.
				body, merr := yaml.Marshal(dirMap)
				if merr != nil {
					return nil, fmt.Errorf("%s: directives: %w", label, merr)
				}
				doc.Directives = body
				if impNode, ok := directives["import"]; ok {
					var il kit.ImportList
					if derr := impNode.Decode(&il); derr != nil {
						return nil, fmt.Errorf("%s: decoding import: %w", label, derr)
					}
					importQueue = append(importQueue, il...)
				}
				if discNode, ok := directives["discover"]; ok {
					var dc kit.DiscoverConfig
					if derr := discNode.Decode(&dc); derr != nil {
						return nil, fmt.Errorf("%s: decoding discover: %w", label, derr)
					}
					acc.discoverSpecs = append(acc.discoverSpecs, kit.AnchorScanSpecs(dc, srcDir)...)
				}
			}
			lp.Docs = append(lp.Docs, doc)
		case kit.DocShapeEmpty:
			// Skip empty docs (YAML streams commonly end with "---\n").
		}
		docIdx++
	}
	return importQueue, nil
}
