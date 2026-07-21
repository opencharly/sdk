package buildkit

import (
	"fmt"
	"maps"
	"strings"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
)

// config_resolve.go — the buildkit-typed half of Config's box-resolution logic (FLOOR-SLIM Unit
// 5, relocated from charly/config.go + charly/namespace.go). Every function here is a FREE
// FUNCTION taking *spec.Config as its first parameter — Go forbids a package outside a type's own
// package from adding methods to it, and these resolvers' signatures unavoidably touch buildkit's
// own types (*ResolvedBox, BuilderMap, *DistroConfig, *BuilderConfig), so they cannot be methods
// on spec.Config (spec is the bottom of the sdk dependency graph and must never import buildkit).
//
// Charly's own config.go keeps THIN WRAPPER free functions named identically (ResolveBox /
// ResolveAllBox) that fill the ONE LoadUnified-coupled fallback (loading the project's
// distro:/builder: vocabulary when the caller didn't supply it) before delegating here — the
// SAME "~35 STAY: LoadConfig/LoadConfigRaw + 2 fallback branches" the original scoping map
// identified; this file's ResolveBox/ResolveAllBox REQUIRE opts.DistroCfg/BuilderCfg to already
// be populated (no fallback, no LoadUnified anywhere in buildkit).

// ResolveOpts carries the box-resolution-relevant subset of charly's own (wider) ResolveOpts —
// projected in by charly's thin wrapper. IncludeDisabled/IncludeDisabledNames/RequestedBoxes are
// pure value/set fields; DistroCfg/BuilderCfg are the project's build vocabulary the caller must
// supply (charly's wrapper fills them via LoadBuildConfigForBox when absent). Charly-only fields
// that no function in this file reads (ExtraCandyRefs, InitCfg — consumed solely by
// charly/layers.go's ScanAllCandyWithConfigOpts) deliberately stay OUT of this type; charly's own
// ResolveOpts carries them alongside this projected subset.
type ResolveOpts struct {
	IncludeDisabled      bool            // skip the `enabled: false` check
	IncludeDisabledNames map[string]bool // when non-empty, scope IncludeDisabled to these names only
	// RequestedBoxes are the explicit build targets (`charly box build <name>`). A qualified name
	// here (e.g. `charly.arch-builder`) is pulled into the resolved set even when it isn't
	// reachable as a base/builder of a root image.
	RequestedBoxes []string
	DistroCfg      *DistroConfig
	BuilderCfg     *BuilderConfig
}

// ShouldIncludeDisabled reports whether name's disabled gate should be bypassed under opts.
func (opts ResolveOpts) ShouldIncludeDisabled(name string) bool {
	if !opts.IncludeDisabled {
		return false
	}
	if len(opts.IncludeDisabledNames) == 0 {
		return true
	}
	return opts.IncludeDisabledNames[name]
}

// ResolveBox resolves a single box's configuration by applying defaults.
func ResolveBox(cfg *spec.Config, name string, calverTag string, dir string, opts ResolveOpts) (*ResolvedBox, error) {
	// Namespace-aware entry: a qualified name (e.g. `charly.arch-builder`, `cachyos.cachyos`)
	// resolves inside the Config of the namespace that owns it, where its base:/builder: refs are
	// relative. This mirrors ResolveBoxRef's descent so that EVERY ResolveBox caller is
	// namespace-aware through this single chokepoint instead of each re-implementing (or
	// omitting) the descent.
	if ns, rest, ok := spec.SplitNamespaceRef(name); ok {
		sub, found := cfg.Namespaces[ns]
		if !found {
			return nil, fmt.Errorf("import namespace %q not found (resolving image %q)", ns, name)
		}
		return ResolveBox(sub, rest, calverTag, dir, opts)
	}
	img, ok := cfg.BoxConfig(name)
	if !ok {
		return nil, fmt.Errorf("box %q not found in charly.yml", name)
	}
	if !img.IsEnabled() && !opts.ShouldIncludeDisabled(name) {
		return nil, fmt.Errorf("image %q is disabled (pass --include-disabled to operate on it without flipping authored config)", name)
	}

	resolved := &ResolvedBox{
		Name:    name,
		Version: img.Version,
		// boxes author no status; the effective rung (worst-of-candy-chain) is computed at
		// generate time for the ai.opencharly.status label. resolveStatus("") always returned
		// "testing" (charly's generate.go) — inlined directly since resolveStatus itself stays
		// charly-core-only (used by other status collection paths this move doesn't touch).
		Status:     "testing",
		Info:       firstDescriptionLine(img.Description),
		CheckLevel: kit.ResolveCheckLevel(img.CheckLevel),
	}

	if err := resolveBase(cfg, resolved, img, name); err != nil {
		return nil, err
	}

	resolvePlatforms(cfg, resolved, img)

	resolveTag(cfg, resolved, img, calverTag)

	// Resolve registry: image -> defaults -> ""
	resolved.Registry = img.Registry
	if resolved.Registry == "" {
		resolved.Registry = cfg.Defaults.Registry
	}

	resolveDistro(cfg, resolved, img)

	if err := resolveBuild(cfg, resolved, img, name); err != nil {
		return nil, err
	}

	// Build unified Tags for task matching: ["all"] + Distro + BuildFormats
	resolved.Tags = append([]string{"all"}, resolved.Distro...)
	resolved.Tags = append(resolved.Tags, resolved.BuildFormats...)

	// Candies are not inherited, they're image-specific.
	// Strip @ prefix and :version suffixes — candy map keys use bare refs.
	resolved.Candy = make([]string, len(img.Candy))
	for i, ref := range img.Candy {
		resolved.Candy[i] = spec.BareCandyRef(ref)
	}

	// Ports are NOT resolved here — they are inherited from the candy chain at OCI-label emission
	// + inspect time, since ResolveBox has no candy map. Boxes no longer declare ports.

	// Resolve user: image -> defaults -> "user"
	resolved.User = img.User
	if resolved.User == "" {
		resolved.User = cfg.Defaults.User
	}
	if resolved.User == "" {
		resolved.User = "user"
	}

	// Resolve UID: image -> defaults -> 1000
	resolved.UID = resolveIntPtr(img.UID, cfg.Defaults.UID, 1000)

	// Resolve GID: image -> defaults -> 1000
	resolved.GID = resolveIntPtr(img.GID, cfg.Defaults.GID, 1000)

	// Resolve merge config: image -> defaults -> nil
	if img.Merge != nil {
		resolved.Merge = img.Merge
	} else if cfg.Defaults.Merge != nil {
		resolved.Merge = cfg.Defaults.Merge
	}

	// Builder resolution flows through the ONE canonical function so it can't diverge across
	// commands (build/generate/inspect via ResolveBox, `charly bundle add`'s synthetic host/VM
	// image, and the remote-ref fetch walk via EffectiveBuilderForBox all call
	// ResolveEffectiveBuilder).
	resolved.Builder = ResolveEffectiveBuilder(cfg, name, resolved.Distro, resolved.Base, resolved.IsExternalBase, img.Builder)

	// BuilderCapabilities: image-specific capability declaration, NOT inherited.
	resolved.BuilderCapabilities = img.Produce

	// Schema v4: DNS / AcmeEmail / Tunnel / Engine no longer resolve from image config — they are
	// deployment choices and flow through MergeDeployOntoMetadata → BoxMetadata directly.

	// VM configuration lives on `kind: vm` entities in vm.yml, NOT on box: entries.

	// Resolve network: image -> defaults -> ""
	resolved.Network = img.Network
	if resolved.Network == "" {
		resolved.Network = cfg.Defaults.Network
	}

	// Data image flag (not inherited from defaults).
	resolved.DataImage = img.DataImage

	// Home directory will be resolved later (after inspecting base image).
	if resolved.User == "root" {
		resolved.Home = "/root"
	} else {
		resolved.Home = fmt.Sprintf("/home/%s", resolved.User)
	}

	// Compute full tag.
	if resolved.Registry != "" {
		resolved.FullTag = fmt.Sprintf("%s/%s:%s", resolved.Registry, name, resolved.Tag)
	} else {
		resolved.FullTag = fmt.Sprintf("%s:%s", name, resolved.Tag)
	}

	resolved.DistroConfig = opts.DistroCfg
	resolved.BuilderConfig = opts.BuilderCfg
	if opts.DistroCfg != nil {
		// Expand the package-cascade chain with any inherit_packages: ancestor (cachyos → [cachyos,
		// arch]) so an `arch:` candy block reaches cachyos. Idempotent when the box already
		// authored the ancestor explicitly.
		resolved.Distro = opts.DistroCfg.ExpandPackageInheritance(resolved.Distro)
		resolved.DistroDef = opts.DistroCfg.ResolveDistro(resolved.Distro)
	}

	// Reconcile user_policy against the distro's base_user declaration. Must run after DistroDef
	// is resolved. Updates resolved.User/UID/GID/Home when adopting so all downstream
	// substitution (${USER}, ${HOME}, COPY --chown, sudoers writes) sees the adopted identity.
	policy := img.UserPolicy
	if policy == "" {
		policy = cfg.Defaults.UserPolicy
	}
	if policy == "" {
		policy = "auto"
	}
	var baseUser *spec.BaseUser
	if resolved.DistroDef != nil {
		baseUser = resolved.DistroDef.BaseUser
	}
	userExplicitlySet := img.User != "" || cfg.Defaults.User != ""
	switch policy {
	case "adopt":
		if baseUser == nil {
			return nil, fmt.Errorf("image %s: user_policy: adopt requires distro %v to declare base_user in the embedded vocabulary (charly/charly.yml)", name, resolved.Distro)
		}
		resolved.User = baseUser.Name
		resolved.UID = baseUser.UID
		resolved.GID = baseUser.GID
		resolved.Home = baseUser.Home
		resolved.UserAdopted = true
	case "auto":
		if baseUser != nil && !userExplicitlySet {
			resolved.User = baseUser.Name
			resolved.UID = baseUser.UID
			resolved.GID = baseUser.GID
			resolved.Home = baseUser.Home
			resolved.UserAdopted = true
		}
	case "create":
		// no-op — resolved.User/UID/GID/Home already reflect image config + defaults + hardcoded
		// fallback, and the bootstrap preamble (deploykit.Generator.WriteBootstrap) will useradd.
	default:
		return nil, fmt.Errorf("image %s: unknown user_policy %q (expected auto, adopt, or create)", name, policy)
	}

	return resolved, nil
}

// firstDescriptionLine returns d's first line (trimmed), or "" — byte-identical to
// deploykit.DescriptionInfo. Duplicated here (rather than imported) because deploykit imports
// buildkit, so the reverse import is a cycle; both copies are pure, tiny, and kept in lockstep.
func firstDescriptionLine(d string) string {
	d = strings.TrimSpace(d)
	if d == "" {
		return ""
	}
	if before, _, ok := strings.Cut(d, "\n"); ok {
		return strings.TrimSpace(before)
	}
	return d
}

// resolveBase resolves a box's base image (from-builder, data-image, or base:/defaults chain) and
// sets IsExternalBase.
func resolveBase(cfg *spec.Config, resolved *ResolvedBox, img spec.BoxConfig, name string) error {
	switch {
	case img.From != "":
		if img.HasBaseFromConflict() {
			return fmt.Errorf("image %s: from: and base: are mutually exclusive", name)
		}
		resolved.From = img.From
		resolved.BootstrapBuilderImage = img.BootstrapBuilderImage
		resolved.Base = "scratch"
		resolved.IsExternalBase = true
	case img.DataImage:
		resolved.Base = "scratch"
		resolved.IsExternalBase = true
	default:
		// Resolve base: image -> defaults -> "quay.io/fedora/fedora:43"
		resolved.Base = img.Base
		if resolved.Base == "" {
			resolved.Base = cfg.Defaults.Base
		}
		if resolved.Base == "" {
			resolved.Base = "quay.io/fedora/fedora:43"
		}

		// Check if base is internal (another enabled image — local OR resolved through an import
		// namespace, e.g. `cachyos.cachyos`) or external.
		if baseImg, _, isInternal := cfg.ResolveBoxRef(resolved.Base); isInternal && baseImg.IsEnabled() {
			resolved.IsExternalBase = false
		} else {
			resolved.IsExternalBase = true
		}
	}
	return nil
}

// resolvePlatforms resolves a box's target platforms (image -> defaults -> linux/amd64+arm64).
func resolvePlatforms(cfg *spec.Config, resolved *ResolvedBox, img spec.BoxConfig) {
	resolved.Platforms = img.Platforms
	if len(resolved.Platforms) == 0 {
		resolved.Platforms = cfg.Defaults.Platforms
	}
	if len(resolved.Platforms) == 0 {
		resolved.Platforms = []string{"linux/amd64", "linux/arm64"}
	}
}

// resolveTag resolves a box's tag (image -> defaults -> "auto"), substituting the computed calver
// when "auto".
func resolveTag(cfg *spec.Config, resolved *ResolvedBox, img spec.BoxConfig, calverTag string) {
	resolved.Tag = img.Tag
	if resolved.Tag == "" {
		resolved.Tag = cfg.Defaults.Tag
	}
	if resolved.Tag == "" {
		resolved.Tag = "auto"
	}
	// If tag is "auto", use the computed calver.
	if resolved.Tag == "auto" {
		resolved.Tag = calverTag
	}
}

// resolveDistro resolves a box's distro tags (image -> base-chain walk -> defaults).
func resolveDistro(cfg *spec.Config, resolved *ResolvedBox, img spec.BoxConfig) {
	resolved.Distro = img.Distro
	if len(resolved.Distro) == 0 {
		resolved.Distro = cfg.WalkBaseChainDistro(resolved.Base)
	}
	if len(resolved.Distro) == 0 {
		resolved.Distro = cfg.Defaults.Distro
	}
}

// resolveBuild resolves a box's build formats (image -> base-chain walk -> defaults; required
// unless a data image) and the primary cache-mount format.
func resolveBuild(cfg *spec.Config, resolved *ResolvedBox, img spec.BoxConfig, name string) error {
	buildFmts := img.Build
	if len(buildFmts) == 0 {
		buildFmts = cfg.WalkBaseChainBuild(resolved.Base)
	}
	if len(buildFmts) == 0 {
		buildFmts = cfg.Defaults.Build
	}
	if len(buildFmts) == 0 && !img.DataImage {
		return fmt.Errorf("image %s: build: field required (set in image, base, or defaults)", name)
	}
	resolved.BuildFormats = buildFmts
	if len(buildFmts) > 0 {
		resolved.Pkg = buildFmts[0] // primary format for cache mounts
	}
	return nil
}

// ResolveAllBox resolves all enabled images in the config. opts.IncludeDisabled extends the
// working set to images marked enabled: false (the build verb's `--include-disabled` flag flips
// this for one-off operational rebuilds without modifying authored config). REQUIRES
// opts.DistroCfg/BuilderCfg to be pre-populated (charly's ResolveAllBox wrapper fills them).
func ResolveAllBox(cfg *spec.Config, calverTag string, dir string, opts ResolveOpts) (map[string]*ResolvedBox, error) {
	resolved := make(map[string]*ResolvedBox)
	for _, name := range cfg.AllBoxNames() {
		img, _ := cfg.BoxConfig(name)
		if !img.IsEnabled() && !opts.ShouldIncludeDisabled(name) {
			continue
		}
		ri, err := ResolveBox(cfg, name, calverTag, dir, opts)
		if err != nil {
			return nil, err
		}
		resolved[name] = ri
	}
	// Pull in any explicitly-requested namespace-qualified targets BEFORE base resolution.
	// resolveNamespacedBases is reachability-scoped (it only follows bases/builders of root
	// images); an on-demand target like `charly box build charly.arch-builder` must be pulled
	// explicitly so it lands in the resolved map under its fully-qualified key.
	for _, name := range opts.RequestedBoxes {
		if _, _, qualified := spec.SplitNamespaceRef(name); !qualified {
			continue
		}
		if _, done := resolved[name]; done {
			continue
		}
		if err := pullNamespacedBox(cfg, name, "", calverTag, dir, opts, resolved); err != nil {
			return nil, err
		}
	}
	if err := resolveNamespacedBases(cfg, resolved, calverTag, dir, opts); err != nil {
		return nil, err
	}
	return resolved, nil
}

// resolveIntPtr resolves a *int value through fallback chain: value -> fallback -> defaultVal.
func resolveIntPtr(value, fallback *int, defaultVal int) int {
	if value != nil {
		return *value
	}
	if fallback != nil {
		return *fallback
	}
	return defaultVal
}

// ResolveEffectiveBuilder computes an image's effective builder map via the SINGLE canonical
// precedence, lowest→highest: defaults.builder → distro-keyed default → direct local base →
// per-image override — then self-references are filtered. EVERY builder-consuming path calls
// this so resolution can never drift between commands.
func ResolveEffectiveBuilder(cfg *spec.Config, name string, distro []string, base string, isExternalBase bool, imgBuilder BuilderMap) BuilderMap {
	out := make(BuilderMap)
	maps.Copy(out, cfg.Defaults.Builder)
	maps.Copy(out, distroBuilderMap(cfg, distro))
	if !isExternalBase {
		// DELIBERATELY flat (not ResolveBoxRef): a base's builder map is only inherited when the
		// base is ROOT-local. A namespace-qualified base intentionally does NOT contribute its
		// builder map here.
		if baseImg, ok := cfg.BoxConfig(base); ok {
			maps.Copy(out, baseImg.Builder)
		}
	}
	maps.Copy(out, imgBuilder)
	for typ, b := range out {
		if b == name {
			delete(out, typ)
		}
	}
	return out
}

// EffectiveBuilderForBox computes the builder image refs an image will build against, from a RAW
// BoxConfig — the FETCH-path counterpart to ResolveBox's resolved-value path. Both end at the ONE
// canonical ResolveEffectiveBuilder.
func EffectiveBuilderForBox(cfg *spec.Config, name string, img spec.BoxConfig) BuilderMap {
	base := "scratch"
	isExternalBase := true
	if img.From == "" && !img.DataImage {
		base = img.Base
		if base == "" {
			base = cfg.Defaults.Base
		}
		if base == "" {
			base = "quay.io/fedora/fedora:43"
		}
		if baseImg, _, isInternal := cfg.ResolveBoxRef(base); isInternal && baseImg.IsEnabled() {
			isExternalBase = false
		}
	}
	distro := img.Distro
	if len(distro) == 0 {
		distro = cfg.WalkBaseChainDistro(base)
	}
	if len(distro) == 0 {
		distro = cfg.Defaults.Distro
	}
	return ResolveEffectiveBuilder(cfg, name, distro, base, isExternalBase, img.Builder)
}

// distroBuilderMap returns the builder map of the root-namespace image that owns the given
// distro — the distro-keyed builder default. distroTags is the image's resolved distro in
// priority order; the first tag with a matching root image wins.
func distroBuilderMap(cfg *spec.Config, distroTags []string) BuilderMap {
	names := cfg.AllBoxNames()
	candidates := make([]DistroBuilderCandidate, 0, len(names))
	for _, name := range names {
		img, _ := cfg.BoxConfig(name)
		candidates = append(candidates, DistroBuilderCandidate{Name: name, Distro: img.Distro, Builder: img.Builder})
	}
	return PickDistroBuilder(candidates, distroTags)
}

// resolveNamespacedBases pulls every namespace-qualified base referenced by the already-resolved
// local image set into `out` (keyed by the fully-qualified name), resolving each within its own
// namespace context. Iterates to a fixpoint because a pulled-in image may itself reference a
// (deeper) namespaced base.
func resolveNamespacedBases(cfg *spec.Config, out map[string]*ResolvedBox, calverTag, dir string, opts ResolveOpts) error {
	for {
		var todo []string
		add := func(ref string) {
			if _, ok := out[ref]; ok {
				return
			}
			if _, _, qualified := spec.SplitNamespaceRef(ref); qualified {
				todo = append(todo, ref)
			}
		}
		for _, ri := range out {
			if !ri.IsExternalBase {
				add(ri.Base)
			}
			// Qualified builder refs are pulled in so the generator can resolve the builder
			// stage's FROM — but ONLY for images that actually have candies to build.
			if len(ri.Candy) > 0 {
				for _, b := range ri.Builder.AllBuilder() {
					add(b)
				}
			}
		}
		if len(todo) == 0 {
			return nil
		}
		for _, qref := range todo {
			if _, ok := out[qref]; ok {
				continue
			}
			if err := pullNamespacedBox(cfg, qref, "", calverTag, dir, opts, out); err != nil {
				return err
			}
		}
	}
}

// pullNamespacedBox resolves `ref` (possibly qualified, relative to base Config `from`) and
// stores it in `out` under its fully-qualified key (keyPrefix + descended namespaces + leaf).
// Re-keys the entry's own internal base so the build graph references the fully-qualified
// ancestor, and recurses to pull that ancestor too.
func pullNamespacedBox(from *spec.Config, ref, keyPrefix, calverTag, dir string, opts ResolveOpts, out map[string]*ResolvedBox) error {
	cur := from
	prefix := keyPrefix
	for {
		ns, rest, qualified := spec.SplitNamespaceRef(ref)
		if !qualified {
			break
		}
		child, ok := cur.Namespaces[ns]
		if !ok {
			return fmt.Errorf("import namespace %q not found (resolving %q)", ns, keyPrefix+ref)
		}
		cur = child
		prefix += ns + "."
		ref = rest
	}
	fullKey := prefix + ref
	if _, ok := out[fullKey]; ok {
		return nil
	}
	if _, ok := cur.Box[ref]; !ok {
		return fmt.Errorf("imported image %q not found in namespace", fullKey)
	}
	ri, err := ResolveBox(cur, ref, calverTag, dir, opts)
	if err != nil {
		return fmt.Errorf("resolving imported image %q: %w", fullKey, err)
	}
	// Re-qualify EVERY by-name ref to another image that the build graph later resolves. A pulled
	// image's refs are namespace-relative to `cur` (the namespace it was authored in); left
	// untouched they get re-resolved from the ROOT config — where `cur`'s own namespaces don't
	// exist — yielding `import namespace "charly" not found`. Prefixing with prefix
	// (`charly.arch-builder` → `selkies.charly.arch-builder`) makes each ref resolvable from root
	// AND matches the key pullNamespacedBox stores the target under.
	requalify := func(r string) string {
		if r == "" {
			return r
		}
		return prefix + r
	}
	if len(ri.Builder) > 0 {
		rk := make(BuilderMap, len(ri.Builder))
		for format, b := range ri.Builder {
			rk[format] = requalify(b)
		}
		ri.Builder = rk
	}
	ri.BootstrapBuilderImage = requalify(ri.BootstrapBuilderImage)
	if ri.IsExternalBase {
		out[fullKey] = ri
		return nil
	}
	// Internal base within `cur` — re-qualify it (drives the recursion below), store, recurse.
	origBase := ri.Base
	ri.Base = requalify(origBase)
	out[fullKey] = ri
	return pullNamespacedBox(cur, origBase, prefix, calverTag, dir, opts, out)
}
