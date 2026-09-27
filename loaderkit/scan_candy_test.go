package loaderkit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestScanInlineCandy_AgentProvideAndTerminalProfiles proves populateFromYAML carries the
// authored agent_provide:/terminal_profile: candy-manifest keys onto CandyView — the scan-time
// half of the W9 gap surfaced merging origin/main's federated control-plane commit into the
// CandyReader retype (the wrap-time half is TestSpecCandyAdapter_AgentProvideAndTerminalProfiles
// in sdk/deploykit).
func TestScanInlineCandy_AgentProvideAndTerminalProfiles(t *testing.T) {
	ly := &spec.CandyYAML{
		Name:         "agent-candy",
		AgentProvide: []spec.AgentRuntimeCapability{{Provider: "pi"}},
		TerminalProfiles: map[string]spec.TerminalProfile{
			"claude-code": {Name: "claude-code", Entrypoint: []string{"claude"}},
		},
	}

	_, v, _ := ScanInlineCandy("agent-candy", t.TempDir(), ly)

	if len(v.AgentProvide) != 1 || v.AgentProvide[0].Provider != "pi" {
		t.Errorf("CandyView.AgentProvide = %v, want [{Provider: pi}]", v.AgentProvide)
	}
	tp, ok := v.TerminalProfiles["claude-code"]
	if !ok || len(tp.Entrypoint) != 1 || tp.Entrypoint[0] != "claude" {
		t.Errorf("CandyView.TerminalProfiles[\"claude-code\"] = %+v, ok=%v, want {Entrypoint: [claude]}, true", tp, ok)
	}
}

// TestScanInlineCandy_PluginRequires proves populateFromYAML carries the authored
// `plugin.requires:` list onto CandyView, the SAME way it carries the plugin's source +
// providers. Without the projection the field is inert: the resolved view (the surface the
// host's requires gate reads) would expose a plugin's providers but never its declared
// inter-plugin dependencies. The wrap-time half is
// TestSpecCandyAdapter_GetPluginRequires in sdk/deploykit.
func TestScanInlineCandy_PluginRequires(t *testing.T) {
	ly := &spec.CandyYAML{
		Name: "req-candy",
		Plugin: &spec.Plugin{
			Source:    "github.com/opencharly/plugin-example/candy/plugin-example",
			Providers: []spec.PluginCapability{"verb:example"},
			Requires: []spec.PluginRequirement{
				{Capability: "command:generate:box", Source: "github.com/opencharly/plugin-build/candy/plugin-build"},
				{Capability: "verb:enc", Optional: true},
			},
		},
	}

	_, v, _ := ScanInlineCandy("req-candy", t.TempDir(), ly)

	if !v.IsPlugin {
		t.Fatal("CandyView.IsPlugin = false for a candy with a plugin: block, want true")
	}
	if len(v.PluginProviders) != 1 || v.PluginProviders[0] != "verb:example" {
		t.Errorf("CandyView.PluginProviders = %v, want [verb:example]", v.PluginProviders)
	}
	if len(v.PluginRequires) != 2 {
		t.Fatalf("CandyView.PluginRequires = %v, want 2 entries (the missing projection leaves it empty)", v.PluginRequires)
	}
	if got := v.PluginRequires[0]; got.Capability != "command:generate:box" || got.Source != "github.com/opencharly/plugin-build/candy/plugin-build" {
		t.Errorf("PluginRequires[0] = %+v, want {Capability: command:generate:box, Source: github.com/opencharly/plugin-build/candy/plugin-build}", got)
	}
	if got := v.PluginRequires[1]; got.Capability != "verb:enc" || !got.Optional {
		t.Errorf("PluginRequires[1] = %+v, want {Capability: verb:enc, Optional: true}", got)
	}
}

// TestPopulateCandyApk (relocated from charly/apk_format_test.go, #55 K3 Cone 4) verifies the
// candy manifest `apk:` field flows through the scan pipeline onto the resulting
// spec.CandyReader.
func TestPopulateCandyApk(t *testing.T) {
	ly := &spec.CandyYAML{
		Apk: []spec.ApkPackageSpec{
			{Package: "org.fdroid.fdroid", Source: "apk-pure", Arch: "x86_64"},
		},
	}
	m, v, _ := ScanInlineCandy("test-apps", "", ly)
	l := newLoaderTestCandy("test-apps", m, v)
	if len(l.Apk()) != 1 || l.Apk()[0].Package != "org.fdroid.fdroid" {
		t.Errorf("Apk() = %+v", l.Apk())
	}
}
