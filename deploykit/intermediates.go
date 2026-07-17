package deploykit

import (
	"fmt"
	"slices"
	"sort"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
)

// intermediates.go — the PURE-compute candy-graph/trie half of the
// auto-intermediate-image subsystem, relocated from charly core (P8b). These
// functions operate over deploykit.CandyModel + buildkit.ResolvedBox (the
// candy-graph model), so they belong beside the graph funcs in graph.go. The
// formerly HOST-COUPLED half — ComputeIntermediates / processSiblingGroup /
// createIntermediate / walkTrieScoped / pickAutoName / resolvePlatforms /
// distroBuilderMap — lives in intermediates_compute.go in this SAME package
// (W3): it consumes the scalar defaults through IntermediateDefaults instead
// of reading *Config, so the whole subsystem is now deploykit-resident and
// charly/intermediates_shim.go is only the thin cfg.Defaults-lifting entry
// point. Behaviour is byte-identical to the former charly/intermediates.go
// (kit.SortStrings is the SAME lexicographic sort core aliased as sortStrings).

// PixiBoundCandies identifies candies that have install files (user.yml/root.yml)
// but depend on a pixi environment from their including parent meta-candy.
// These candies must NOT be extracted into auto-intermediates because the
// intermediate won't have the pixi environment they need.
//
// A candy is pixi-bound if:
// 1. It has install files (user.yml or root.yml)
// 2. It does NOT have its own pixi manifest (pixi.toml/pyproject.toml/environment.yml)
// 3. It is included via candy: by another candy that DOES have a pixi manifest
func PixiBoundCandies(layers map[string]CandyModel) map[string]bool {
	bound := make(map[string]bool)
	for _, layer := range layers {
		if layer.PixiManifest() == "" {
			continue
		}
		// This candy owns a pixi env. Check its IncludedCandies.
		for _, includedRef := range layer.GetIncludedCandy() {
			included := includedRef.Bare()
			child, ok := layers[included]
			if !ok {
				continue
			}
			// If the included candy has install files but no pixi manifest,
			// it depends on this parent's pixi env and must not be extracted.
			if child.HasInstallFiles() && child.PixiManifest() == "" {
				bound[included] = true
			}
		}
	}
	return bound
}

// TrieNode represents a node in the candy prefix trie.
type TrieNode struct {
	Layer    string               // layer at this position ("" for root)
	Children map[string]*TrieNode // layer name → child node
	Boxes    []string             // user-defined images terminating here
}

// NewTrieNode returns an empty trie node labeled with layer.
func NewTrieNode(layer string) *TrieNode {
	return &TrieNode{
		Layer:    layer,
		Children: make(map[string]*TrieNode),
	}
}

// GlobalCandyOrder computes a global topological order of all candies across
// all enabled images, using popularity (number of images needing each candy)
// as the primary tie-breaker and lexicographic as secondary.
func GlobalCandyOrder(boxes map[string]*buildkit.ResolvedBox, layers map[string]CandyModel) ([]string, error) {
	// Count popularity: how many images need each candy (including transitive deps)
	popularity := make(map[string]int)
	for _, img := range boxes {
		resolved, err := ResolveCandyOrder(img.Candy, layers, nil)
		if err != nil {
			return nil, fmt.Errorf("resolving candies for image %q: %w", img.Name, err)
		}
		// Also include candies from the base chain
		allCandies := CollectAllBoxCandies(img.Name, boxes, layers)
		// Merge resolved with allCandies
		seen := make(map[string]bool)
		for _, l := range allCandies {
			seen[l] = true
		}
		for _, l := range resolved {
			if !seen[l] {
				allCandies = append(allCandies, l)
				seen[l] = true
			}
		}
		for _, l := range allCandies {
			popularity[l]++
		}
	}

	// Build dependency graph from candy depends and included candies
	// Only include candies that appear in at least one image
	graph := make(map[string][]string)
	for name := range popularity {
		layer, ok := layers[name]
		if !ok {
			continue
		}
		var deps []string
		for _, depRef := range layer.GetRequire() {
			dep := depRef.Bare()
			if _, inUse := popularity[dep]; inUse {
				deps = append(deps, dep)
			}
		}
		for _, includedRef := range layer.GetIncludedCandy() {
			included := includedRef.Bare()
			if _, inUse := popularity[included]; inUse {
				deps = append(deps, included)
			}
		}
		graph[name] = deps
	}

	// Authored candy-list order is an ordering CONSTRAINT, not just a seed set.
	// When an image (or metacandy) writes `candy: [A, B]`, the author means A's
	// install steps run before B's — even when B declares no `require: A`. The
	// canonical case is the builder images' `[rpmfusion, …, build-toolchain]`:
	// build-toolchain installs ffmpeg-devel / x264-devel / libva-devel, which
	// live in the RPM Fusion repos that the rpmfusion candy enables, yet
	// build-toolchain CANNOT `require: rpmfusion` (it is also used on Arch, where
	// those libs come from the distro repos). Without honoring authored order the
	// popularity tie-break can place build-toolchain ahead of rpmfusion in a
	// project whose image set makes build-toolchain the more popular candy,
	// emitting its dnf install before the repos exist and breaking the build.
	//
	// We add each list-adjacent graph-node pair as a dependency edge (the later
	// entry depends on the earlier), skipping any edge that is redundant or that
	// would create a cycle — so genuinely conflicting authored orders fall back
	// to the popularity tie-break exactly as before, while consistent orders
	// (the overwhelming majority) are now respected.
	isNode := func(name string) bool {
		if _, ok := popularity[name]; !ok {
			return false
		}
		_, ok := layers[name]
		return ok
	}
	addListOrderEdge := func(prev, cur string) {
		if prev == cur || !isNode(prev) || !isNode(cur) {
			return
		}
		if slices.Contains(graph[cur], prev) {
			return // already constrained
		}
		// Adding "cur depends on prev" creates a cycle iff prev already
		// (transitively) depends on cur.
		if GraphReaches(graph, prev, cur) {
			return
		}
		graph[cur] = append(graph[cur], prev)
	}
	addListEdges := func(list []string) {
		for i := 1; i < len(list); i++ {
			addListOrderEdge(list[i-1], list[i])
		}
	}
	// Every authored candy list contributes ordering edges: image-level lists
	// AND metacandy `candy:` (IncludedCandy) lists. Non-node entries (pure-
	// composition metacandies with no RUN steps) are skipped by addListOrderEdge,
	// so only content candies are constrained.
	//
	// Iterate in sorted name order, NOT map-iteration order: addListOrderEdge
	// skips any edge that would create a cycle, so the SET of edges added depends
	// on insertion order. Go map iteration is randomized, which would make the
	// graph — and thus the global candy order and every auto-intermediate derived
	// from it — vary run-to-run, breaking cross-run cache reuse. Sorted iteration
	// makes the edge set, the topo order, and the intermediates deterministic.
	boxNames := make([]string, 0, len(boxes))
	for name := range boxes {
		boxNames = append(boxNames, name)
	}
	kit.SortStrings(boxNames)
	for _, name := range boxNames {
		addListEdges(boxes[name].Candy)
	}
	candyNames := make([]string, 0, len(popularity))
	for name := range popularity {
		candyNames = append(candyNames, name)
	}
	kit.SortStrings(candyNames)
	for _, name := range candyNames {
		if l, ok := layers[name]; ok {
			inc := l.GetIncludedCandy()
			bare := make([]string, len(inc))
			for i, r := range inc {
				bare[i] = r.Bare()
			}
			addListEdges(bare)
		}
	}

	// Kahn's algorithm with popularity-based tie-breaking
	return TopoSortByPopularity(graph, popularity)
}

// GraphReaches reports whether `to` is reachable from `from` by following
// dependency edges (graph[x] lists the candies x depends on). Used to keep
// authored-list-order edge insertion cycle-safe in GlobalCandyOrder.
func GraphReaches(graph map[string][]string, from, to string) bool {
	if from == to {
		return true
	}
	visited := make(map[string]bool)
	var dfs func(n string) bool
	dfs = func(n string) bool {
		if n == to {
			return true
		}
		if visited[n] {
			return false
		}
		visited[n] = true
		return slices.ContainsFunc(graph[n], dfs)
	}
	return dfs(from)
}

// TopoSortByPopularity performs topological sort with popularity tie-breaking.
// Higher popularity candies come first among zero-in-degree candidates.
func TopoSortByPopularity(graph map[string][]string, popularity map[string]int) ([]string, error) {
	inDegree := make(map[string]int)
	reverseGraph := make(map[string][]string)

	for node := range graph {
		inDegree[node] = len(graph[node])
		for _, dep := range graph[node] {
			reverseGraph[dep] = append(reverseGraph[dep], node)
		}
	}

	// Find all nodes with no dependencies
	var queue []string
	for node, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, node)
		}
	}
	SortByPopularity(queue, popularity)

	var result []string
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		result = append(result, node)

		dependents := reverseGraph[node]
		for _, dep := range dependents {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
		SortByPopularity(queue, popularity)
	}

	if len(result) != len(graph) {
		return nil, fmt.Errorf("cycle detected in candy dependency graph")
	}
	return result, nil
}

// SortByPopularity sorts by descending popularity, then lexicographic ascending.
func SortByPopularity(s []string, popularity map[string]int) {
	for i := 0; i < len(s)-1; i++ {
		for j := i + 1; j < len(s); j++ {
			pi, pj := popularity[s[i]], popularity[s[j]]
			if pi < pj || (pi == pj && s[i] > s[j]) {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

// AbsoluteCandySequence returns an image's complete candy set (own + entire
// base chain) as a subsequence of the global order.
func AbsoluteCandySequence(boxName string, boxes map[string]*buildkit.ResolvedBox, layers map[string]CandyModel, globalOrder []string) []string {
	allCandies := CollectAllBoxCandies(boxName, boxes, layers)
	candySet := make(map[string]bool, len(allCandies))
	for _, l := range allCandies {
		candySet[l] = true
	}

	// Filter global order to only include this image's candies
	var seq []string
	for _, l := range globalOrder {
		if candySet[l] {
			seq = append(seq, l)
		}
	}
	return seq
}

// SiblingKey identifies a sibling-grouping equivalence class for intermediate
// computation. Grouping by (Base, UID) — not just Base — ensures that images
// with different user contexts (e.g. uid=1000 default vs. uid=0 root) don't
// share an auto-intermediate. Sharing would bake one group's HOME-relative
// paths (path_append `~/.foo/bin`, env vars using `~` or `$HOME`) into the
// intermediate's `ENV PATH` directives, leaving the other group with dead
// PATH entries that can't be overridden by its own ENV emission.
type SiblingKey struct {
	Base string
	UID  int
}

// SortedSiblingKeys returns the sibling-group keys in a deterministic order —
// by base name, then UID. Used to make auto-intermediate creation independent of
// Go's randomized map iteration (see ComputeIntermediates).
func SortedSiblingKeys(m map[SiblingKey][]string) []SiblingKey {
	keys := make([]SiblingKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Base != keys[j].Base {
			return keys[i].Base < keys[j].Base
		}
		return keys[i].UID < keys[j].UID
	})
	return keys
}

// RelativeCandySequence returns an image's candies minus what the parent provides,
// ordered according to the global candy order.
func RelativeCandySequence(boxName string, parentProvided map[string]bool, boxes map[string]*buildkit.ResolvedBox, layers map[string]CandyModel, globalOrder []string, pixiBound map[string]bool) []string {
	allCandies := CollectAllBoxCandies(boxName, boxes, layers)
	candySet := make(map[string]bool, len(allCandies))
	for _, l := range allCandies {
		candySet[l] = true
	}

	var seq []string
	for _, l := range globalOrder {
		if candySet[l] && !parentProvided[l] && !pixiBound[l] {
			seq = append(seq, l)
		}
	}
	return seq
}

// CollectSubtreeBoxes returns every terminal user image in the subtree rooted
// at node — the images terminating at node plus all images in descendant nodes.
// These are exactly the images that will base, directly or transitively, on an
// auto-intermediate created at this node, so they define the union of build
// formats / distro tags the intermediate must carry (see createIntermediate).
func CollectSubtreeBoxes(node *TrieNode) []string {
	out := append([]string(nil), node.Boxes...)
	// Sorted child traversal (not map order) so consumerBoxes — and the
	// format/distro union createIntermediate derives from it — are deterministic.
	for _, childName := range SortedKeys(node.Children) {
		out = append(out, CollectSubtreeBoxes(node.Children[childName])...)
	}
	return out
}

// ComputeOwnCandies determines which candies an intermediate needs to install
// (pathCandies minus what the parent already provides).
func ComputeOwnCandies(parentName string, pathCandies []string, result map[string]*buildkit.ResolvedBox, layers map[string]CandyModel, globalOrder []string, pixiBound map[string]bool) []string {
	parentProvided := make(map[string]bool)
	if _, ok := result[parentName]; ok {
		provided, err := CandyProvidedByBox(parentName, result, layers)
		if err == nil {
			parentProvided = provided
		}
	}

	var own []string
	for _, l := range pathCandies {
		if !parentProvided[l] {
			own = append(own, l)
		}
	}

	// Also include transitive dependencies of these candies that aren't parent-provided
	needed := make(map[string]bool)
	for _, l := range own {
		needed[l] = true
		AddTransitiveDeps(l, layers, needed, parentProvided)
	}

	// Return in global order, excluding pixi-bound candies
	var ordered []string
	for _, l := range globalOrder {
		if needed[l] && !parentProvided[l] && !pixiBound[l] {
			ordered = append(ordered, l)
		}
	}
	if len(ordered) == 0 {
		return own // fallback
	}
	return ordered
}

// AddTransitiveDeps adds all transitive dependencies of a candy to the needed set.
func AddTransitiveDeps(candyName string, layers map[string]CandyModel, needed map[string]bool, excluded map[string]bool) {
	layer, ok := layers[candyName]
	if !ok {
		return
	}
	for _, depRef := range layer.GetRequire() {
		dep := depRef.Bare()
		if excluded[dep] || needed[dep] {
			continue
		}
		needed[dep] = true
		AddTransitiveDeps(dep, layers, needed, excluded)
	}
	for _, includedRef := range layer.GetIncludedCandy() {
		included := includedRef.Bare()
		if excluded[included] || needed[included] {
			continue
		}
		needed[included] = true
		AddTransitiveDeps(included, layers, needed, excluded)
	}
}

// UpdateBoxBase updates an image's Base to point to the given parent.
func UpdateBoxBase(imgName, parentName string, result map[string]*buildkit.ResolvedBox) {
	img, ok := result[imgName]
	if !ok {
		return
	}
	img.Base = parentName
	if _, isInternal := result[parentName]; isInternal {
		img.IsExternalBase = false
	} else {
		img.IsExternalBase = true
	}
}

// IntersectPlatforms returns platforms present in both slices.
// If the intersection is empty, returns parent (the more restrictive set).
func IntersectPlatforms(parent, defaults []string) []string {
	set := make(map[string]bool, len(parent))
	for _, p := range parent {
		set[p] = true
	}
	var result []string
	for _, p := range defaults {
		if set[p] {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return parent
	}
	return result
}

// SortedKeys returns sorted keys from a trie-node map.
func SortedKeys(m map[string]*TrieNode) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	kit.SortStrings(keys)
	return keys
}
