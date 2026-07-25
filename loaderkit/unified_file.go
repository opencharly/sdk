package loaderkit

import (
	"encoding/json"

	"github.com/opencharly/sdk/deploykit"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
)

// unified_file.go — the sdk-importable home of UnifiedFile (K1 keystone, task #24 unit 1):
// relocated from charly/unified.go. UnifiedFile is the typed, in-memory result of loading a
// project's charly.yml (the kind-blind WALK + the registry-coupled MATERIALIZE already run
// through the seams in walk.go/materialize.go — this is the DATA the loader produces, not the
// loading mechanism itself). Every field type is already sdk-portable (spec.*/deploykit.*/plain
// maps), so the type carries NO charly-core dependency of its own.
//
// Boundary (Go forbids methods on a type outside its defining package, so this file draws the
// line precisely): a method stays HERE iff it needs nothing beyond uf's own fields — no provider
// registry, no plugin round-trip, no other charly-core-only helper. A UnifiedFile "accessor" that
// resolves an opaque PluginKinds body via a plugin's OpResolve leg (Distros/Builders/
// resolveResources/resolveAndroids/resolveInits, and the Project*Config wrappers built on them),
// or that reaches the registered CandyScanner/DocParser seams (ProjectCandies/ApplyDiscover), is a
// charly-core FREE FUNCTION taking *UnifiedFile as a parameter instead — see charly/unified.go.
// ResolvePluginKindViaPlugin/DecodePluginKindMap below are the shared, registry-agnostic HALF of
// that resolution (the loop), exported so charly-core's free functions reuse them with an injected
// resolve callback, instead of re-deriving the loop per kind.

// UnifiedFile is the full schema of a single unified-format YAML document. Every field is
// optional — a file with only `distro:` is valid (typical for the embedded build vocabulary); a
// file with only `deploy:` is valid; etc.
type UnifiedFile struct {
	Version string `yaml:"version,omitempty" json:"version,omitempty"`
	// Repo is this project's canonical repo identity (e.g. "github.com/opencharly/charly").
	// Optional; only meaningful on the ROOT file. See repo_identity.go.
	Repo string `yaml:"repo,omitempty" json:"repo,omitempty"`
	// Import is the SINGLE composition statement. A list whose items are either a bare string
	// (flat import into THIS root namespace) or a single-key map `alias: ref` (a namespaced
	// child import). See ImportList (directives.go).
	Import   kit.ImportList     `yaml:"import,omitempty" json:"import,omitempty"`
	Discover kit.DiscoverConfig `yaml:"discover,omitempty" json:"discover,omitempty"`
	// Defaults carries the `defaults:` block (BoxConfig-shaped inheritance defaults).
	Defaults spec.BoxConfig `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	// Box is the generic kind-keyed IMAGE map (P6): name → opaque marshaled spec.BoxConfig.
	Box spec.BoxMap `yaml:"box,omitempty" json:"box,omitempty"`
	// Candy is the generic kind-keyed LAYER map: name → opaque marshaled InlineCandy.
	Candy map[string]json.RawMessage `yaml:"candy,omitempty" json:"candy,omitempty"`
	// Bundle is the flat name-keyed deploy map (the canonical `deploy:` surface).
	Bundle   map[string]spec.BundleNode `yaml:"deploy,omitempty" json:"deploy,omitempty"`
	Provides *deploykit.ProvidesConfig  `yaml:"provides,omitempty" json:"provides,omitempty"`

	// PluginKinds holds entities of KINDS contributed by plugins (a kind the core has no typed
	// map for). NAME-KEYED: kind word → entity NAME → canonical body (opaque JSON). Built-in
	// kinds decode into their typed maps above. Host-internal — never serialized.
	PluginKinds map[string]map[string]json.RawMessage `yaml:"-" json:"-"`

	// Namespaces holds child namespaces mounted by namespaced `import:` entries (alias →
	// fully-resolved isolated UnifiedFile). NOT authored directly — populated by the
	// materialize pass. Entries are referenced qualified, e.g. `base: cachyos.cachyos`.
	Namespaces map[string]*UnifiedFile `yaml:"-"`

	// RootDir is this UnifiedFile's OWN base directory — the dir its root document's SrcDir
	// names. Set once per materialize, for both the top-level project and each mounted
	// namespace. Empty for a project-less / synthetic UnifiedFile.
	RootDir string `yaml:"-"`
}

// InlineCandy is a candy declared inline in the unified file's `candy:` map. Mutually exclusive
// options: `from:` points at a directory to scan (the loader's CandyScanner seam), OR the inline
// body defines the candy (same fields as the candy manifest, flattened via yaml:",inline").
type InlineCandy struct {
	From           string `yaml:"from,omitempty" json:"from,omitempty"`
	spec.CandyYAML `yaml:",inline"`
	// Manifest carries the discovery manifest filename for a `From:` directory so the candy
	// scan reads the right file. Not YAML-authored; carried through the opaque candy-map fold
	// via JSON, hence exported + json-tagged.
	Manifest string `yaml:"-" json:"__manifest,omitempty"`
}

// DecodeInlineCandy decodes one opaque layer body into the InlineCandy loader shape.
func DecodeInlineCandy(raw json.RawMessage) (*InlineCandy, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var il InlineCandy
	if err := json.Unmarshal(raw, &il); err != nil {
		return nil, false
	}
	return &il, true
}

// EncodeInlineCandy marshals a loader InlineCandy into its opaque body.
func EncodeInlineCandy(il *InlineCandy) json.RawMessage {
	raw, err := json.Marshal(il)
	if err != nil {
		// An InlineCandy always marshals (plain struct + generated spec fields); a failure is a
		// programming error.
		panic("EncodeInlineCandy: " + err.Error())
	}
	return raw
}

// BoxConfig decodes the authored image config for name; ok=false when absent.
func (uf *UnifiedFile) BoxConfig(name string) (spec.BoxConfig, bool) {
	return spec.BoxConfigFrom(uf.Box, name)
}

// HasBox reports whether an image named name is present.
func (uf *UnifiedFile) HasBox(name string) bool { _, ok := uf.Box[name]; return ok }

// BoxNames returns the image names, sorted.
func (uf *UnifiedFile) BoxNames() []string { return spec.BoxNamesOf(uf.Box) }

// SetBox stores an authored image config under name (marshaling it opaque).
func (uf *UnifiedFile) SetBox(name string, b spec.BoxConfig) {
	if uf.Box == nil {
		uf.Box = spec.BoxMap{}
	}
	uf.Box[name] = spec.EncodeBox(b)
}

// SetCandy stores a layer under name (marshaling it opaque).
func (uf *UnifiedFile) SetCandy(name string, il *InlineCandy) {
	if uf.Candy == nil {
		uf.Candy = map[string]json.RawMessage{}
	}
	uf.Candy[name] = EncodeInlineCandy(il)
}

// VM/Pod/K8s/Local/Android are DERIVED accessors over uf.PluginKinds[disc] — the 5
// standalone-substrate-TEMPLATE kinds fold into PluginKinds generically (no per-kind-word
// switch). Each returns the opaque name→body map for its kind (nil when none configured); the
// kernel never decodes the bodies itself — consuming PLUGINS decode a body into the concrete
// kind they need.
func (uf *UnifiedFile) VM() map[string]json.RawMessage      { return uf.PluginKinds["vm"] }
func (uf *UnifiedFile) Pod() map[string]json.RawMessage     { return uf.PluginKinds["pod"] }
func (uf *UnifiedFile) K8s() map[string]json.RawMessage     { return uf.PluginKinds["k8s"] }
func (uf *UnifiedFile) Local() map[string]json.RawMessage   { return uf.PluginKinds["local"] }
func (uf *UnifiedFile) Android() map[string]json.RawMessage { return uf.PluginKinds["android"] }

// CheckBeds returns the disposable R10 beds keyed by name. In the unified node-form model a bed
// IS a `disposable: true` bundle, so the bed set is derived directly from the disposable bundles
// in the Bundle map. Members are instruments (brought up alongside a driver), never standalone
// beds. Single enumeration source for `charly check run <bed>` (and the /verify-beds fan-out).
func (uf *UnifiedFile) CheckBeds() map[string]spec.BundleNode {
	if uf == nil {
		return nil
	}
	beds := map[string]spec.BundleNode{}
	for name, node := range uf.Bundle {
		if node.IsDisposable() && node.MemberOf == "" {
			beds[name] = node
		}
	}
	return beds
}

// ProjectConfig returns the *spec.Config equivalent of uf (the box config view).
func (uf *UnifiedFile) ProjectConfig() *spec.Config {
	return uf.projectConfigCached(map[*UnifiedFile]*spec.Config{})
}

// projectConfigCached projects uf (and its import namespaces, recursively) into a *spec.Config.
// The pointer-keyed cache breaks an intentional mutual-import cycle (a shared UnifiedFile node —
// e.g. a namespace two projects both import — is projected exactly once).
func (uf *UnifiedFile) projectConfigCached(cache map[*UnifiedFile]*spec.Config) *spec.Config {
	if c, ok := cache[uf]; ok {
		return c
	}
	images := uf.Box
	if images == nil {
		images = spec.BoxMap{}
	}
	c := &spec.Config{
		Defaults: uf.Defaults,
		Box:      images,
		Local:    uf.Local(),
		Sidecar:  uf.PluginKinds["sidecar"], // opaque bodies; candy/plugin-sidecar resolves them
	}
	cache[uf] = c // cache BEFORE recursing (cycle break)
	if len(uf.Namespaces) > 0 {
		c.Namespaces = make(map[string]*spec.Config, len(uf.Namespaces))
		for ns, sub := range uf.Namespaces {
			c.Namespaces[ns] = sub.projectConfigCached(cache)
		}
	}
	return c
}

// ProjectBundleConfig returns the *deploykit.BundleConfig equivalent (deployments: section of
// the authored file, independent of any per-machine ~/.config/charly/charly.yml, which remains
// loaded separately by deploykit.LoadBundleConfig).
func (uf *UnifiedFile) ProjectBundleConfig() *deploykit.BundleConfig {
	if uf == nil {
		return nil
	}
	sidecars := uf.PluginKinds["sidecar"] // opaque bodies; candy/plugin-sidecar resolves them
	if len(uf.Bundle) == 0 && uf.Provides == nil && len(sidecars) == 0 {
		return nil
	}
	return &deploykit.BundleConfig{
		Provides: uf.Provides,
		Bundle:   uf.Bundle,
		Sidecar:  sidecars,
	}
}

// ResolvePluginKindViaPlugin projects uf.PluginKinds[kind] (opaque bodies) into *T value
// envelopes via resolve, a per-body plugin OpResolve leg — the ONE shared loop every "resolve a
// plugin-kind catalog through its OpResolve leg" accessor uses (charly-core's Distros/Builders/
// resolveResources/resolveAndroids/resolveInits each supply their own registry-coupled resolve
// callback; this loop itself touches no registry). A bad entry is skipped rather than poisoning
// the whole vocabulary.
func ResolvePluginKindViaPlugin[T any](uf *UnifiedFile, kind string, resolve func(json.RawMessage) (*T, error)) map[string]*T {
	if uf == nil {
		return nil
	}
	bodies := uf.PluginKinds[kind]
	if len(bodies) == 0 {
		return nil
	}
	out := make(map[string]*T, len(bodies))
	for name, body := range bodies {
		v, err := resolve(body)
		if err != nil || v == nil {
			continue
		}
		out[name] = v
	}
	return out
}

// DecodePluginKindMap reconstructs the typed name-keyed map[string]*T for a plugin kind from
// uf.PluginKinds[kind] (each body the canonical spec.T JSON the kind plugin's Invoke produced)
// via a PLAIN json.Unmarshal — no plugin OpResolve round-trip, for a kind whose body is already
// self-contained (Builder). Compare ResolvePluginKindViaPlugin above, its sibling for kinds that
// DO need a plugin-side resolve leg. A bad entry is skipped rather than poisoning the whole
// vocabulary. Returns nil when the kind has no entities.
func DecodePluginKindMap[T any](uf *UnifiedFile, kind string) map[string]*T {
	if uf == nil {
		return nil
	}
	bodies := uf.PluginKinds[kind]
	if len(bodies) == 0 {
		return nil
	}
	out := make(map[string]*T, len(bodies))
	for name, body := range bodies {
		var v T
		if err := json.Unmarshal(body, &v); err != nil {
			continue
		}
		out[name] = &v
	}
	return out
}
