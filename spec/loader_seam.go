package spec

import (
	"encoding/json"

	"gopkg.in/yaml.v3"
)

// loader_seam.go — the hand-written CONTRACT types for the unified-config loader seam (K1/#46).
// These are interface + data contracts (no mechanism): the parse + walk machinery lives in
// sdk/loaderkit, and — since K1 unit 1 — the per-node kind-decode MATERIALIZE dispatch POLICY
// lives there too (Materializer below; the ACTUAL registry resolve + provider dispatch stays
// host-side, clause M, reached through MaterializeSeams exactly like WalkSeams above). They live
// in spec (the shared contract home, alongside the CUE-generated #ParsedProject / #LoadedProject
// wire types) so BOTH the loader plugin (candy/plugin-loader → loaderkit) and the host may
// reference them without either importing the other — charly core imports NEITHER loaderkit NOR
// any other sdk mechanism kit; it reaches the whole-project WALK exclusively through the typed
// ProjectWalker seam below, and the per-node MATERIALIZE exclusively through Materializer, both
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
	// DeployTraits is the substrate word → DECLARED #DeployTraits map (K1-LOADER RELOCATION): the
	// descent stamp reads THIS registry-derived DATA snapshot instead of querying the provider
	// registry live, so the venue-hop descent stamp (loaderkit.StampBundleDescents) is registry-free
	// and can run plugin-side. The host fills it (deployTraitsFor per recognized kind/substrate word)
	// exactly as the former live stampBundleDescents did; a word absent from the map resolves to the
	// external-in-place default, matching deployTraitsFor's nil-for-unrecognized-word semantics.
	DeployTraits map[string]*DeployTraits
	// ExternalDeploySubstrates is the EXACT set of words for which the host's registry-live
	// isExternalDeploySubstrate returns true (K1-LOADER RELOCATION): the host fills it by EVALUATING
	// its own isExternalDeploySubstrate over every recognized substrate word, so a plugin-side
	// validator (loaderkit.ValidateCheckBeds) checks membership here instead of RECONSTRUCTING the
	// host's resourceKindSet ∧ externalizedDeploySubstrates ∧ recognizedDeploySubstrate decision —
	// the byte-exact predicate is threaded, never approximated. A word absent from the set is
	// NOT an external deploy substrate (isExternalDeploySubstrate would return false for it).
	ExternalDeploySubstrates map[string]bool
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

// MaterializedProject accumulates the kind-decoded ENTITY maps ONE document's or ONE discovered
// node's fold produces — the SAME fields charly-core's *UnifiedFile carries for this purpose
// (Box/Candy/Bundle/PluginKinds); the host copies them in before a Materializer call and back out
// after (cheap map-header copies — maps are reference types, so this is NOT a deep copy).
//
// The 5 standalone-substrate-TEMPLATE kinds (vm/pod/k8s/local/android) do NOT get their own
// dedicated fields here — they fold into PluginKinds[disc][name] like every other templated kind
// (distro/builder/init/sidecar/resource/agent already do), so foldStandaloneTemplateReply
// (charly/node_normalize.go) needs NO per-kind-word switch to pick a destination field: the
// generic write `acc.PluginKinds[disc][name] = replyJSON` IS the fold, for any disc. charly-core's
// UnifiedFile.VM()/.Pod()/.K8s()/.Local()/.Android() are now DERIVED accessor methods reading
// PluginKinds, mirroring the established Distros()/Builders()/Inits() pattern — not stored fields
// — so there is nothing left to copy for them either.
//
// SDD classification (hand-written, non-wire — precedent: ParsedProject/LoadedProject/CandyRefs/
// ScannedCandy above, the established sibling family this type extends): same-process PIPELINE
// STATE crossing ONLY the compiled-in typed Materializer seam below — never marshaled, because the
// loader plugin is bootstrap-critical and ALWAYS compiled-in (see the package doc above). A live
// `cue exp gengotypes` spike on this exact shape (all fields already-portable
// map[string]json.RawMessage / map[string]BundleNode / map[string]map[string]json.RawMessage —
// zero disjunctions, zero open tails) would generate a faithful plain struct per the CAN/CANNOT
// quick reference in /charly-internals:go — CUE-sourcing it is NOT precluded by shape. It stays
// hand-written here instead because it is not a wire type at all: it never crosses a real
// marshal/unmarshal boundary (the compiled-in placement passes it as a live *MaterializedProject
// Go pointer, exactly like WalkSeams' live callbacks above) — the SAME reasoning CandyRefs/
// ScannedCandy already document for this file, which the SDD "wire types are CUE-sourced without
// exception" mandate does not reach (it binds host↔plugin / render-context WIRE carriers; this is
// same-process pipeline state, the class loader_seam.go's own precedent already carves out).
type MaterializedProject struct {
	Box         map[string]json.RawMessage
	Candy       map[string]json.RawMessage
	Bundle      map[string]BundleNode
	PluginKinds map[string]map[string]json.RawMessage
}

// MaterializeSeams is the set of host-supplied callbacks the per-node kind-decode DISPATCH needs
// for everything registry-coupled — resolving a node's discriminator word to its live Provider and
// invoking it stays host-side (boundary law clause M: provider_registry.go + provider_kind_invoke.go
// are the TRUE mechanism, same bucket as WalkSeams.Boundary/ResolveRef/GateDoc above); the
// Materializer NEVER touches the provider registry directly.
type MaterializeSeams struct {
	// DecodeEntity resolves pn's discriminator against the provider registry and, if a provider is
	// found, folds the decoded entity into acc (mutating whichever field the kind belongs in) — the
	// SAME dispatch the former in-core normalizeNodeInto/runPluginKind always performed. found=false
	// (no error) means the registry has no provider for pn.Disc — the Materializer applies its OWN
	// not-found policy from there, using Threaded + the callbacks below.
	DecodeEntity func(pn ParsedNode, acc *MaterializedProject) (found bool, err error)
	// BuildBundleEntity folds pn as a deploy-substrate entity into acc.Bundle — the fallback for a
	// RECOGNIZED (Threaded.DeploySubstrates) but not-yet-connected external deploy substrate word.
	BuildBundleEntity func(pn ParsedNode, acc *MaterializedProject) error
	// InKindConnectPass reports whether the loader is inside the re-entrant connect-declared-kind
	// pre-pass (a nested load triggered by connecting a plugin) — a still-unconnected declared kind
	// is silently deferred (skip, no error) during this pass.
	InKindConnectPass func() bool
	// DeclaredKindConnectError returns the retained build/connect failure for a declared-but-
	// unconnected kind word, or nil if it simply hasn't been reached yet.
	DeclaredKindConnectError func(word string) error
}

// Materializer is the swappable per-node kind-decode DISPATCH seam (#46 unit 1, K1): the loader
// plugin candy implements it (candy/plugin-loader, delegating to loaderkit.Materialize), and the
// host resolves the registered loader provider to it and calls it once per parsed entity node — so
// an alternative loader plugin applies a different not-found/fallback policy by implementing this.
// Typed (no wire envelope), mirroring DocParser/ProjectWalker/CandyScanner above.
type Materializer interface {
	MaterializeNode(pn ParsedNode, t Threaded, seams MaterializeSeams, acc *MaterializedProject) error
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

// RefsDownloader is the swappable remote-repo FETCH BACKEND seam (P7): the host dispatches every
// cache-miss download through a RefsDownloader; the DEFAULT (candy/plugin-refs, delegating to
// kit.DownloadRepo) fetches via git, and an alternative refs plugin can serve a different backend
// (OCI/S3-hosted candies) by registering a different RefsDownloader. Mirrors DocParser/ProjectWalker/
// CandyScanner above: a typed interface a compiled-in plugin implements alongside its provider, so
// the host calls it in-proc with no wire envelope. Relocated from sdk/kit (FLOOR-SLIM axis-A
// mechanical batch) — the interface contract itself is kind-blind and registry-decoupled; the
// concrete git-fetch implementation (kit.DefaultDownloader, wrapping kit.DownloadRepo) stays in kit.
type RefsDownloader interface {
	// Download fetches repoPath@version into the local repo cache and returns the cache path.
	// Called only on a cache MISS (the host checks IsRepoCached first).
	Download(repoPath, version string) (string, error)
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
