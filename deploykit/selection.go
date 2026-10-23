package deploykit

// selection.go — W3.1's ONE selection model. Every consumer that needs to know
// which container engine, VM backend, containerd target or namespace a deploy
// uses calls the resolver for that concept; no consumer reads rt.RunEngine or
// carries a word literal.
//
// PRECEDENCE, identical for every setting (the plan's W3.1 table):
//
//	flag > deploy entity > env > settings file > built-in default
//
// The env/settings fold is NOT re-implemented here: hostenv.ResolveRuntime owns
// it (env > config file > default) and the resolver receives the folded value on
// the runtime argument. The precedence tests drive that fold end-to-end through
// hostenv.ResolveRuntime, so the two levels are covered without a second
// implementation.
//
// `auto` is a RESOLUTION selector, never a provider word. When a level carries
// it, detection runs at that level and the levels below are not consulted.
//
// A candy's `engine:` is a CONSTRAINT, not a precedence level: a resolved engine
// that disagrees with the composition is a validation error, never an override.
//
// The vocabularies are schema-owned: spec.EngineNames / #EngineBinaries /
// #EngineCapability for engines, spec.VmBackends (#VmBackendName) for VM
// backends, spec.RootfulModes (#RootfulMode) for rootful modes. This file
// carries no vocabulary LIST of its own; its one word-keyed table is the
// auto-detection probe table, whose keys are drift-tested against
// spec.VmBackends.

import (
	"fmt"
	"os"
	"os/exec"
	"slices"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/vmshared"
	"github.com/opencharly/spec/container"
	"github.com/opencharly/spec/spec"
)

// autoSelector is the resolution selector shared by every selection: the engine
// and VM-backend provider vocabularies exclude it, and it is the rootful default.
// It is not a provider word: it asks the resolver to detect (or default) at the
// level where it appears.
const autoSelector = "auto"

// rootfulNative is the rootful mode that means "the caller already has rootful
// socket access", so the engine's privilege prefix is not applied. It is a
// settings-contract word (#RootfulMode), not an engine capability fact.
const rootfulNative spec.RootfulMode = "native"

// Selection is the per-deploy input to the resolvers: the deploy entity's
// authored selectors, the composed candy's engine constraint, and the flag-level
// rootful/namespace selectors.
//
// The ENGINE and BACKEND flags are the `flag` argument of ResolvePodEngine /
// ResolveVmBackend. The ROOTFUL and NAMESPACE flags live here because
// ResolveContainerdTarget receives the already-resolved engine and therefore has
// no engine flag; keeping all of that call's flag inputs in one place avoids a
// second signature shape.
type Selection struct {
	// Deploy entity (spec.Deploy) values. Empty means "not set at this level".
	Engine      spec.EngineName
	Backend     spec.VmBackend
	Rootful     spec.RootfulMode
	Namespace   string
	CandyEngine spec.EngineName // the composed candy's engine: constraint

	// Flag-level values for ResolveContainerdTarget.
	RootfulFlag   spec.RootfulMode
	NamespaceFlag string
}

// ContainerdTarget is the resolved engine target: the engine word plus every
// fact a run plan, unit emission or image store needs. The container-engine
// capability row supplies address / namespace / privilege prefix; the
// capability's `namespace` fact is also the namespace's built-in default.
type ContainerdTarget struct {
	Engine          spec.EngineName
	Rootful         bool
	Address         string
	Namespace       string
	PrivilegePrefix string
}

// selectionLevel names one precedence level. It exists so the order is testable:
// the precedence tests permute levels and assert the winner changes.
type selectionLevel string

const (
	levelFlag    selectionLevel = "flag"
	levelDeploy  selectionLevel = "deploy"
	levelRuntime selectionLevel = "runtime"
	levelDefault selectionLevel = "default"
)

// selectionSource is one level's value. An empty value is "not set" and is
// skipped by firstSet.
type selectionSource struct {
	level selectionLevel
	value string
}

// firstSet returns the first non-empty value in SOURCE ORDER. The order is the
// rule; callers build the slice in precedence order and tests mutate it.
func firstSet(sources ...selectionSource) string {
	for _, source := range sources {
		if source.value != "" {
			return source.value
		}
	}
	return ""
}

// detection seams. Production uses the host probes; tests replace them so a
// precedence test does not depend on what is installed on the runner.
var (
	detectPodEngine = container.DetectEngine
	vmBackendProbes = map[spec.VmBackend]func() bool{
		"libvirt": libvirtAvailable,
		"qemu":    qemuAvailable,
	}
)

// podEngineSources is the canonical precedence chain for the engine word. It is
// a function (not an inline slice) so the precedence test asserts the SAME order
// the resolver uses.
func podEngineSources(flag string, deploy Selection, rt *kit.ResolvedRuntime) []selectionSource {
	runtimeEngine := ""
	if rt != nil {
		runtimeEngine = rt.RunEngine
	}
	return []selectionSource{
		{levelFlag, flag},
		{levelDeploy, string(deploy.Engine)},
		{levelRuntime, runtimeEngine},
	}
}

// ResolvePodEngine resolves the container-engine word for a pod deploy.
// Precedence: flag > deploy.Engine > rt.RunEngine (env > settings) > detection.
// `auto` at any level runs detection at that level. A candy engine constraint
// that disagrees with the resolved engine is an error, not an override.
func ResolvePodEngine(flag string, deploy Selection, rt *kit.ResolvedRuntime) (spec.EngineName, error) {
	if deploy.CandyEngine != "" && !container.IsEngineName(string(deploy.CandyEngine)) {
		return "", fmt.Errorf("candy engine %q is not one of %v", deploy.CandyEngine, spec.EngineNames)
	}

	resolved := firstSet(podEngineSources(flag, deploy, rt)...)
	if resolved == "" || resolved == autoSelector {
		detected, err := detectPodEngine()
		if err != nil {
			return "", fmt.Errorf("engine selection: %w", err)
		}
		resolved = detected
	}
	if !container.IsEngineName(resolved) {
		return "", container.EngineValidationError("engine", resolved)
	}
	if deploy.CandyEngine != "" && string(deploy.CandyEngine) != resolved {
		return "", fmt.Errorf(
			"engine constraint conflict: the composed candy requires %q, but the selection resolves %q",
			deploy.CandyEngine, resolved)
	}
	return spec.EngineName(resolved), nil
}

// vmBackendSources is the canonical precedence chain for the VM backend.
func vmBackendSources(flag string, deploy Selection, rt *kit.ResolvedRuntime) []selectionSource {
	runtimeBackend := ""
	if rt != nil {
		runtimeBackend = rt.VmBackend
	}
	return []selectionSource{
		{levelFlag, flag},
		{levelDeploy, string(deploy.Backend)},
		{levelRuntime, runtimeBackend},
	}
}

// ResolveVmBackend resolves the VM-backend word for a vm deploy. Precedence:
// flag > deploy.Backend > rt.VmBackend (env > settings) > detection. `auto`
// detects libvirt then qemu, in the schema's own word order; krun is selected
// explicitly and is never auto-detected.
func ResolveVmBackend(flag string, deploy Selection, rt *kit.ResolvedRuntime) (spec.VmBackend, error) {
	resolved := firstSet(vmBackendSources(flag, deploy, rt)...)
	if resolved == "" || resolved == autoSelector {
		detected, err := detectVmBackend()
		if err != nil {
			return "", fmt.Errorf("vm backend selection: %w", err)
		}
		return spec.VmBackend(detected), nil
	}
	if !slices.Contains(spec.VmBackends, resolved) {
		return "", fmt.Errorf("vm backend %q is not one of %v (or %q)", resolved, spec.VmBackends, autoSelector)
	}
	return spec.VmBackend(resolved), nil
}

// detectVmBackend probes the auto-detectable backends in the schema's word
// order. A backend with no probe (krun) is explicit-only and is skipped, so
// `auto` can never select it.
func detectVmBackend() (string, error) {
	probed := make([]string, 0, len(vmBackendProbes))
	for _, word := range spec.VmBackends {
		probe, ok := vmBackendProbes[spec.VmBackend(word)]
		if !ok {
			continue
		}
		probed = append(probed, word)
		if probe() {
			return word, nil
		}
	}
	return "", fmt.Errorf("no VM backend available (probed %v)", probed)
}

// libvirtAvailable reports whether the libvirt session daemon's socket is
// reachable. Starting the session first is deliberate: a cold stat alone
// false-negatives a fully working libvirt (the vmshared helper documents it).
func libvirtAvailable() bool {
	vmshared.StartLibvirtUserSession()
	picked, _ := vmshared.LibvirtSessionSocketWithProbes()
	_, err := os.Stat(picked)
	return err == nil
}

// qemuAvailable reports whether the architecture-appropriate QEMU binary is on
// PATH.
func qemuAvailable() bool {
	_, err := exec.LookPath(vmshared.QemuSystemBinary())
	return err == nil
}

// containerdRootfulSources is the canonical precedence chain for the rootful
// mode: flag > deploy > rt (env > settings) > the capability default (`auto`).
func containerdRootfulSources(deploy Selection, rt *kit.ResolvedRuntime) []selectionSource {
	runtimeRootful := ""
	if rt != nil {
		runtimeRootful = rt.Rootful
	}
	return []selectionSource{
		{levelFlag, string(deploy.RootfulFlag)},
		{levelDeploy, string(deploy.Rootful)},
		{levelRuntime, runtimeRootful},
	}
}

// containerdNamespaceSources is the canonical precedence chain for the
// containerd namespace. There is no env level (the plan's table has none); the
// runtime value is the settings-file fold, and the capability row is the
// built-in default.
func containerdNamespaceSources(deploy Selection, rt *kit.ResolvedRuntime, capability spec.EngineCapability) []selectionSource {
	runtimeNamespace := ""
	if rt != nil {
		runtimeNamespace = rt.Namespace
	}
	return []selectionSource{
		{levelFlag, deploy.NamespaceFlag},
		{levelDeploy, deploy.Namespace},
		{levelRuntime, runtimeNamespace},
		{levelDefault, capability.Namespace},
	}
}

// ResolveContainerdTarget resolves the engine target a run plan, unit emission,
// image store or image resolver needs. `engine` is the ALREADY-RESOLVED engine
// word (ResolvePodEngine's output). Rootful precedence: flag > deploy > env >
// settings > `auto` (rootless). Namespace precedence: flag > deploy > settings >
// the capability's namespace fact.
func ResolveContainerdTarget(engine spec.EngineName, deploy Selection, rt *kit.ResolvedRuntime) (ContainerdTarget, error) {
	word := string(engine)
	if !container.IsEngineName(word) {
		return ContainerdTarget{}, container.EngineValidationError("engine", word)
	}
	capability, ok := kit.EngineCapabilityFor(word)
	if !ok {
		return ContainerdTarget{}, fmt.Errorf("engine %q has no capability row", word)
	}

	mode := firstSet(containerdRootfulSources(deploy, rt)...)
	if mode == "" {
		mode = autoSelector
	}
	if !slices.Contains(spec.RootfulModes, mode) {
		return ContainerdTarget{}, fmt.Errorf("rootful mode %q is not one of %v", mode, spec.RootfulModes)
	}
	rootful, prefix, err := resolveRootful(spec.RootfulMode(mode), capability)
	if err != nil {
		return ContainerdTarget{}, err
	}

	namespace := firstSet(containerdNamespaceSources(deploy, rt, capability)...)
	if namespace == "" && capability.Namespace != "" {
		return ContainerdTarget{}, fmt.Errorf(
			"engine %q advertises namespace %q but the selection resolved an empty namespace",
			capability.Name, capability.Namespace)
	}

	return ContainerdTarget{
		Engine:          engine,
		Rootful:         rootful,
		Address:         capability.Address,
		Namespace:       namespace,
		PrivilegePrefix: prefix,
	}, nil
}

// resolveRootful maps a rootful mode onto the capability facts. `auto` means
// rootless (the default posture); every concrete mode is an explicit rootful
// request. A rootful request against an engine whose `supports_rootful` is false
// is an error. `native` means the caller already has rootful socket access, so
// no privilege prefix is applied; every other rootful mode uses the engine's
// `privilege_prefix` fact (empty for an engine that needs none).
func resolveRootful(mode spec.RootfulMode, capability spec.EngineCapability) (bool, string, error) {
	if mode == "" || string(mode) == autoSelector {
		return false, "", nil
	}
	if !capability.SupportsRootful {
		return false, "", fmt.Errorf(
			"rootful %q requested, but engine %q does not support rootful launches", mode, capability.Name)
	}
	if mode == rootfulNative {
		return true, "", nil
	}
	return true, capability.PrivilegePrefix, nil
}
