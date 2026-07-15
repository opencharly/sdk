package deploykit

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
	"gopkg.in/yaml.v3"
)

// WalkPreOrder invokes fn on this node, then recurses into every
// child in sorted key order. Pre-order is the add-order semantic: a
// parent's environment must exist before its children can run inside
// it, so the caller applies deploys root-first.
//
// fn receives the full dotted path to each node (e.g. "stack.web.db").
// The root path argument is prepended; callers pass the node's own
// key as `path`.
//
// When fn returns a non-nil error, traversal stops immediately and
// the error propagates.
func BundleWalkPreOrder(n *BundleNode, path string, fn func(path string, node *BundleNode) error) error {
	if n == nil {
		return nil
	}
	if err := fn(path, n); err != nil {
		return err
	}
	for _, k := range SortedNestedKeys(n.Children) {
		childPath := k
		if path != "" {
			childPath = path + "." + k
		}
		if err := BundleWalkPreOrder(n.Children[k], childPath, fn); err != nil {
			return err
		}
	}
	return nil
}

// WalkPostOrder invokes fn on every child (recursively, post-order)
// before invoking fn on this node. Post-order is the delete-order
// semantic: a child must be torn down while its parent environment
// is still alive, so the caller reverses leaves first.
func BundleWalkPostOrder(n *BundleNode, path string, fn func(path string, node *BundleNode) error) error {
	if n == nil {
		return nil
	}
	for _, k := range SortedNestedKeys(n.Children) {
		childPath := k
		if path != "" {
			childPath = path + "." + k
		}
		if err := BundleWalkPostOrder(n.Children[k], childPath, fn); err != nil {
			return err
		}
	}
	return fn(path, n)
}

// ResolveNodePath walks roots[path0].Children[path1]...[pathN] and
// returns the targeted node plus its parent chain (root-first,
// excluding the target itself). Returns a descriptive error when any
// path segment is missing so the user sees which segment doesn't
// exist.
//
// An empty path is invalid — callers dispatch to
// WalkPreOrder/WalkPostOrder against a named root instead of
// resolving "".
func ResolveNodePath(roots map[string]BundleNode, path string) (*BundleNode, []*BundleNode, error) {
	parts := SplitDottedPath(path)
	if len(parts) == 0 {
		return nil, nil, fmt.Errorf("empty or malformed deployment path %q", path)
	}
	rootName := parts[0]
	rootEntry, ok := roots[rootName]
	if !ok {
		return nil, nil, fmt.Errorf("no deployment named %q", rootName)
	}
	current := &rootEntry
	var ancestors []*BundleNode
	for i := 1; i < len(parts); i++ {
		ancestors = append(ancestors, current)
		next, ok := current.Children[parts[i]]
		if !ok {
			prefix := strings.Join(parts[:i], ".")
			return nil, nil, fmt.Errorf("no child %q under %q", parts[i], prefix)
		}
		current = next
	}
	return current, ancestors, nil
}

// SplitDottedPath splits a dotted deployment path into segments. An
// empty input or a path with any empty segment (leading/trailing/
// doubled dots) yields nil so callers can flag the error at their
// layer with the original offending path string.
func SplitDottedPath(path string) []string {
	if path == "" {
		return nil
	}
	out := strings.Split(path, ".")
	if slices.Contains(out, "") {
		return nil
	}
	return out
}

// deployNodeVenue returns a node's stamped descent VENUE trait (P9) nil-safely — the deploykit
// analogue of charly's nodeTraits for the SDK-side consult sites, which cannot reach charly's
// provider registry to resolve traits for an unstamped node. A node with no stamped descent
// yields "" (the external-in-place default), so a caller that also checks a positive fallback
// (e.g. `|| e.From != ""`) preserves the former word-switch behaviour.
func deployNodeVenue(n *BundleNode) string {
	if n != nil && n.Descent != nil {
		return n.Descent.Venue
	}
	return ""
}

// SortedNestedKeys returns the keys of a children map in deterministic
// order so traversal produces stable output across runs.
// BedCheckLiveRefs returns the ordered `charly check live` targets for a bed: the
// substrate itself first, then each nested child as a sorted dotted path. This
// is the pure list `charly check run` walks so a nested pod's BAKED candy/box check
// (e.g. the selkies candy's encoder + frame checks on a nested selkies-kde pod)
// is exercised against its real venue through the chain — not just the parent
// substrate. Without the nested entries, `charly check run` deploys nested children
// but never evaluates them. Pure + unit-tested.
func BedCheckLiveRefs(name string, children map[string]*BundleNode) []string {
	refs := []string{name}
	for _, k := range SortedNestedKeys(children) {
		// A nested child gets its own `charly check live <parent>.<child>` hop ONLY
		// if it is an independently-resolvable venue (a pod/vm/local child with
		// its own image/host that the chain can reach). A `target: android`
		// child shares the parent pod's venue and has NO own image — its
		// app-presence checks are baked into the parent's android-emulator-layer
		// and already run in the parent ref. `charly check live` has no android
		// dotted-path branch, so a hop for it would wrongly resolve to a
		// non-existent `charly-<parent>.device` container. Skip android children.
		if c := children[k]; c != nil && deployNodeVenue(c) == "parent" { // android (parent venue)
			continue
		}
		refs = append(refs, name+"."+k)
	}
	return refs
}

// EffectiveStop returns the configured stop mechanism with the default.
func PreemptEffectiveStop(p *PreemptibleConfig) string {
	if p == nil || p.Stop == "" {
		return PreemptStopShutdown
	}
	return p.Stop
}

// EffectiveRestore returns the configured restore policy with the default.
func PreemptEffectiveRestore(p *PreemptibleConfig) string {
	if p == nil || p.Restore == "" {
		return PreemptRestoreAlways
	}
	return p.Restore
}

// ApplyTo merges install_opts settings into an EmitOpts. CLI flags
// still win — charly.yml provides defaults, not overrides. Nil
// receiver is a no-op.
func InstallOptsApplyTo(o *InstallOptsConfig, opts EmitOpts) EmitOpts {
	if o == nil {
		return opts
	}
	if !opts.WithServices {
		opts.WithServices = o.WithServices
	}
	if !opts.AllowRepoChanges {
		opts.AllowRepoChanges = o.AllowRepoChanges
	}
	if !opts.AllowRootTasks {
		opts.AllowRootTasks = o.AllowRootTasks
	}
	if !opts.SkipIncompatible {
		opts.SkipIncompatible = o.SkipIncompatible
	}
	if !opts.Verify {
		opts.Verify = o.Verify
	}
	if opts.BuilderImageOverride == "" {
		opts.BuilderImageOverride = o.BuilderImage
	}
	return opts
}

// DeployKey returns the charly.yml map key for an image, optionally qualified by instance.
// Base images use just the image name; instances use "image/instance".
func DeployKey(boxName, instance string) string {
	if instance == "" {
		return boxName
	}
	return boxName + "/" + instance
}

// CanonicalizeDeployArg splits Pattern A "<base>/<instance>" CLI positional
// args into their component (image, instance) pair. Idempotent: if the input
// is already split (instance != "") or contains no slash, returns as-is.
// Pattern B (FQ ref containing "/") is identified by presence of "@" or ":"
// suffix on the leftmost segment OR a registry-host pattern (contains "."
// before the first "/") and passed through untouched.
//
// MUST be called at the top of every CLI verb that takes a positional
// deploy-arg (`charly config`, `charly start`, `charly stop`, `charly shell`, `charly logs`,
// `charly update`, `charly status`, `charly remove`) — before any downstream code reads
// c.Image or c.Instance. Without this, Pattern A instance deploys leak
// past the canonicalization boundary and downstream code conflates the
// deploy key with the image short-name (see Bug 2/3 RCA notes —
// MergeDeployOntoMetadata composes wrong key, port/env overlays drop).
func CanonicalizeDeployArg(arg, instance string) (box, inst string) {
	if instance != "" || arg == "" {
		return arg, instance
	}
	if !strings.Contains(arg, "/") {
		return arg, ""
	}
	// Registry-qualified ref (Pattern B): contains "." in the first segment
	// (registry host like ghcr.io) or "@" anywhere (digest pin) or the
	// trailing segment carries ":tag". Pass through.
	first := arg
	if before, _, ok := strings.Cut(arg, "/"); ok {
		first = before
	}
	if strings.Contains(first, ".") || strings.Contains(arg, "@") || ArgHasImageTag(arg) {
		return arg, ""
	}
	return ParseDeployKey(arg)
}

// ArgHasImageTag reports whether arg's trailing path segment carries a ":tag" — the marker of a
// registry IMAGE ref (ghcr.io/org/image:tag), as opposed to a github REPO ref (which pins with
// @version) or a dotted-path deploy address. Shared by CanonicalizeDeployArg + the
// deploy-name guard (R3).
func ArgHasImageTag(arg string) bool {
	i := strings.LastIndex(arg, "/")
	if i < 0 {
		return false
	}
	return strings.Contains(arg[i:], ":")
}

// RejectImageRefAsDeployName fails a deploy-CREATION command (config setup / start) whose
// positional is a tagged registry image ref used AS the deploy NAME. The ref's registry-host dots
// make an invalid charly.yml deploy key (dots are reserved for dotted-path addressing), so the
// deploy would save and the NEXT config load would hard-fail (the 2026-07
// `charly config ghcr.io/…:tag` corruption). A registry image needs an explicit short deploy
// name. Gated on BOTH a dot (invalid key) AND an image `:tag` (so a github repo ref, which pins
// with @version, and a bare dotted-path address are untouched).
func RejectImageRefAsDeployName(box string) error {
	if strings.Contains(box, ".") && ArgHasImageTag(box) {
		return fmt.Errorf(
			"deploy name %q is a tagged registry image ref — a registry ref can't be a deploy name (its dots collide with dotted-path addressing). Give it a short name:\n    charly bundle add <name> %s",
			box, box)
	}
	return nil
}

// ParseDeployKey splits a charly.yml map key back into image name and instance.
// "selkies-desktop" → ("selkies-desktop", "")
// "selkies-desktop/foo" → ("selkies-desktop", "foo")
func ParseDeployKey(key string) (boxName, instance string) {
	if before, after, ok := strings.Cut(key, "/"); ok {
		return before, after
	}
	return key, ""
}

// FindVmDeployNode finds the BundleNode for a vm-target deploy. It is
// THE shared "which deploy entry backs this VM" lookup used by both
// `charly bundle add` (artifact-env collection) and `charly check live` (tests
// overlay), so the two never diverge. Resolution order:
//  1. by deploy NAME (the entry key) — the precise match;
//  2. by the legacy "vm:<name>" key form;
//  3. by scanning for any target:vm entry whose `vm:` field == vmName (or
//     == name) — the fallback when the caller only knows the vm entity.
//
// Keying by the deploy NAME first is load-bearing: a bed whose key differs
// from its vm entity (e.g. check-k3s-vm -> vm: k3s-vm) is found by its key,
// not mis-resolved via the vm entity name.
func FindVmDeployNode(deploys map[string]BundleNode, name, vmName string) (BundleNode, bool) {
	if deploys == nil {
		return BundleNode{}, false
	}
	if name != "" {
		if e, ok := deploys[name]; ok && (e.Target == "vm" || e.From != "") {
			return e, true
		}
		if e, ok := deploys["vm:"+name]; ok {
			return e, true
		}
	}
	for _, e := range deploys {
		if e.Target == "vm" && e.From != "" && (e.From == vmName || e.From == name) {
			return e, true
		}
	}
	return BundleNode{}, false
}

// DropMappingKey removes a key (and its value) from a YAML mapping node in place.
func DropMappingKey(m *yaml.Node, key string) {
	if m == nil || m.Kind != yaml.MappingNode {
		return
	}
	kept := make([]*yaml.Node, 0, len(m.Content))
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			continue
		}
		kept = append(kept, m.Content[i], m.Content[i+1])
	}
	m.Content = kept
}

// MergeBundleNode applies non-zero fields from `src` onto `dst` and
// returns the merged copy. Walks every yaml-tagged field via reflect; the
// yaml `-` tag (derived/runtime-only fields) is skipped. Same precedence
// rule as the underlying merge: src non-zero wins, otherwise dst passes
// through. Per R3 the single helper replaces the hand-rolled per-field
// merges that previously lived in MergeDeployConfigs (drift-prone — every
// new struct field needed a remembered append, and 19+ were missed).
func MergeBundleNode(dst, src BundleNode) BundleNode {
	dstV := reflect.ValueOf(&dst).Elem()
	srcV := reflect.ValueOf(src)
	t := dstV.Type()
	for i := 0; i < t.NumField(); i++ {
		ft := t.Field(i)
		tag := ft.Tag.Get("yaml")
		// Skip derived fields (yaml:"-") and untagged fields (rare; not
		// part of the persisted schema, so not merge-relevant).
		if tag == "-" || tag == "" {
			continue
		}
		sv := srcV.Field(i)
		if sv.IsZero() {
			continue
		}
		dstV.Field(i).Set(sv)
	}
	// Children/Members/Target are loader-DERIVED (yaml:"-" — not authored) yet are
	// real TREE DATA that must merge across project + per-host overlay. The reflect
	// loop above skips yaml:"-" fields (intended for the genuinely runtime-only
	// MemberOf/Inside/venue), so merge the structural tree fields EXPLICITLY here:
	// src non-zero wins, else dst passes through (same precedence). Without this a
	// project's nested/peer tree + target is dropped on the empty→project merge AND
	// by a nestedless operator overlay (resolveTreeRoot → MergeDeployConfigs).
	if src.Target != "" {
		dst.Target = src.Target
	}
	if len(src.Children) > 0 {
		dst.Children = src.Children
	}
	if len(src.Members) > 0 {
		dst.Members = src.Members
	}
	return dst
}

// IsAutoVmDeployEntry reports whether a VM deploy entry carries NOTHING beyond
// the fields saveVmDeployState auto-sets — target: vm, vm:, and vm_state. Such
// an entry is a pure runtime-state record (e.g. a disposable check-bed VM) that
// `charly vm destroy` should delete so it doesn't accumulate. Any OTHER non-zero
// field means operator-authored per-host config (preemptible, env, tunnel,
// port, security, …) that MUST survive a destroy→create cycle. Compares against
// the zero node after blanking the three auto-set fields, so a newly-added
// per-host field is covered automatically (no remembered append — same
// drift-proof discipline as MergeBundleNode).
func IsAutoVmDeployEntry(entry BundleNode) bool {
	probe := entry
	probe.VmState = nil
	probe.Target = ""
	probe.From = ""
	probe.Descent = nil // loader-DERIVED (Cutover H), never operator-authored
	return reflect.DeepEqual(probe, BundleNode{})
}

// AppendOrReplaceEnv adds or replaces an env var entry (KEY=VALUE) in a slice.
// If the key already exists, the value is replaced in-place.
func AppendOrReplaceEnv(envs []string, entry string) []string {
	key := EnvKey(entry)
	for i, e := range envs {
		if EnvKey(e) == key {
			envs[i] = entry
			return envs
		}
	}
	return append(envs, entry)
}

// EnvKey extracts the KEY part from a KEY=VALUE string.
func EnvKey(entry string) string {
	if before, _, ok := strings.Cut(entry, "="); ok {
		return before
	}
	return entry
}

// StripSecretEnvNames removes any KEY=VAL entries from env whose KEY is in
// the blocked list. The blocked list is expected to be short (one entry per
// secret_* declaration on the image), so a per-entry delete is fine.
func StripSecretEnvNames(env map[string]string, blocked []string) map[string]string {
	if len(env) == 0 || len(blocked) == 0 {
		return env
	}
	out := make(map[string]string, len(env))
	maps.Copy(out, env)
	for _, name := range blocked {
		delete(out, name)
	}
	return out
}

// MergeEnvVars merges new env vars into existing ones (upsert by key).
// New vars override existing vars with the same key.
func MergeEnvVars(existing, newVars map[string]string) map[string]string {
	out := make(map[string]string, len(existing)+len(newVars))
	maps.Copy(out, existing)
	maps.Copy(out, newVars)
	return out
}

// --- BundleConfig state container (moved from charly/deploy.go, P4) ---
// BundleConfig represents per-machine deployment overrides (~/.config/charly/charly.yml).
// Only runtime/deployment fields are supported — build-time fields are structurally excluded.
//
// Schema v4: the top-level map key is `deployment:` (singular, flat). The
// legacy `images:` / `deployments.images.*` nesting is gone — all target
// kinds (host / vm / pod / k8s) live under the single `deployment:` map.
type BundleConfig struct {
	Provides *ProvidesConfig       `yaml:"provides,omitempty" json:"provides,omitempty"`
	Bundle   map[string]BundleNode `yaml:"deploy" json:"deploy"`
	// Sidecar carries the project's sidecar-template library as OPAQUE bodies
	// (the raw PluginKinds["sidecar"] map). candy/plugin-sidecar's OpResolve merges
	// these UNDER each deploy node's own overrides; the kernel reads no fields
	// (the sidecar de-type, Cutover D).
	Sidecar map[string]json.RawMessage `yaml:"sidecar,omitempty" json:"sidecar,omitempty"`
}

// OccupiedHostPorts returns the set of host ports already published by
// any deployment in dc except the named one (`excludeKey` is typically
// the deploy key for the entry currently being expanded — we want to
// allow it to keep its old allocations, not avoid them). Used by
// ResolveDeployPorts to keep auto-allocations from colliding across deploys.

// Lookup returns the BundleNode for (deployName, instance), or
// (zero, false) when the entry is absent. Safe to call on a nil
// *BundleConfig — lets callers chain
// `loadDeployConfigForRead(...).Lookup(deployName, instance)` without a
// separate nil check. deployName is the charly.yml key base the caller is
// operating on (typically c.Image), NOT the baked image short-name — for a
// kind:check bed or Pattern-B deploy the two differ. Pass the deploy key, never
// a value derived from an image label (see MergeDeployOntoMetadata).
func (dc *BundleConfig) Lookup(deployName, instance string) (BundleNode, bool) {
	if dc == nil {
		return BundleNode{}, false
	}
	entry, ok := dc.Bundle[DeployKey(deployName, instance)]
	return entry, ok
}

// LookupKey looks up a deploy entry by its full charly.yml key (e.g.
// "foo", "foo/instance", "vm:name"). Safe on nil receiver.
func (dc *BundleConfig) LookupKey(key string) (BundleNode, bool) {
	if dc == nil {
		return BundleNode{}, false
	}
	entry, ok := dc.Bundle[key]
	return entry, ok
}

// MergeDeployConfigs merges multiple DeployConfigs left-to-right. Later
// configs take precedence (field-level replace per image). The merge walks
// every yaml-tagged field of BundleNode via reflect: a field copies
// from src → dst when src's value is non-zero (string != "", slice/map/ptr
// not nil, bool != false, numeric != 0). This makes adding a new field to
// BundleNode automatically merge-correct — the pre-2026-05 hand-rolled
// per-field merge silently dropped 19+ fields (ResolvedPort, Description,
// Secret, Sidecar, Shell, Kubernetes, ForwardGpgAgent, ForwardSshAgent,
// Kind, Replica, Restart, Schedule, Resources, Expose, Storage, Probes,
// Cpus, Ram, DiskSize) whenever any merge → save cycle ran.
//
// The yaml tag `-` (currently only BundleNode.Inside, a derived
// runtime field) skips the merge. Untagged fields are also skipped.
func MergeDeployConfigs(configs ...*BundleConfig) *BundleConfig {
	result := &BundleConfig{Bundle: make(map[string]BundleNode)}
	for _, dc := range configs {
		if dc == nil || dc.Bundle == nil {
			continue
		}
		for name, overlay := range dc.Bundle {
			existing := result.Bundle[name]
			result.Bundle[name] = MergeBundleNode(existing, overlay)
		}
	}
	return result
}

// --- deploy state-model helpers relocated from charly/deploy.go (K5-Unit-1) ---
// These pure helpers moved out of core alongside the load/save/merge/clean/saveDeployState
// bodies: they carry NO core Mechanism dep (only spec types + deploykit's own primitives),
// so they live here unconditionally. The ONE core-coupled op (LoadUnified) reaches deploykit
// through the seam var in deploy_file.go (DeployStateHost), filled by charly at init.

// Named is the interface for provides entries (shared pipeline logic). EnvProvideEntry and
// MCPProvideEntry both satisfy it via their GetName/GetSource methods. Relocated from
// charly/provides.go so RemoveBySource/RemoveByExactSource (used by CleanDeployEntry) stay in
// the same package as their caller.
type Named interface {
	GetName() string
	GetSource() string
}

// IsSameBaseBox returns true if source is the same base image (with or without instance).
// Relocated from charly/deploy.go (used by RemoveBySource).
func IsSameBaseBox(source, boxName string) bool {
	return source == boxName || strings.HasPrefix(source, boxName+"/")
}

// RemoveBySource removes all entries injected by the given image (same base, any instance).
// Returns the filtered list and whether anything was removed. Relocated from
// charly/provides.go.
func RemoveBySource[T Named](entries []T, boxName string) ([]T, bool) {
	var result []T
	removed := false
	for _, e := range entries {
		if IsSameBaseBox(e.GetSource(), boxName) {
			removed = true
		} else {
			result = append(result, e)
		}
	}
	return result, removed
}

// RemoveByExactSource removes entries whose source matches the exact deploy key (no
// cross-instance match). Relocated from charly/provides.go.
func RemoveByExactSource[T Named](entries []T, source string) ([]T, bool) {
	var result []T
	removed := false
	for _, e := range entries {
		if e.GetSource() == source {
			removed = true
		} else {
			result = append(result, e)
		}
	}
	return result, removed
}

// DescriptionInfo returns the human-facing summary: the FIRST line of the plain-string
// description (multi-line prose lives in the rest of the string). Relocated from
// charly/generate.go so MergeDeployOntoMetadata (deploy_file.go) reads it without a core dep.
func DescriptionInfo(d string) string {
	d = strings.TrimSpace(d)
	if d == "" {
		return ""
	}
	if before, _, ok := strings.Cut(d, "\n"); ok {
		return strings.TrimSpace(before)
	}
	return d
}

// ScopeVolumesToDeployKey renames meta's named-volume mounts from the image-derived prefix
// (charly-<image>-) to the deploy's own prefix (DeployVolumePrefix), so every distinctly-named
// deploy ALWAYS gets volume mounts distinct from any other deploy of the same image. No-op
// when the deploy's prefix already equals the image prefix (the common `charly config <image>`
// base deploy). Idempotent. Relocated from charly/deploy.go (reads *spec.BoxMetadata, no core
// dep — folds cleanly into the kit).
func ScopeVolumesToDeployKey(meta *spec.BoxMetadata, deployName, instance string) {
	if meta == nil || deployName == "" {
		return
	}
	newPrefix := DeployVolumePrefix(deployName, instance)
	oldPrefix := "charly-" + meta.Box + "-"
	if newPrefix == oldPrefix {
		return
	}
	for i := range meta.Volume {
		if rest, ok := strings.CutPrefix(meta.Volume[i].VolumeName, oldPrefix); ok {
			meta.Volume[i].VolumeName = newPrefix + rest
		}
	}
}

// SaveDeployStateInput holds the deployment parameters to persist. Relocated from
// charly/deploy.go (K5-Unit-1). Every field type is a spec/vmshared type the SDK already
// carries — no core Mechanism dep.
type SaveDeployStateInput struct {
	Ports []string
	// SetPorts gates whether Ports is written to charly.yml at all. This ensures
	// `charly config <name>` (without --port flags) and `charly update <name>` no
	// longer silently overwrite operator port overrides with image-label defaults.
	SetPorts bool
	Env      map[string]string
	CleanEnv bool // true = replace env map; false = merge (upsert by key)
	EnvFile  string
	Network  string
	Security *spec.Security
	Volume   []spec.DeployVolume
	Sidecar  map[string]json.RawMessage
	Tunnel   *spec.TunnelYAML

	// SecretNames lists env var names declared as secret_accepts / secret_requires on the
	// image. saveDeployState uses this list to defensively strip any matching KEY=VAL entries
	// from both the input Env and the existing persisted entry.Env before writing. Defense in
	// depth for the §6 / Run() pipeline (MigratePlaintextEnvSecret and scrubSecretCLIEnv are
	// the primary gates). Populated by the Run() call site from meta.SecretAccept/SecretRequires.
	SecretNames []string

	// Disposable + Lifecycle — the classification fields (see /charly-internals:disposable).
	SetDisposable bool
	Disposable    bool
	SetLifecycle  bool
	Lifecycle     string

	// Box + Target — the schema-required fields per the 2026-05-12 require-image cutover.
	// Written when non-empty AND when the existing entry doesn't already have a value (don't
	// clobber operator-authored refs on re-config).
	Box    string
	Target string

	// ResolvedImage is the concrete overlay image ref produced by a pod deploy's add_candy:
	// overlay build, persisted by PrepareVenue so config/start deploy EXACTLY that overlay.
	ResolvedImage string

	// VmState + VmCrossRef — the vm substrate's persisted runtime state (instance-id, ssh_port,
	// disk path) + the kind:vm cross-ref, shipped by the externalized vm plugin's PrepareVenue
	// reply as the generic State patch. VmState is written whenever non-nil; VmCrossRef seeds
	// entry.From only when unset.
	VmState    *spec.VmDeployState
	VmCrossRef string

	// Resource-arbitration axis: the holder-side Preemptible block + the claimant-side
	// RequiresExclusive / RequiresShared token lists. Persisted so a deploy/bed MEMBER
	// round-trips its arbiter role through the per-host overlay. Written when non-empty.
	Preemptible       *spec.PreemptibleConfig
	RequiresExclusive []string
	RequiresShared    []string
}

// SaveDeployState persists deployment parameters to charly.yml (best-effort). Merges onto any
// existing entry to preserve fields from charly bundle import. Relocated from charly/deploy.go
// (K5-Unit-1); the process-shared flock is a kind-blind kit primitive (kit.AcquireFileLock on
// the deploy-config path) and the loader reaches core through the DeployStateHost seam
// (LoadUnifiedBundleConfig). marshalNode is the deploy-kind-specific node-form serializer the
// caller supplies (the callback SaveBundleConfig invokes per entry).
//
//nolint:gocyclo // field-by-field conditional persist; every branch is a peer (write-when-set)
func SaveDeployState(boxName, instance string, input SaveDeployStateInput, marshalNode func(name string, node *BundleNode) (*yaml.Node, error)) {
	if DeployStateHost == nil {
		return
	}
	path, pathErr := kit.DefaultDeployConfigPath()
	if pathErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not resolve charly.yml path: %v\n", pathErr)
		return
	}
	unlock, lockErr := kit.AcquireFileLock(path+".lock", true)
	if lockErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not lock charly.yml for write: %v\n", lockErr)
		return
	}
	defer func() { _ = unlock() }()
	dc, err := LoadDeployConfigForWrite("saveDeployState")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save to charly.yml: %v\n", err)
		return
	}
	key := DeployKey(boxName, instance)
	entry := dc.Bundle[key] // preserve existing fields (tunnel, volumes, etc.)
	if input.Box != "" && entry.Image == "" {
		entry.Image = input.Box
	}
	if input.Target != "" && entry.Target == "" {
		entry.Target = input.Target
	}
	// ResolvedImage: the latest overlay build wins (clobber). Only PrepareVenue sets it.
	if input.ResolvedImage != "" {
		entry.ResolvedImage = input.ResolvedImage
	}
	// Vm runtime state: write whenever non-nil; seed the kind:vm cross-ref only when unset.
	if input.VmState != nil {
		entry.VmState = input.VmState
	}
	if input.VmCrossRef != "" && entry.From == "" {
		entry.From = input.VmCrossRef
	}
	if input.Volume != nil {
		entry.Volume = input.Volume
	}
	// Ports gated on SetPorts: explicit opt-in required so a recompute path that always-passes
	// computed `meta.Port` doesn't silently overwrite operator overrides.
	if input.SetPorts && input.Ports != nil {
		entry.Port = input.Ports
	}
	// Defensive scrub: drop credential-backed env vars from both input and existing entry.
	if len(input.SecretNames) > 0 {
		input.Env = StripSecretEnvNames(input.Env, input.SecretNames)
		entry.Env = StripSecretEnvNames(entry.Env, input.SecretNames)
	}
	if len(input.Env) > 0 {
		if input.CleanEnv || len(entry.Env) == 0 {
			entry.Env = input.Env
		} else {
			entry.Env = MergeEnvVars(entry.Env, input.Env)
		}
	}
	if input.EnvFile != "" {
		entry.EnvFile = input.EnvFile
	}
	if input.Network != "" {
		entry.Network = input.Network
	}
	if input.Security != nil {
		entry.Security = input.Security
	}
	if len(input.Sidecar) > 0 {
		entry.Sidecar = input.Sidecar
	}
	if input.Tunnel != nil {
		entry.Tunnel = input.Tunnel
	}
	// Classification fields: only written when the caller explicitly opts in via SetDisposable
	// / SetLifecycle. This lets repeated SaveDeployState calls from unrelated code paths leave
	// a user-authored `disposable: true` in place.
	if input.SetDisposable {
		v := input.Disposable
		entry.Disposable = &v
	}
	if input.SetLifecycle {
		entry.Lifecycle = input.Lifecycle
	}
	// Resource-arbitration axis: persist the holder-side preemptible block + the claimant-side
	// requires_exclusive / requires_shared token lists. Write-when-non-empty (Volume/Tunnel
	// idiom): an unset field never clobbers a previously-seeded role on a re-config.
	if input.Preemptible != nil {
		entry.Preemptible = input.Preemptible
	}
	if len(input.RequiresExclusive) > 0 {
		entry.RequiresExclusive = input.RequiresExclusive
	}
	if len(input.RequiresShared) > 0 {
		entry.RequiresShared = input.RequiresShared
	}
	// Defensive zero-write guard: refuse to persist a fully-zero BundleNode (every field at its
	// Go zero value). A future caller invoking SaveDeployState with an empty SaveDeployStateInput
	// on a key that doesn't yet exist would otherwise write `<key>: {}`, materializing an empty
	// entry that masks any matching entry from the project charly.yml deploy block.
	if reflect.DeepEqual(entry, BundleNode{}) {
		return
	}
	dc.Bundle[key] = entry
	if err := SaveBundleConfig(dc, marshalNode); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save to charly.yml: %v\n", err)
	}
}

// CleanDeployEntry removes an image's entry from charly.yml (best-effort). Also removes global
// service env vars injected by this image. If charly.yml becomes empty after removal, the file is
// deleted. Relocated from charly/deploy.go (K5-Unit-1); the flock is a kind-blind kit primitive
// (kit.AcquireFileLock) and the loader reaches core through the DeployStateHost seam. marshalNode
// is the deploy-kind-specific node-form serializer the caller supplies.
func CleanDeployEntry(boxName, instance string, marshalNode func(name string, node *BundleNode) (*yaml.Node, error)) {
	if DeployStateHost == nil {
		return
	}
	path, pathErr := kit.DefaultDeployConfigPath()
	if pathErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not resolve charly.yml path: %v\n", pathErr)
		return
	}
	unlock, lockErr := kit.AcquireFileLock(path+".lock", true)
	if lockErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not lock charly.yml for clean: %v\n", lockErr)
		return
	}
	defer func() { _ = unlock() }()
	dc, err := LoadBundleConfig()
	if err != nil || dc == nil {
		return
	}

	key := DeployKey(boxName, instance)
	hasImage := false
	if _, ok := dc.Bundle[key]; ok {
		hasImage = true
		RemoveBoxDeploy(dc, key)
	}

	// Remove provides entries injected by this image/instance.
	removedProvides := false
	if dc.Provides != nil {
		if instance != "" {
			// Instance removal: remove only this instance's provides (exact source match)
			if len(dc.Provides.Env) > 0 {
				cleaned, removed := RemoveByExactSource(dc.Provides.Env, key)
				if removed {
					dc.Provides.Env = cleaned
					removedProvides = true
					fmt.Fprintf(os.Stderr, "Removed env provides from %s\n", key)
				}
			}
			if len(dc.Provides.MCP) > 0 {
				cleaned, removed := RemoveByExactSource(dc.Provides.MCP, key)
				if removed {
					dc.Provides.MCP = cleaned
					removedProvides = true
					fmt.Fprintf(os.Stderr, "Removed MCP provides from %s\n", key)
				}
			}
		} else {
			// Base image removal: only remove if no other entries for the same base image remain
			hasOtherEntries := false
			for k := range dc.Bundle {
				base, _ := ParseDeployKey(k)
				if base == boxName {
					hasOtherEntries = true
					break
				}
			}
			if !hasOtherEntries {
				if len(dc.Provides.Env) > 0 {
					cleaned, removed := RemoveBySource(dc.Provides.Env, boxName)
					if removed {
						dc.Provides.Env = cleaned
						removedProvides = true
						fmt.Fprintf(os.Stderr, "Removed env provides from %s\n", boxName)
					}
				}
				if len(dc.Provides.MCP) > 0 {
					cleaned, removed := RemoveBySource(dc.Provides.MCP, boxName)
					if removed {
						dc.Provides.MCP = cleaned
						removedProvides = true
						fmt.Fprintf(os.Stderr, "Removed MCP provides from %s\n", boxName)
					}
				}
			}
		}
		if len(dc.Provides.MCP) == 0 && len(dc.Provides.Env) == 0 {
			dc.Provides = nil
		}
	}

	if !hasImage && !removedProvides {
		return
	}

	if len(dc.Bundle) == 0 && dc.Provides == nil {
		if path, pathErr := kit.DefaultDeployConfigPath(); pathErr == nil {
			_ = os.Remove(path)
		}
	} else if err := SaveBundleConfig(dc, marshalNode); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not clean charly.yml: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stderr, "Cleaned charly.yml entry for %s\n", key)
}
