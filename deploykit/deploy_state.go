package deploykit

import (
	"fmt"
	"maps"
	"os"
	"reflect"
	"strings"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
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
func FleetWalkPreOrder(n *FleetNode, path string, fn func(path string, node *FleetNode) error) error {
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
		if err := FleetWalkPreOrder(n.Children[k], childPath, fn); err != nil {
			return err
		}
	}
	return nil
}

// WalkPostOrder invokes fn on every child (recursively, post-order)
// before invoking fn on this node. Post-order is the delete-order
// semantic: a child must be torn down while its parent environment
// is still alive, so the caller reverses leaves first.
func FleetWalkPostOrder(n *FleetNode, path string, fn func(path string, node *FleetNode) error) error {
	if n == nil {
		return nil
	}
	for _, k := range SortedNestedKeys(n.Children) {
		childPath := k
		if path != "" {
			childPath = path + "." + k
		}
		if err := FleetWalkPostOrder(n.Children[k], childPath, fn); err != nil {
			return err
		}
	}
	return fn(path, n)
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
			"deploy name %q is a tagged registry image ref — a registry ref can't be a deploy name (its dots collide with dotted-path addressing). Give it a short name:\n    charly fleet add <name> %s",
			box, box)
	}
	return nil
}

// FindVmDeployNode finds the FleetNode for a vm-target deploy. It is
// THE shared "which deploy entry backs this VM" lookup used by both
// `charly fleet add` (artifact-env collection) and `charly check live` (tests
// overlay), so the two never diverge. Resolution order:
//  1. by deploy NAME (the entry key) — the precise match;
//  2. by the legacy "vm:<name>" key form;
//  3. by scanning for any target:vm entry whose `vm:` field == vmName (or
//     == name) — the fallback when the caller only knows the vm entity.
//
// Keying by the deploy NAME first is load-bearing: a bed whose key differs
// from its vm entity (e.g. check-k3s-vm -> vm: k3s-vm) is found by its key,
// not mis-resolved via the vm entity name.
//
// RCA #14 (FINAL/K5 unit 6a): the step-3 fallback scan has no uniqueness
// guarantee — with N top-level vm deploys sharing one base template/entity
// (a common shape: several disposable eval beds all `from: eval-vm`), a
// first-wins match is genuinely AMBIGUOUS and, because Go randomizes map
// iteration order per process, can silently return a DIFFERENT (unrelated)
// deploy's node on different runs of the identical lookup — proven live:
// `check-substrate` and `check-builder-vm` (both real top-level vm deploys,
// so their own descendants' hoisted plan steps land on THEIR OWN Plan) vs
// `check-group-vm`/`check-structkind-vm` (both promoted from a peer Member,
// so their Plan was already relocated onto their FORMER parent's root before
// promotion, leaving it empty) all shared `from: eval-vm` — a caller
// resolving an unrelated vm entity could nondeterministically inherit
// check-substrate's own hoisted plan steps, and their embedded venue then
// points at check-substrate's own domain. err is non-nil ONLY when the
// step-3 fallback scan finds 2+ candidates — steps 1-2 are exact-key matches
// and never ambiguous.
func FindVmDeployNode(deploys map[string]FleetNode, name, vmName string) (FleetNode, bool, error) {
	if deploys == nil {
		return FleetNode{}, false, nil
	}
	if name != "" {
		if e, ok := deploys[name]; ok && (e.Target == "vm" || e.From != "") {
			return e, true, nil
		}
		if e, ok := deploys["vm:"+name]; ok {
			return e, true, nil
		}
	}
	var match FleetNode
	var matchKey string
	found := false
	for k, e := range deploys {
		if e.Target == "vm" && e.From != "" && (e.From == vmName || e.From == name) {
			if found {
				return FleetNode{}, false, fmt.Errorf("ambiguous vm deploy lookup for %q (vm %q): both %q and %q declare from %q — the caller must resolve the exact deploy node instead of scanning by entity", name, vmName, matchKey, k, e.From)
			}
			match, matchKey, found = e, k, true
		}
	}
	if found {
		return match, true, nil
	}
	return FleetNode{}, false, nil
}

// FindFleetNode locates a deploy node by key across the WHOLE tree — every
// top-level root, plus its nested Children and peer Members, at any depth.
// Returns nil when no node with that key exists anywhere in the tree. The
// SDK-side twin of ResolveNodePath (which descends a known DOTTED path) and
// FindVmDeployNode (which searches only the top level) — this one searches
// for an unqualified NAME anywhere, needed when the caller only has the
// node's bare key, not its path from a root.
//
// Moved from charly/k3s_post.go (Cutover B unit 5, P13-KERNEL-B): a pure
// FleetNode-tree search with zero loader/registry dependency — the
// scoping-map re-audit found this was the ONLY genuinely movable piece of
// its family; the LoadUnified-coupled orchestration around it (resolving
// which deploy owns a VM entity, reading the persisted port-forward
// allocation) stays host-side, since LoadUnified/materialize is
// K1-permanent core (R-E2).
func FindFleetNode(fleet map[string]FleetNode, name string) *FleetNode {
	for k := range fleet {
		n := fleet[k]
		if k == name {
			return &n
		}
		if r := findFleetNodePtr(n.Children, name); r != nil {
			return r
		}
		if r := findFleetNodePtr(n.Members, name); r != nil {
			return r
		}
	}
	return nil
}

func findFleetNodePtr(m map[string]*FleetNode, name string) *FleetNode {
	for k, n := range m {
		if k == name {
			return n
		}
		if r := findFleetNodePtr(n.Children, name); r != nil {
			return r
		}
		if r := findFleetNodePtr(n.Members, name); r != nil {
			return r
		}
	}
	return nil
}

// IsAutoVmDeployEntry reports whether a VM deploy entry carries NOTHING beyond
// the fields SaveVmDeployState auto-sets — target: vm, vm:, and vm_state. Such
// an entry is a pure runtime-state record (e.g. a disposable check-bed VM) that
// `charly vm destroy` should delete so it doesn't accumulate. Any OTHER non-zero
// field means operator-authored per-host config (preemptible, env, tunnel,
// port, security, …) that MUST survive a destroy→create cycle. Compares against
// the zero node after blanking the three auto-set fields, so a newly-added
// per-host field is covered automatically (no remembered append — same
// drift-proof discipline as MergeFleetNode).
func IsAutoVmDeployEntry(entry FleetNode) bool {
	probe := entry
	probe.VmState = nil
	probe.Target = ""
	probe.From = ""
	probe.Descent = nil // loader-DERIVED (Cutover H), never operator-authored
	return reflect.DeepEqual(probe, FleetNode{})
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

// --- FleetConfig state container (moved from charly/deploy.go, P4) ---
// FleetConfig represents per-machine deployment overrides (~/.config/charly/charly.yml).
// SPIKE (value-type relocation, #55 cluster 2): relocated to spec.FleetConfig
// (spec/spec/fleet_config.go) — every field already resolved to a spec.* type, so
// the type carried zero deploykit-only content. This is now a zero-churn alias;
// the two pure methods (Lookup/LookupKey) moved with it. The three methods that
// reach sdk/kit (DeployedContainerNames/OccupiedHostPorts/GlobalEnvForImage) stay
// below as free functions (fleet_derive.go) — spec can never import sdk/kit.
type FleetConfig = spec.FleetConfig

// MergeDeployConfigs merges multiple DeployConfigs left-to-right. Later
// configs take precedence (field-level replace per image). The merge walks
// every yaml-tagged field of FleetNode via reflect: a field copies
// from src → dst when src's value is non-zero (string != "", slice/map/ptr
// not nil, bool != false, numeric != 0). This makes adding a new field to
// FleetNode automatically merge-correct — the pre-2026-05 hand-rolled
// per-field merge silently dropped 19+ fields (ResolvedPort, Description,
// Secret, Sidecar, Shell, Kubernetes, ForwardGpgAgent, ForwardSshAgent,
// Kind, Replica, Restart, Schedule, Resources, Expose, Storage, Probes,
// Cpus, Ram, DiskSize) whenever any merge → save cycle ran.
//
// The yaml tag `-` (currently only FleetNode.Inside, a derived
// runtime field) skips the merge. Untagged fields are also skipped.
func MergeDeployConfigs(configs ...*FleetConfig) *FleetConfig {
	result := &FleetConfig{Fleet: make(map[string]FleetNode)}
	for _, dc := range configs {
		if dc == nil || dc.Fleet == nil {
			continue
		}
		for name, overlay := range dc.Fleet {
			existing := result.Fleet[name]
			result.Fleet[name] = MergeFleetNode(existing, overlay)
		}
	}
	return result
}

// --- deploy state-model helpers relocated from charly/deploy.go (K5-Unit-1) ---
// These pure helpers moved out of core alongside the load/save/merge/clean/saveDeployState
// bodies: they carry NO core Mechanism dep (only spec types + deploykit's own primitives),
// so they live here unconditionally. The ONE core-coupled op (LoadUnified) reaches deploykit
// through the seam var in deploy_file.go (DeployStateHost), filled by charly at init.

// Named is the interface for provides entries (shared pipeline logic). spec.EnvProvideEntry and
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

// SaveDeployState persists deployment parameters to charly.yml (best-effort). Merges onto any
// existing entry to preserve fields from charly fleet import. Relocated from charly/deploy.go
// (K5-Unit-1); the process-shared flock is a kind-blind kit primitive (kit.AcquireFileLock on
// the deploy-config path) and the loader reaches core through the DeployStateHost seam
// (LoadUnifiedFleetConfig). marshalNode is the deploy-kind-specific node-form serializer the
// caller supplies (the callback SaveFleetConfig invokes per entry).
//
//nolint:gocyclo // field-by-field conditional persist; every branch is a peer (write-when-set)
func SaveDeployState(boxName, instance string, input SaveDeployStateInput, marshalNode func(name string, node *FleetNode) (*yaml.Node, error), read func() (*FleetConfig, error)) {
	// read is the current-state re-read this load-mutate-save performs. A nil read falls back to
	// LoadDeployConfigForWrite — the DeployStateHost-backed host read — so an IN-PROCESS host
	// caller passes nil and behaves exactly as before, INCLUDING the "can't read → don't write"
	// data-safety guard (a nil-read caller with no DeployStateHost registered is not compiled to
	// touch the ledger, so the write is skipped rather than clobbering an unreadable file). A
	// plugin caller (out-of-process command:fleet) injects its OWN loader-backed reader, so
	// SaveDeployState no longer requires the DeployStateHost package var (#55 K4 config-write
	// seam-collapse). NAMED EXIT: the nil-read branch is DI serving the still-in-proc host callers
	// (bed_session / CleanDeployEntry) — NOT a transitional shim; it dies when the last migrates
	// plugin-side in its own deferred cone.
	loadBase := read
	if loadBase == nil {
		if DeployStateHost == nil {
			return
		}
		loadBase = func() (*FleetConfig, error) { return LoadDeployConfigForWrite("saveDeployState") }
	}
	// The lock hold, the fresh re-read inside it, and the nil-config self-heal are
	// MutateFleetConfig's (deploy_config_cycle.go) — THE one locked read-modify-write cycle every
	// overlay writer shares. The write is best-effort, so a lock/read/write failure warns rather
	// than propagating, exactly as this body's own inline lock did before.
	// Thread the same reader into the fail-safe re-check so an out-of-process caller's write
	// path never falls back to the DeployStateHost-backed LoadFleetConfig (nil → host default).
	save := func(dc *FleetConfig) error { return SaveFleetConfig(dc, marshalNode, read) }
	if _, err := MutateFleetConfig(loadBase, save, func(dc *FleetConfig) (bool, error) {
		return applyDeployState(dc, boxName, instance, input), nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save to charly.yml: %v\n", err)
	}
}

// applyDeployState writes input's set fields onto the deploy's entry in a FRESH overlay read under
// the deploy-config lock, reporting whether anything is worth persisting. Split out of
// SaveDeployState so the mutation is a plain function over fresh state — the shape
// MutateFleetConfig requires.
//
//nolint:gocyclo // field-by-field conditional persist; every branch is a peer (write-when-set)
func applyDeployState(dc *FleetConfig, boxName, instance string, input SaveDeployStateInput) bool {
	key := DeployKey(boxName, instance)
	entry := dc.Fleet[key] // preserve existing fields (tunnel, volumes, etc.)
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
	// Defensive zero-write guard: refuse to persist a fully-zero FleetNode (every field at its
	// Go zero value). A future caller invoking SaveDeployState with an empty SaveDeployStateInput
	// on a key that doesn't yet exist would otherwise write `<key>: {}`, materializing an empty
	// entry that masks any matching entry from the project charly.yml deploy block.
	if reflect.DeepEqual(entry, FleetNode{}) {
		return false
	}
	dc.Fleet[key] = entry
	return true
}

// CleanDeployEntry removes an image's entry from charly.yml (best-effort). Also removes global
// service env vars injected by this image. If charly.yml becomes empty after removal, the file is
// deleted. Relocated from charly/deploy.go (K5-Unit-1); the flock is a kind-blind kit primitive
// (kit.AcquireFileLock) and the loader reaches core through the DeployStateHost seam. marshalNode
// is the deploy-kind-specific node-form serializer the caller supplies.
// CleanDeployEntry removes an image's entry from charly.yml (best-effort). Also removes global
// service env vars injected by this image. If charly.yml becomes empty after removal, the file is
// deleted. Relocated from charly/deploy.go (K5-Unit-1); the flock is a kind-blind kit primitive
// (kit.AcquireFileLock) and the loader reaches core through the DeployStateHost seam. marshalNode
// is the deploy-kind-specific node-form serializer the caller supplies.
//
// read is the current-state re-read this load-mutate-save performs. A nil read falls back to the
// DeployStateHost-backed LoadFleetConfig — the in-proc host read — INCLUDING the
// "DeployStateHost==nil → skip" guard (a nil-read caller with no DeployStateHost registered is not
// compiled to touch the ledger, so the clean is skipped rather than clobbering an unreadable
// file). A plugin caller (out-of-process command:pod) injects its OWN loader-backed reader
// (loaderkit.LoadHostFleetConfigViaExecutor), so CleanDeployEntry no longer requires the
// DeployStateHost package var (#55 coneC-dsh — mirrors the SaveFleetConfig/SaveDeployState
// reader-callback precedent). The reader is ALSO threaded as SaveFleetConfig's failsafeRead so the
// data-safety re-check uses the same loader-backed read, not the DeployStateHost-backed one.
func CleanDeployEntry(boxName, instance string, marshalNode func(name string, node *FleetNode) (*yaml.Node, error), read func() (*FleetConfig, error)) {
	loadBase := read
	if loadBase == nil {
		if DeployStateHost == nil {
			return
		}
		loadBase = LoadFleetConfig
	}
	key := DeployKey(boxName, instance)
	save := func(dc *FleetConfig) error { return SaveFleetConfig(dc, marshalNode, read) }
	cleaned := false
	// The lock hold and the fresh re-read inside it are MutateFleetConfig's — the same locked
	// cycle every other overlay writer uses. This clean is the one writer whose mutation may end
	// in REMOVING the file rather than saving it, so that branch reports changed=false (nothing
	// left to persist) after removing it.
	if _, err := MutateFleetConfig(loadBase, save, func(dc *FleetConfig) (bool, error) {
		if !cleanDeployEntryFrom(dc, boxName, instance, key) {
			return false, nil
		}
		cleaned = true
		if len(dc.Fleet) == 0 && dc.Provides == nil {
			if path, pathErr := kit.DefaultDeployConfigPath(); pathErr == nil {
				_ = os.Remove(path)
			}
			return false, nil
		}
		return true, nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not clean charly.yml: %v\n", err)
		return
	}
	if cleaned {
		fmt.Fprintf(os.Stderr, "Cleaned charly.yml entry for %s\n", key)
	}
}

// cleanDeployEntryFrom removes the deploy's entry and any provides it injected from a FRESH overlay
// read under the deploy-config lock, reporting whether it removed anything. Split out of
// CleanDeployEntry so the mutation is a plain function over fresh state — the shape
// MutateFleetConfig requires.
func cleanDeployEntryFrom(dc *FleetConfig, boxName, instance, key string) bool {
	hasImage := false
	if _, ok := dc.Fleet[key]; ok {
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
			for k := range dc.Fleet {
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

	return hasImage || removedProvides
}
