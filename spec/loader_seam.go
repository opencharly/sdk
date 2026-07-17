package spec

import "gopkg.in/yaml.v3"

// loader_seam.go — the hand-written CONTRACT types for the unified-config loader seam (K1/#46).
// These are interface + data contracts (no mechanism): the parse + walk machinery lives in
// sdk/loaderkit, the materialize in charly core. They live in spec (the shared contract home,
// alongside the CUE-generated #ParsedProject / #LoadedProject wire types) so BOTH the loader
// plugin (candy/plugin-loader → loaderkit) and the host may reference them without either
// importing the other — charly core imports NEITHER loaderkit NOR any other sdk mechanism kit;
// it reaches the whole-project WALK exclusively through the typed ProjectWalker seam below,
// resolved from the registered compiled-in loader plugin (mirroring DocParser/Threaded for PARSE).

// Threaded is the host-computed, registry-derived DATA the per-document parse consults instead of
// querying the provider registry (boundary law clause D): which words are recognized kinds /
// deploy substrates, which external kinds may nest sub-entity members, and each plugin verb's
// scalar-sugar primary field. The host snapshots it before the parse; the parse never touches the
// registry.
type Threaded struct {
	Kinds            map[string]bool   // recognizedKind
	DeploySubstrates map[string]bool   // recognizedDeploySubstrate
	StructuralKinds  map[string]bool   // externalKindMayNestMembers
	Primaries        map[string]string // pluginPrimaryFor: verb word → scalar-sugar primary field
}

// DocParser is the swappable per-document PARSE seam: the loader plugin candy implements it
// (candy/plugin-loader, delegating to loaderkit.ParseDoc), and the host resolves the registered
// loader provider to it and calls it for every config document — so an alternative loader plugin
// serves a different config front-end by implementing this. Typed (no wire envelope) since it runs
// on every document load. `directives` is the reserved-directive mapping (import/discover/version/
// repo/defaults/provides); `pp` is the decomposed entity nodes.
type DocParser interface {
	ParseDoc(doc *yaml.Node, t Threaded) (directives map[string]*yaml.Node, pp ParsedProject, err error)
}

// WalkSeams is the set of host-supplied callbacks the whole-project WALK needs for everything
// registry-coupled or host-coupled — the host builds this value and hands it to the registered
// ProjectWalker; the walk mechanism (sdk/loaderkit.Walk) calls each seam and never does the
// coupled work itself (boundary law clause D: the walk consults host-threaded DATA/mechanisms,
// never the provider registry directly).
type WalkSeams struct {
	// Parser is the per-document parse (the host passes the registered DocParser).
	Parser DocParser
	// Boundary runs at each PROJECT boundary (the root file AND each namespace root) BEFORE that
	// boundary's documents parse: the host does the parse pre-scan + connect-declared-kind-plugins
	// side effects (registry mutation). data = the boundary file bytes.
	Boundary func(dir string, data []byte) error
	// Threaded returns the current registry-derived kind-recognition snapshot. Called fresh per
	// document parse (the host's loaderThreaded()).
	Threaded func() Threaded
	// ResolveRef resolves an import ref (local path OR remote "@host/org/repo[/sub]:ver") to a
	// stable cache KEY + a concrete on-disk PATH. The host owns remote fetch + cache + auto-migration.
	ResolveRef func(ref, baseDir string) (key, path string, err error)
	// GateDoc runs the host #NodeDoc CUE validate-before-execute gate on one raw document's bytes.
	GateDoc func(label string, raw []byte) error
	// RepoIdentity returns the canonical repo identity of an import ref (for the cycle-break), or ""
	// (the host's nsRepoIdentity). Empty → version-keyed fallback.
	RepoIdentity func(ref, baseDir string) string
}

// ProjectWalker is the swappable WHOLE-PROJECT WALK seam: the loader plugin candy implements it
// (candy/plugin-loader, delegating to loaderkit.Walk), and the host resolves the registered loader
// provider to it and calls it once per project load — so an alternative loader plugin serves a
// different walk mechanism by implementing this. Typed (no wire envelope) since the compiled-in
// placement passes live Go callbacks (WalkSeams) that cannot cross a JSON envelope. rootData is the
// (possibly bootstrap-transformed) root document bytes; rootIdentity seeds the namespace
// cycle-break with the root project's own repo identity.
type ProjectWalker interface {
	WalkProject(rootDir string, rootData []byte, rootIdentity string, seams WalkSeams) (LoadedProject, error)
}

// CandyScanner is the swappable CANDY-SCAN seam (W9): the loader plugin candy implements it
// (candy/plugin-loader, delegating to loaderkit.ScanCandyManifest), and the host resolves the
// registered loader provider to it and calls it once per candy directory. Typed (no wire envelope)
// — the compiled-in placement passes a live parseManifest callback (a Go function value, exactly
// like WalkSeams.Parser above) since the candy-manifest parse itself is registry-coupled (it
// threads the registered DocParser + the registry-derived Threaded snapshot) and so stays a
// HOST-injected seam rather than moving into loaderkit — only the SCAN+CONSTRUCT logic (fs-probes,
// the bake_plugin/package-derivation/port-normalization business logic) moves. Returns the two
// resolved envelope views (spec.CandyModel + spec.CandyView) DIRECTLY — the same shape
// sdk/deploykit.NewSpecCandyModel already consumes to build a spec.CandyReader, so core never
// needs a concrete Candy struct to hold the scan result.
//
// ScanCandyManifest is named distinctly from the ESTABLISHED exported charly.ScanCandy(dir) (the
// whole-project scan-all-candies entry point, charly/layers.go) — a similar name on a
// single-candy-directory scan risks confusion once both exist side by side during the cutover.
type CandyScanner interface {
	ScanCandyManifest(path, name, manifestName string, parseManifest func(path string) (*Candy, error)) (CandyModel, CandyView, CandyRefs, error)
	// ScanInlineCandy builds the two views for a candy declared INLINE in a unified charly.yml —
	// ly is already the parsed body (no manifest file, no parseManifest seam needed). sourceDir is
	// the charly.yml's own directory.
	ScanInlineCandy(name, sourceDir string, ly *Candy) (CandyModel, CandyView, CandyRefs)
	// ScanRemoteCandy scans specific candies out of a downloaded remote repository directory —
	// only the bare refs in wantRefs (each "github.com/org/repo/candy/x" form). Sets each result's
	// CandyView.Remote/.RepoPath/.SubPathPrefix and runs the remote-sibling-dep qualification
	// (QualifyRemoteSiblingDeps) before returning, mirroring the pre-move charly/layers.go
	// ScanRemoteCandy, which did the same two things (post-scan.Remote/RepoPath/SubPathPrefix
	// mutation, then qualifyRemoteSiblingDeps) on the live *Candy it had just built.
	ScanRemoteCandy(repoDir, repoPath string, wantRefs map[string]bool, parseManifest func(path string) (*Candy, error)) (map[string]ScannedCandy, error)
}

// CandyRefs carries the RICH require:/candy:/bake_plugin: refs (CandyRefEntry, with a mutable
// .Resolved) a freshly scanned candy declares.
//
// SDD classification (hand-written, non-wire — precedent: ParsedProject/LoadedProject above, the
// original hand-written-contract types this seam file establishes): CandyRefs is same-process
// PIPELINE STATE crossing ONLY the compiled-in typed CandyScanner seam — it is never marshaled,
// because the loader plugin is bootstrap-critical and ALWAYS compiled-in (see the package doc
// above: "the loader must ALWAYS resolve... registered at init() before the first load"), so this
// seam never crosses a real wire the way an out-of-process plugin's gRPC envelope would. It exists
// only between ScanCandyManifest and the host's qualifyRemoteSiblingDeps (which sets .Resolved on a
// remote candy's plain-name sibling deps) and the FINAL bare-string conversion into
// CandyView.Require/.IncludedCandy (mirrors the pre-move projectCandyView's bareRefs() call, which
// ran AFTER qualification on the live *Candy — this type is what lets that same ordering survive
// the *Candy struct's departure). The FINAL bare-string form lands on CandyView.Require/
// .IncludedCandy and CandyModel.BakePlugin (FinalizeCandyRefs, sdk/loaderkit).
type CandyRefs struct {
	Require       []CandyRefEntry
	IncludedCandy []CandyRefEntry
	BakePlugin    []CandyRefEntry
}

// ScannedCandy bundles one candy's full scan result — the two resolved envelope views plus the
// rich pre-qualification refs.
//
// SDD classification: same non-wire, same-process pipeline-state rationale as CandyRefs above (one
// note covers both) — it is the mutable intermediate the whole scan→fetch→qualify→arbitrate
// pipeline (charly's ScanAllCandy family) carries in place of the pre-move *Candy, until the FINAL
// step bare-strings the refs (FinalizeCandyRefs) and wraps (Model, View) into a spec.CandyReader
// via sdk/deploykit.NewSpecCandyModel.
type ScannedCandy struct {
	Model CandyModel
	View  CandyView
	Refs  CandyRefs
}
