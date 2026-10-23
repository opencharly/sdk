package deploykit

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/hostenv"
	"github.com/opencharly/spec/spec"
)

// stubEngineDetection makes the detection fallback deterministic.
func stubEngineDetection(t *testing.T, engine string, err error) {
	t.Helper()
	orig := detectPodEngine
	detectPodEngine = func() (string, error) { return engine, err }
	t.Cleanup(func() { detectPodEngine = orig })
}

// stubVmProbes replaces the auto-detectable backend probes. Every word in the
// map is present; a word absent from spec.VmBackends is never consulted.
func stubVmProbes(t *testing.T, available map[spec.VmBackend]bool) {
	t.Helper()
	orig := vmBackendProbes
	vmBackendProbes = make(map[spec.VmBackend]func() bool, len(available))
	for word, ok := range available {
		available := ok
		vmBackendProbes[word] = func() bool { return available }
	}
	t.Cleanup(func() { vmBackendProbes = orig })
}

// writeSettings points the hostenv settings seam at a temp config file and
// returns nothing; the caller resolves the runtime.
func writeSettings(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing settings: %v", err)
	}
	orig := hostenv.RuntimeConfigPath
	hostenv.RuntimeConfigPath = func() (string, error) { return path, nil }
	t.Cleanup(func() { hostenv.RuntimeConfigPath = orig })
}

// TestResolvePodEnginePrecedence is the per-level table: flag > deploy >
// runtime (env > settings) > detection, plus the `auto` selector and the candy
// constraint.
func TestResolvePodEnginePrecedence(t *testing.T) {
	stubEngineDetection(t, "docker", nil)
	cases := []struct {
		name    string
		flag    string
		deploy  Selection
		rt      *kit.ResolvedRuntime
		want    spec.EngineName
		wantErr bool
	}{
		{
			name: "flag wins over deploy and runtime",
			flag: "podman", deploy: Selection{Engine: "docker"},
			rt: &kit.ResolvedRuntime{RunEngine: "containerd"}, want: "podman",
		},
		{
			name:   "deploy wins over runtime",
			deploy: Selection{Engine: "docker"},
			rt:     &kit.ResolvedRuntime{RunEngine: "containerd"}, want: "docker",
		},
		{
			name: "runtime wins over detection",
			rt:   &kit.ResolvedRuntime{RunEngine: "containerd"}, want: "containerd",
		},
		{
			name: "detection when every level is empty",
			want: "docker",
		},
		{
			name: "auto at the flag level detects and stops the chain",
			flag: "auto", deploy: Selection{Engine: "docker"},
			rt: &kit.ResolvedRuntime{RunEngine: "containerd"}, want: "docker",
		},
		{
			name:   "matching candy constraint is accepted",
			deploy: Selection{Engine: "containerd", CandyEngine: "containerd"},
			rt:     &kit.ResolvedRuntime{RunEngine: "podman"}, want: "containerd",
		},
		{
			name:   "conflicting candy constraint is an error, not an override",
			deploy: Selection{Engine: "containerd", CandyEngine: "docker"},
			rt:     &kit.ResolvedRuntime{RunEngine: "podman"}, wantErr: true,
		},
		{
			name: "unknown flag word is rejected",
			flag: "bogus", wantErr: true,
		},
		{
			name:   "unknown candy constraint is rejected",
			deploy: Selection{CandyEngine: "bogus"}, wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolvePodEngine(tc.flag, tc.deploy, tc.rt)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ResolvePodEngine() = %q, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolvePodEngine() error: %v", err)
			}
			if got != tc.want {
				t.Errorf("ResolvePodEngine() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestResolvePodEngineEnvBeatsSettings drives the env > settings fold through
// hostenv.ResolveRuntime (the ONE implementation), so the two folded levels are
// covered end-to-end rather than re-implemented in this package.
func TestResolvePodEngineEnvBeatsSettings(t *testing.T) {
	writeSettings(t, "engine:\n  run: docker\n")
	t.Setenv("CHARLY_RUN_MODE", "direct")
	t.Setenv("CHARLY_RUN_ENGINE", "podman")
	t.Setenv("CHARLY_VM_BACKEND", "")

	rt, err := kit.ResolveRuntime()
	if err != nil {
		t.Fatalf("ResolveRuntime: %v", err)
	}
	got, err := ResolvePodEngine("", Selection{}, rt)
	if err != nil {
		t.Fatalf("ResolvePodEngine: %v", err)
	}
	if got != "podman" {
		t.Errorf("env must beat the settings file: got %q, want podman", got)
	}

	t.Setenv("CHARLY_RUN_ENGINE", "")
	rt, err = kit.ResolveRuntime()
	if err != nil {
		t.Fatalf("ResolveRuntime after clearing the env: %v", err)
	}
	got, err = ResolvePodEngine("", Selection{}, rt)
	if err != nil {
		t.Fatalf("ResolvePodEngine: %v", err)
	}
	if got != "docker" {
		t.Errorf("the settings file must beat the default: got %q, want docker", got)
	}
}

// TestResolveVmBackendPrecedence is the per-level table for the VM backend.
func TestResolveVmBackendPrecedence(t *testing.T) {
	stubVmProbes(t, map[spec.VmBackend]bool{"libvirt": true, "qemu": true})
	cases := []struct {
		name    string
		flag    string
		deploy  Selection
		rt      *kit.ResolvedRuntime
		want    spec.VmBackend
		wantErr bool
	}{
		{
			name: "flag wins over deploy and runtime",
			flag: "libvirt", deploy: Selection{Backend: "qemu"},
			rt: &kit.ResolvedRuntime{VmBackend: "krun"}, want: "libvirt",
		},
		{
			name:   "deploy wins over runtime",
			deploy: Selection{Backend: "qemu"},
			rt:     &kit.ResolvedRuntime{VmBackend: "krun"}, want: "qemu",
		},
		{
			name: "runtime wins over detection",
			rt:   &kit.ResolvedRuntime{VmBackend: "krun"}, want: "krun",
		},
		{
			name: "detection uses the schema order (libvirt before qemu)",
			want: "libvirt",
		},
		{
			name: "auto at the flag level detects",
			flag: "auto", deploy: Selection{Backend: "qemu"}, want: "libvirt",
		},
		{
			name: "explicit krun is accepted",
			flag: "krun", want: "krun",
		},
		{
			name: "unknown word is rejected",
			flag: "bogus", wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveVmBackend(tc.flag, tc.deploy, tc.rt)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ResolveVmBackend() = %q, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveVmBackend() error: %v", err)
			}
			if got != tc.want {
				t.Errorf("ResolveVmBackend() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestVmBackendProbesAreSchemaWords keeps the probe table anchored to the
// schema-owned vocabulary: every probe key must be one of spec.VmBackends, and
// detection must be a PROPER subset (at least one provider word is
// explicit-only — krun — so `auto` can never select it).
func TestVmBackendProbesAreSchemaWords(t *testing.T) {
	for word := range vmBackendProbes {
		if !slices.Contains(spec.VmBackends, string(word)) {
			t.Errorf("probe key %q is not one of spec.VmBackends %v", word, spec.VmBackends)
		}
	}
	if len(vmBackendProbes) >= len(spec.VmBackends) {
		t.Errorf("every VM backend has a probe (%d of %d); at least one provider word must be explicit-only",
			len(vmBackendProbes), len(spec.VmBackends))
	}
}

// TestResolveVmBackendDetection pins the detection detail: libvirt is probed
// before qemu, and krun is never auto-selected (it has no probe).
func TestResolveVmBackendDetection(t *testing.T) {
	stubVmProbes(t, map[spec.VmBackend]bool{"libvirt": false, "qemu": true})
	got, err := ResolveVmBackend("", Selection{}, nil)
	if err != nil {
		t.Fatalf("ResolveVmBackend: %v", err)
	}
	if got != "qemu" {
		t.Errorf("detection = %q, want qemu when libvirt is unavailable", got)
	}

	stubVmProbes(t, map[spec.VmBackend]bool{"libvirt": false, "qemu": false})
	if _, err := ResolveVmBackend("", Selection{}, nil); err == nil {
		t.Error("detection must error when no probed backend is available")
	}
}

// TestResolveVmBackendEnvBeatsSettings drives the folded env > settings levels
// for vm.backend.
func TestResolveVmBackendEnvBeatsSettings(t *testing.T) {
	writeSettings(t, "vm:\n  backend: qemu\n")
	t.Setenv("CHARLY_VM_BACKEND", "libvirt")
	t.Setenv("CHARLY_RUN_MODE", "direct")
	t.Setenv("CHARLY_RUN_ENGINE", "podman")

	rt, err := kit.ResolveRuntime()
	if err != nil {
		t.Fatalf("ResolveRuntime: %v", err)
	}
	got, err := ResolveVmBackend("", Selection{}, rt)
	if err != nil {
		t.Fatalf("ResolveVmBackend: %v", err)
	}
	if got != "libvirt" {
		t.Errorf("env must beat the settings file: got %q, want libvirt", got)
	}

	t.Setenv("CHARLY_VM_BACKEND", "")
	rt, err = kit.ResolveRuntime()
	if err != nil {
		t.Fatalf("ResolveRuntime after clearing the env: %v", err)
	}
	got, err = ResolveVmBackend("", Selection{}, rt)
	if err != nil {
		t.Fatalf("ResolveVmBackend: %v", err)
	}
	if got != "qemu" {
		t.Errorf("the settings file must beat the default: got %q, want qemu", got)
	}
}

// TestResolveContainerdTargetPrecedence covers both chains in the target
// resolver: rootful (flag > deploy > runtime > auto) and namespace (flag >
// deploy > settings > capability default).
func TestResolveContainerdTargetPrecedence(t *testing.T) {
	rootfulCases := []struct {
		name       string
		flag       spec.RootfulMode
		deploy     spec.RootfulMode
		rt         string
		wantRoot   bool
		wantPrefix string
		wantErr    bool
	}{
		{name: "flag wins", flag: "sudo", deploy: "native", rt: "machine", wantRoot: true, wantPrefix: "sudo -n"},
		{name: "deploy wins", deploy: "sudo", rt: "native", wantRoot: true, wantPrefix: "sudo -n"},
		{name: "native has no prefix", deploy: "native", rt: "sudo", wantRoot: true, wantPrefix: ""},
		{name: "runtime wins", rt: "sudo", wantRoot: true, wantPrefix: "sudo -n"},
		{name: "auto is rootless", rt: "auto", wantRoot: false, wantPrefix: ""},
		{name: "empty defaults to auto/rootless", wantRoot: false, wantPrefix: ""},
		{name: "unknown mode is rejected", flag: "bogus", wantErr: true},
	}
	for _, tc := range rootfulCases {
		t.Run("rootful/"+tc.name, func(t *testing.T) {
			deploy := Selection{RootfulFlag: tc.flag, Rootful: tc.deploy}
			var rt *kit.ResolvedRuntime
			if tc.rt != "" {
				rt = &kit.ResolvedRuntime{RunEngine: "containerd", Rootful: tc.rt}
			}
			got, err := ResolveContainerdTarget("containerd", deploy, rt)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ResolveContainerdTarget() = %+v, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveContainerdTarget() error: %v", err)
			}
			if got.Rootful != tc.wantRoot || got.PrivilegePrefix != tc.wantPrefix {
				t.Errorf("rootful/prefix = %v/%q, want %v/%q",
					got.Rootful, got.PrivilegePrefix, tc.wantRoot, tc.wantPrefix)
			}
		})
	}

	namespaceCases := []struct {
		name   string
		flag   string
		deploy string
		rt     string
		want   string
	}{
		{name: "flag wins", flag: "ns-flag", deploy: "ns-deploy", rt: "ns-settings", want: "ns-flag"},
		{name: "deploy wins", deploy: "ns-deploy", rt: "ns-settings", want: "ns-deploy"},
		{name: "settings wins", rt: "ns-settings", want: "ns-settings"},
		{name: "capability default", want: "default"},
	}
	for _, tc := range namespaceCases {
		t.Run("namespace/"+tc.name, func(t *testing.T) {
			deploy := Selection{NamespaceFlag: tc.flag, Namespace: tc.deploy}
			var rt *kit.ResolvedRuntime
			if tc.rt != "" {
				rt = &kit.ResolvedRuntime{RunEngine: "containerd", Namespace: tc.rt}
			}
			got, err := ResolveContainerdTarget("containerd", deploy, rt)
			if err != nil {
				t.Fatalf("ResolveContainerdTarget() error: %v", err)
			}
			if got.Namespace != tc.want {
				t.Errorf("namespace = %q, want %q", got.Namespace, tc.want)
			}
			if got.Address != "/run/containerd/containerd.sock" {
				t.Errorf("address = %q, want the containerd capability address", got.Address)
			}
		})
	}
}

// TestResolveContainerdTargetNonContainerdEngines proves the target is uniform:
// an engine with no address/namespace/privilege-prefix facts yields empty
// fields, and rootful is still capability-driven.
func TestResolveContainerdTargetNonContainerdEngines(t *testing.T) {
	for _, engine := range []spec.EngineName{"podman", "docker"} {
		got, err := ResolveContainerdTarget(engine, Selection{RootfulFlag: "sudo"}, nil)
		if err != nil {
			t.Fatalf("%s: ResolveContainerdTarget: %v", engine, err)
		}
		if !got.Rootful {
			t.Errorf("%s: rootful = false, want true for an explicit mode", engine)
		}
		if got.PrivilegePrefix != "" {
			t.Errorf("%s: privilege prefix = %q, want empty (the capability row has none)", engine, got.PrivilegePrefix)
		}
		if got.Address != "" || got.Namespace != "" {
			t.Errorf("%s: address/namespace = %q/%q, want empty", engine, got.Address, got.Namespace)
		}
	}
}

// TestResolveRootfulRefusesUnsupportedEngine uses a fabricated capability row:
// no shipped engine at this pin has supports_rootful=false, so the refusal arm
// needs its own input.
func TestResolveRootfulRefusesUnsupportedEngine(t *testing.T) {
	capability := spec.EngineCapability{Name: "test-engine", SupportsRootful: false}
	if _, _, err := resolveRootful("native", capability); err == nil {
		t.Error("a rootful request against supports_rootful=false must be an error")
	}
	if rootful, prefix, err := resolveRootful("auto", capability); err != nil || rootful || prefix != "" {
		t.Errorf("auto against an unsupported engine = (%v, %q, %v), want (false, \"\", nil)", rootful, prefix, err)
	}
}

// TestSelectionPrecedenceMutantsReverseAdjacentPairs is the FORM mutation: for
// every concept, each adjacent pair in the canonical chain is swapped and the
// winner must change to the level that moved forward. A resolver that ignored
// the order (or a chain built in the wrong order) cannot satisfy this.
func TestSelectionPrecedenceMutantsReverseAdjacentPairs(t *testing.T) {
	engineSources := append(
		podEngineSources("podman", Selection{Engine: "docker"}, &kit.ResolvedRuntime{RunEngine: "containerd"}),
		selectionSource{levelDefault, "detected-engine"},
	)
	assertAdjacentSwapsFlip(t, "pod engine", engineSources, []string{"podman", "docker", "containerd", "detected-engine"})

	backendSources := append(
		vmBackendSources("libvirt", Selection{Backend: "qemu"}, &kit.ResolvedRuntime{VmBackend: "krun"}),
		selectionSource{levelDefault, "detected-backend"},
	)
	assertAdjacentSwapsFlip(t, "vm backend", backendSources, []string{"libvirt", "qemu", "krun", "detected-backend"})

	rootfulSources := append(
		containerdRootfulSources(Selection{RootfulFlag: "sudo", Rootful: "native"}, &kit.ResolvedRuntime{Rootful: "machine"}),
		selectionSource{levelDefault, autoSelector},
	)
	assertAdjacentSwapsFlip(t, "rootful", rootfulSources, []string{"sudo", "native", "machine", autoSelector})

	capability, ok := kit.EngineCapabilityFor("containerd")
	if !ok {
		t.Fatal("containerd capability row is missing")
	}
	namespaceSources := containerdNamespaceSources(
		Selection{NamespaceFlag: "ns-flag", Namespace: "ns-deploy"},
		&kit.ResolvedRuntime{Namespace: "ns-settings"},
		capability,
	)
	assertAdjacentSwapsFlip(t, "namespace", namespaceSources, []string{"ns-flag", "ns-deploy", "ns-settings", capability.Namespace})
}

// assertAdjacentSwapsFlip proves the canonical order AND every adjacent swap.
func assertAdjacentSwapsFlip(t *testing.T, name string, sources []selectionSource, values []string) {
	t.Helper()
	if len(sources) != len(values) {
		t.Fatalf("%s: %d sources, want %d", name, len(sources), len(values))
	}
	for i, source := range sources {
		if source.value != values[i] {
			t.Fatalf("%s: source %d = %q, want %q (the chain order is wrong)", name, i, source.value, values[i])
		}
	}
	if got := firstSet(sources...); got != values[0] {
		t.Fatalf("%s: canonical order resolves %q, want %q", name, got, values[0])
	}
	// Each adjacent pair is tested in ISOLATION: the levels above it are
	// emptied, so the pair is what decides. Swapping the pair must flip the
	// winner; if the resolver ignored the order, it would not.
	for i := 0; i+1 < len(sources); i++ {
		isolated := append([]selectionSource(nil), sources...)
		for j := 0; j < i; j++ {
			isolated[j].value = ""
		}
		if got := firstSet(isolated...); got != values[i] {
			t.Fatalf("%s: isolated pair %d resolves %q, want %q", name, i, got, values[i])
		}
		isolated[i], isolated[i+1] = isolated[i+1], isolated[i]
		got := firstSet(isolated...)
		if got != values[i+1] {
			t.Errorf("%s: swapping levels %d and %d resolved %q, want %q — the precedence order is not load-bearing",
				name, i, i+1, got, values[i+1])
		}
	}
}
