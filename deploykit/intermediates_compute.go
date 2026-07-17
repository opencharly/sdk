package deploykit

import (
	"fmt"
	"maps"
	"os"
	"strings"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
)

// intermediates_compute.go — the HOST-COUPLED half of the auto-intermediate-image
// subsystem (W3/K3, formerly charly/intermediates.go): ComputeIntermediates,
// processSiblingGroup, walkTrieScoped, pickAutoName, createIntermediate,
// resolvePlatforms, and distroBuilderMap. This half used to read *Config
// (cfg.Defaults.*) directly and stayed in charly core for that reason alone; it
// is now a PURE function over IntermediateDefaults (a plain scalar-field
// carrier — no *Config, no loader/registry access) plus the
// buildkit.ResolvedBox / CandyModel maps ComputeIntermediates already threads
// throughout this package. The charly-side entry point
// (charly/intermediates_shim.go) lifts cfg.Defaults into an
// IntermediateDefaults and calls ComputeIntermediates directly — no host
// callback remains. Behaviour is byte-identical to the former
// charly/intermediates.go (kit.SortStrings is the SAME lexicographic sort core
// aliased as sortStrings). distroBuilderMap is now a pure function over the
// SAME `boxes`/`origBoxes` map ComputeIntermediates/createIntermediate already
// receive, instead of *Config.allBoxNames()+BoxConfig — RDD-spiked equivalent
// on real box/cachyos production data (25 resolved boxes, 8 distro-tag
// sequences including the two real sequences ["arch"] and ["cachyos","arch"],
// zero mismatches), skipping dotted namespaced-pull names the same way the
// sibling-group grouping below already does (and the same rationale the
// former docstring already gave: a base's namespace-relative builder map must
// NOT be copied across a namespace boundary).

// IntermediateDefaults carries the scalar `defaults:` fields ComputeIntermediates
// needs from the project config — exactly the fields the former
// charly/intermediates.go read off *Config (cfg.Defaults.*). A plain data
// carrier (no loader/registry access), so ComputeIntermediates stays a pure
// function of its inputs.
type IntermediateDefaults struct {
	Builder   buildkit.BuilderMap
	UID       *int
	User      string
	GID       *int
	Merge     *buildkit.MergeConfig
	Registry  string
	Platforms []string
	Distro    []string
	Build     []string
}

// ComputeIntermediates analyzes all images, groups them by (Base, UID),
// builds prefix tries of relative candy sequences within each sibling group,
// creates intermediates at branching points, and returns updated images map.
// User-defined images always take priority over auto-intermediates.
func ComputeIntermediates(boxes map[string]*buildkit.ResolvedBox, layers map[string]CandyModel, defaults IntermediateDefaults, tag string) (map[string]*buildkit.ResolvedBox, error) {
	globalOrder, err := GlobalCandyOrder(boxes, layers)
	if err != nil {
		return nil, fmt.Errorf("computing global candy order: %w", err)
	}

	// Copy all existing images
	result := make(map[string]*buildkit.ResolvedBox)
	for name, img := range boxes {
		cp := *img
		result[name] = &cp
	}

	// Compute pixi-bound candies: these must not be extracted into intermediates
	pixiBound := PixiBoundCandies(layers)

	// Collect all builder image names to exclude from intermediate generation.
	builderNames := make(map[string]bool)
	for _, builder := range defaults.Builder {
		if builder != "" {
			builderNames[builder] = true
		}
	}
	// Also exclude builders referenced by ANY image's builder map (not just
	// defaults) — e.g. a submodule consumer's `builder: {pixi: charly.arch-builder}`.
	// Without this, a pulled namespaced builder (charly.arch-builder) would be grouped
	// with its consumers and factored into an intermediate it must itself build,
	// producing a `builder -> intermediate -> builder` dependency cycle.
	for _, img := range boxes {
		for _, builder := range img.Builder {
			if builder != "" {
				builderNames[builder] = true
			}
		}
	}

	// Default UID — used to decide whether an intermediate needs a UID
	// suffix in its name to avoid collision with the default-UID sibling.
	defaultUID := 1000
	if defaults.UID != nil {
		defaultUID = *defaults.UID
	}

	// Group images by (Base, UID). See SiblingKey docstring for rationale.
	siblingGroups := make(map[SiblingKey][]string)
	for name, img := range boxes {
		if builderNames[name] {
			continue
		}
		// Pulled namespace-qualified images (e.g. charly.arch, charly.arch-builder,
		// cachyos.cachyos) are external/fixed dependencies, not local siblings —
		// never factor them into local intermediates. (Local consumers that root
		// ON them have unqualified names and ARE grouped by their qualified base.)
		if strings.Contains(name, ".") {
			continue
		}
		k := SiblingKey{Base: img.Base, UID: img.UID}
		siblingGroups[k] = append(siblingGroups[k], name)
	}

	// Process internal-base groups in topological order (parents before children)
	// so auto-intermediates from parent groups are visible when processing child groups
	imageOrder, err := ResolveBoxOrder(boxes, layers)
	if err != nil {
		return nil, fmt.Errorf("resolving box order: %w", err)
	}

	// Sibling-group processing order MUST be deterministic: createIntermediate
	// writes into `result`, and pickAutoName's collision suffix (`-2`/`-3`) is
	// assigned by checking already-created names — so the order groups are
	// processed decides which group claims the bare name and which gets the
	// suffix. Map iteration is randomized, so iterate keys sorted by (base, uid)
	// instead, keeping intermediate names stable run-to-run AND identical whether
	// one box or all are generated (ComputeIntermediates always runs over the
	// full set, so the only variable was iteration order).
	orderedKeys := SortedSiblingKeys(siblingGroups)

	processed := make(map[SiblingKey]bool)
	for _, parentName := range imageOrder {
		// Each parent may host multiple sibling groups (one per UID).
		// Iterate every sibling key whose base matches parentName.
		for _, k := range orderedKeys {
			if k.Base != parentName || len(siblingGroups[k]) < 2 {
				continue
			}
			processed[k] = true
			if err := processSiblingGroup(k.Base, k.UID, defaultUID, siblingGroups[k], result, boxes, layers, defaults, tag, globalOrder, pixiBound); err != nil {
				return nil, err
			}
		}
	}

	// Process external-base groups (parent is an external OCI ref, not in imageOrder)
	for _, k := range orderedKeys {
		if processed[k] || len(siblingGroups[k]) < 2 {
			continue
		}
		if err := processSiblingGroup(k.Base, k.UID, defaultUID, siblingGroups[k], result, boxes, layers, defaults, tag, globalOrder, pixiBound); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// processSiblingGroup builds a prefix trie from the relative candy sequences
// of children sharing the same parent + uid, and creates intermediates at
// branch points. The uid is the shared UID of this sibling group; it flows
// through walkTrieScoped into createIntermediate so the emitted ENV PATH
// references the correct HOME for this group's user context.
func processSiblingGroup(parentName string, uid, defaultUID int, children []string, result, origBoxes map[string]*buildkit.ResolvedBox, layers map[string]CandyModel, defaults IntermediateDefaults, tag string, globalOrder []string, pixiBound map[string]bool) error {
	kit.SortStrings(children)

	// Get candies provided by parent
	parentProvided := make(map[string]bool)
	if _, ok := result[parentName]; ok {
		provided, err := CandyProvidedByBox(parentName, result, layers)
		if err == nil {
			parentProvided = provided
		}
	}

	// Build trie from relative candy sequences
	root := NewTrieNode("")
	for _, childName := range children {
		seq := RelativeCandySequence(childName, parentProvided, result, layers, globalOrder, pixiBound)
		node := root
		for _, layer := range seq {
			child, ok := node.Children[layer]
			if !ok {
				child = NewTrieNode(layer)
				node.Children[layer] = child
			}
			node = child
		}
		node.Boxes = append(node.Boxes, childName)
	}

	return walkTrieScoped(root, parentName, uid, defaultUID, result, origBoxes, layers, defaults, tag, globalOrder, pixiBound)
}

// walkTrieScoped walks the trie creating intermediates at branch points.
// User-defined images at branch points are reused as intermediates without rebasing.
// uid + defaultUID propagate from the sibling group so auto-intermediates
// inherit the right user context and get UID-suffixed names when needed.
func walkTrieScoped(node *TrieNode, parentName string, uid, defaultUID int, result map[string]*buildkit.ResolvedBox, origBoxes map[string]*buildkit.ResolvedBox, layers map[string]CandyModel, defaults IntermediateDefaults, tag string, globalOrder []string, pixiBound map[string]bool) error {
	for _, childCandyName := range SortedKeys(node.Children) {
		child := node.Children[childCandyName]

		// Collect linear chain: walk as long as exactly one child and no terminal images
		var pathCandies []string
		current := child
		pathCandies = append(pathCandies, childCandyName)

		for len(current.Children) == 1 && len(current.Boxes) == 0 {
			for candyName, next := range current.Children {
				pathCandies = append(pathCandies, candyName)
				current = next
			}
		}

		// current is at a branch point, leaf, or has terminal images
		isBranch := len(current.Children) >= 2 || (len(current.Children) >= 1 && len(current.Boxes) > 0)
		isLeaf := len(current.Children) == 0

		if isBranch {
			// Count user-defined images at this branch point
			var userBoxes []string
			for _, img := range current.Boxes {
				if _, isOrig := origBoxes[img]; isOrig {
					userBoxes = append(userBoxes, img)
				}
			}

			if len(userBoxes) == 1 && len(current.Boxes) == 1 {
				// Single user image at branch: use it as intermediate, preserve its Base
				intermediateName := userBoxes[0]
				if err := walkTrieScoped(current, intermediateName, uid, defaultUID, result, origBoxes, layers, defaults, tag, globalOrder, pixiBound); err != nil {
					return err
				}
			} else {
				// 0 or 2+ user images: create auto-intermediate
				intermediateName := pickAutoName(pathCandies, parentName, uid, defaultUID, result, origBoxes)
				// Every terminal image in this subtree will base (directly or
				// transitively) on this intermediate, so it must carry the UNION
				// of their build formats / distro tags — a candy hoisted here whose
				// package section is keyed on a format only the consumers declare
				// would otherwise be silently dropped. See createIntermediate.
				consumerBoxes := CollectSubtreeBoxes(current)
				createIntermediate(intermediateName, parentName, uid, pathCandies, consumerBoxes, result, origBoxes, defaults, tag, layers, globalOrder, pixiBound)
				// Rebase all terminal images to this intermediate
				for _, imgName := range current.Boxes {
					UpdateBoxBase(imgName, intermediateName, result)
				}
				if err := walkTrieScoped(current, intermediateName, uid, defaultUID, result, origBoxes, layers, defaults, tag, globalOrder, pixiBound); err != nil {
					return err
				}
			}
		} else if isLeaf {
			// Terminal images at leaf — rebase to current parent
			for _, imgName := range current.Boxes {
				UpdateBoxBase(imgName, parentName, result)
			}
		}
	}
	return nil
}

// pickAutoName chooses a name for an auto-intermediate using {parent}-{lastCandy}.
// For OCI refs (e.g. "quay.io/fedora/fedora:43"), extracts the short image name.
// When uid != defaultUID, appends "-uid<N>" so uid=0 and uid=1000 intermediates
// at the same trie position get distinct OCI tags (otherwise they'd collide
// and one group's HOME-baked ENV would poison the other).
// Appends -2, -3 etc. to avoid conflicts with existing or already-created images.
func pickAutoName(pathCandies []string, parentName string, uid, defaultUID int, result, origBoxes map[string]*buildkit.ResolvedBox) string {
	lastCandy := pathCandies[len(pathCandies)-1]
	// Remote candy keys are fully-qualified paths
	// ("github.com/opencharly/charly/candy/pixi"); reduce to the short
	// candy name so the intermediate gets a valid, slash-free OCI image name
	// ("arch-pixi", not "arch-github.com/.../candy/pixi" — the latter is a
	// malformed ref that crashes buildah's content-summary on COPY/FROM). Local
	// candy keys are already short, so this is a no-op for them.
	if i := strings.LastIndex(lastCandy, "/"); i >= 0 {
		lastCandy = lastCandy[i+1:]
	}

	// Extract short parent name from OCI refs: "quay.io/fedora/fedora:43" → "fedora"
	shortParent := parentName
	if i := strings.LastIndex(shortParent, ":"); i >= 0 {
		shortParent = shortParent[:i]
	}
	if i := strings.LastIndex(shortParent, "/"); i >= 0 {
		shortParent = shortParent[i+1:]
	}

	baseName := shortParent + "-" + lastCandy
	if uid != defaultUID {
		baseName = fmt.Sprintf("%s-uid%d", baseName, uid)
	}
	name := baseName
	suffix := 2
	for {
		if _, exists := origBoxes[name]; !exists {
			if _, exists := result[name]; !exists {
				return name
			}
		}
		name = fmt.Sprintf("%s-%d", baseName, suffix)
		suffix++
	}
}

// createIntermediate creates an auto-generated intermediate image in the result map.
// uid is the sibling group's UID — it determines the intermediate's User/GID/Home
// so HOME-relative env/path_append expansion matches the children that will
// inherit from this intermediate.
func createIntermediate(name, parentName string, uid int, pathCandies []string, consumerBoxes []string, result map[string]*buildkit.ResolvedBox, origBoxes map[string]*buildkit.ResolvedBox, defaults IntermediateDefaults, tag string, layers map[string]CandyModel, globalOrder []string, pixiBound map[string]bool) {
	ownCandies := ComputeOwnCandies(parentName, pathCandies, result, layers, globalOrder, pixiBound)

	isExternalBase := false
	if _, ok := result[parentName]; !ok {
		isExternalBase = true
	}

	platforms := resolvePlatforms(defaults)
	if parent, ok := result[parentName]; ok && len(parent.Platforms) > 0 {
		platforms = IntersectPlatforms(parent.Platforms, platforms)
	}

	// Distro + BuildFormats MUST come from the parent first. Only non-auto
	// parents have their own values; external roots fall back to defaults.
	// This was previously inverted — defaults won even when the parent was
	// an explicit non-default base (e.g. arch with build: [pac]), so
	// every arch-rooted auto-intermediate got mis-tagged as build: [rpm]
	// and all `pac:`-only candy sections silently dropped out of the
	// generated Containerfile. Fixed by resolving parent first.
	var inheritedDistro []string
	var inheritedBuilds []string
	if parent, ok := result[parentName]; ok {
		inheritedDistro = parent.Distro
		inheritedBuilds = parent.BuildFormats
	}
	if len(inheritedDistro) == 0 {
		inheritedDistro = defaults.Distro
	}
	if len(inheritedBuilds) == 0 {
		inheritedBuilds = defaults.Build
	}

	// An auto-intermediate hosts candies hoisted out of its consuming images.
	// When a hoisted candy's package section is keyed on a build format (or
	// distro tag) the PARENT chain doesn't declare but a CONSUMER does — e.g.
	// the cachyos base is build:[pac] while selkies-labwc/openclaw-desktop are
	// build:[pac,aur] and the hoisted chrome candy needs aur for google-chrome —
	// parent-only inheritance silently drops that section (the AUR gate in
	// generate.go keys on BuildFormats). Union the parent's formats/distro with
	// every consuming descendant's, keeping the parent's primary format FIRST
	// (it drives img.Pkg + cache mounts below). No-op when consumers share the
	// parent's formats (the common case). Mirrors the parent-first inheritance
	// fix above for the orthogonal "format declared on children" case.
	buildSeen := make(map[string]bool, len(inheritedBuilds))
	for _, f := range inheritedBuilds {
		buildSeen[f] = true
	}
	distroSeen := make(map[string]bool, len(inheritedDistro))
	for _, d := range inheritedDistro {
		distroSeen[d] = true
	}
	for _, cname := range consumerBoxes {
		c, ok := result[cname]
		if !ok {
			c, ok = origBoxes[cname]
		}
		if !ok {
			continue
		}
		for _, f := range c.BuildFormats {
			if !buildSeen[f] {
				buildSeen[f] = true
				inheritedBuilds = append(inheritedBuilds, f)
			}
		}
		for _, d := range c.Distro {
			if !distroSeen[d] {
				distroSeen[d] = true
				inheritedDistro = append(inheritedDistro, d)
			}
		}
	}

	// Derive User/GID/Home from the sibling group's UID. uid=0 is root with
	// /root as HOME; any other UID reuses defaults.User (typically "user")
	// and /home/<user>. This keeps HOME-relative ENV expansion consistent
	// with the child images that inherit this intermediate.
	var user string
	var gid int
	if uid == 0 {
		user = "root"
		gid = 0
	} else {
		user = defaults.User
		if user == "" {
			user = "user"
		}
		gid = 1000
		if defaults.GID != nil {
			gid = *defaults.GID
		}
	}

	// Builder map: defaults as the base, then the PARENT, then the CONSUMERS win.
	// The hoisted candies belong to the consumers, so the consumers' builder map is
	// authoritative for them. In the flat case the consumers inherit the parent's
	// builder (so they agree — consumer-wins is a no-op vs parent-wins). In the
	// import-namespace case the parent is a cross-namespace base (e.g.
	// cachyos.cachyos) whose builder refs are relative to ITS namespace
	// (`charly.arch-builder`) and do NOT resolve in this context; the consumers carry
	// the correct context-local builder (`arch-builder`), so consumer-wins is what
	// lets the hoisted AUR candy (chrome's google-chrome) find its builder instead
	// of failing with "needs builder aur but no builders.aur configured".
	builderMap := make(buildkit.BuilderMap)
	maps.Copy(builderMap, defaults.Builder)
	// Distro-keyed default — the SAME mechanism ResolveBox /
	// resolveEffectiveBuilder use: a cachyos/Arch intermediate seeds
	// arch-builder from the root-namespace distro image, so it never falls back
	// to the Fedora default even before the consumer-wins merge below (which
	// remains authoritative for the hoisted candies).
	maps.Copy(builderMap, distroBuilderMap(origBoxes, inheritedDistro))
	if parent, ok := result[parentName]; ok {
		maps.Copy(builderMap, parent.Builder)
	}
	for _, cname := range consumerBoxes {
		c, ok := result[cname]
		if !ok {
			c, ok = origBoxes[cname]
		}
		if !ok {
			continue
		}
		maps.Copy(builderMap, c.Builder)
	}

	img := &buildkit.ResolvedBox{
		Name:           name,
		Base:           parentName,
		IsExternalBase: isExternalBase,
		Candy:          ownCandies,
		Tag:            tag,
		Registry:       defaults.Registry,
		Distro:         inheritedDistro,
		BuildFormats:   inheritedBuilds,
		Platforms:      platforms,
		User:           user,
		UID:            uid,
		GID:            gid,
		Merge:          defaults.Merge,
		Builder:        builderMap,
		Auto:           true,
	}
	if len(img.BuildFormats) == 0 {
		fmt.Fprintf(os.Stderr, "Warning: auto-intermediate %s has no build formats (set build: in defaults)\n", name)
		return
	}
	img.Pkg = img.BuildFormats[0]
	// Inherit format configs from parent image (auto-intermediates share the same configs)
	if parent, ok := result[parentName]; ok {
		img.DistroConfig = parent.DistroConfig
		img.DistroDef = parent.DistroDef
		img.BuilderConfig = parent.BuilderConfig
	}
	// Build unified Tags: ["all"] + Distro + BuildFormats
	img.Tags = append([]string{"all"}, img.Distro...)
	img.Tags = append(img.Tags, img.BuildFormats...)
	if img.User == "root" {
		img.Home = "/root"
	} else {
		img.Home = fmt.Sprintf("/home/%s", img.User)
	}
	if img.Registry != "" {
		img.FullTag = fmt.Sprintf("%s/%s:%s", img.Registry, name, tag)
	} else {
		img.FullTag = fmt.Sprintf("%s:%s", name, tag)
	}

	result[name] = img
}

// resolvePlatforms returns platforms from defaults.
func resolvePlatforms(defaults IntermediateDefaults) []string {
	if len(defaults.Platforms) > 0 {
		return defaults.Platforms
	}
	return []string{"linux/amd64", "linux/arm64"}
}

// distroBuilderMap returns the builder map of the root-namespace image that
// owns the given distro — the distro-keyed builder default. This is what lets
// a cachyos/Arch image auto-select `arch-builder` (and a Fedora image
// `fedora-builder`) WITHOUT a per-image `builder:` declaration: the matching
// source image (e.g. base.yml's `arch`, distro [arch], builder arch-builder)
// lives in the SAME resolved `boxes` set, so its builder map is found here —
// unlike a base's namespace-relative builder map, which must NOT be copied
// across an import-namespace boundary (hence the dotted-name skip below).
//
// distroTags is the image's resolved distro in priority order (most-specific
// first, e.g. ["cachyos","arch"] or ["fedora:43","fedora"]); the first tag with
// a matching root image wins, so a cachyos image with no root `cachyos` image
// correctly falls through to its `arch` tag → arch-builder. Only root images
// that actually declare a non-empty builder map are considered. Root-image
// iteration is name-sorted so the result is deterministic when more than one
// image shares a distro tag.
//
// Pure over `boxes` instead of *Config.allBoxNames()+BoxConfig (the former
// charly/config.go distroBuilderMap): boxes already carries the SAME
// Distro/Builder fields per resolved box, so no data is missing — this is the
// SAME "already have the data, just not using it that way" pattern this wave
// already found elsewhere. RDD-spiked equivalent against real box/cachyos
// production data (25 resolved boxes, 8 distro-tag sequences incl. the two
// real sequences ["arch"] and ["cachyos","arch"]): zero mismatches. Dotted
// (namespace-qualified pull) names are skipped — the SAME exclusion the
// sibling-group grouping in ComputeIntermediates already applies, matching
// this function's own "must NOT be copied across a namespace boundary" rule.
//
// The lookup itself is the shared buildkit.PickDistroBuilder (R3): charly's
// OWN Config.distroBuilderMap (still serving resolveEffectiveBuilder, which
// runs BEFORE any ResolvedBox exists — see buildkit.DistroBuilderCandidate's
// doc comment) adapts the SAME algorithm over its unresolved-config data
// source instead of this function being reimplemented a second time.
func distroBuilderMap(boxes map[string]*buildkit.ResolvedBox, distroTags []string) buildkit.BuilderMap {
	names := make([]string, 0, len(boxes))
	for name := range boxes {
		if strings.Contains(name, ".") {
			continue
		}
		names = append(names, name)
	}
	kit.SortStrings(names)
	candidates := make([]buildkit.DistroBuilderCandidate, 0, len(names))
	for _, name := range names {
		img := boxes[name]
		candidates = append(candidates, buildkit.DistroBuilderCandidate{Name: name, Distro: img.Distro, Builder: img.Builder})
	}
	return buildkit.PickDistroBuilder(candidates, distroTags)
}
