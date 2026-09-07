package packagekit

// render_test.go — the packaging section's systemd units + system-wide config
// render into the deb/rpm/archlinux package contents (NOT apk/ipk — no systemd),
// and the non-autostarting contract holds (units ship with [Install] but no
// post-install enable; the preset files disable them).

import (
	"strings"
	"testing"

	"github.com/goreleaser/nfpm/v2"
	"github.com/opencharly/spec/spec"
)

// testPackagingWithSystemd returns the fixture packaging section plus a
// systemd + config declaration.
func testPackagingWithSystemd() *spec.Packaging {
	pkg := testPackaging()
	pkg.Systemd = []*spec.PackagingSystemdUnit{
		{
			Name:             "charly-mcp",
			Scope:            "system",
			Exec:             "/usr/bin/charly mcp serve --listen 127.0.0.1:18765",
			Description:      "Charly MCP server (Streamable HTTP on 127.0.0.1:18765)",
			Restart:          "on-failure",
			After:            []string{"network-online.target"},
			Wants:            []string{"network-online.target"},
			Working_directory: "/etc/charly",
		},
		{
			Name:        "charly-mcp",
			Scope:       "user",
			Exec:        "/usr/bin/charly mcp serve --listen 127.0.0.1:18765",
			Description: "Charly MCP server (user session, Streamable HTTP on 127.0.0.1:18765)",
			Restart:     "on-failure",
		},
	}
	pkg.Config = &spec.PackagingConfig{
		Path:        "/etc/charly/charly.yml",
		Version:     "2026.250.0001",
		Description: "System-wide charly MCP server project (started via systemd)",
		Plugins:     []string{"@github.com/opencharly/plugin-mcp/candy/plugin-mcp:v1"},
	}
	return pkg
}

// TestBuildInfoSystemdContents — the deb/rpm/archlinux contents carry the
// systemd units + presets + the system-wide config; apk/ipk do not.
func TestBuildInfoSystemdContents(t *testing.T) {
	binary, pluginsDir := fixtureInputs(t)
	pkg := testPackagingWithSystemd()

	for _, format := range []string{"deb", "rpm", "archlinux"} {
		opts := BuildOptions{Binary: binary, PluginsDir: pluginsDir, Version: "2026.250.0001", Arch: "amd64", Variant: "minimal"}
		info, err := BuildInfo(pkg, format, opts)
		if err != nil {
			t.Fatalf("BuildInfo(%s): %v", format, err)
		}
		dests := contentsDestinations(info)
		for _, want := range []string{
			"/usr/lib/systemd/system/charly-mcp.service",
			"/usr/lib/systemd/user/charly-mcp.service",
			"/usr/lib/systemd/system-preset/50-charly.preset",
			"/usr/lib/systemd/user-preset/50-charly.preset",
			"/etc/charly/charly.yml",
		} {
			if !dests[want] {
				t.Errorf("%s: missing %s (contents: %v)", format, want, keysOf(dests))
			}
		}
	}

	for _, format := range []string{"apk", "ipk"} {
		opts := BuildOptions{Binary: binary, PluginsDir: pluginsDir, Version: "2026.250.0001", Arch: "amd64", Variant: "minimal"}
		info, err := BuildInfo(pkg, format, opts)
		if err != nil {
			t.Fatalf("BuildInfo(%s): %v", format, err)
		}
		for _, c := range info.Contents {
			if strings.Contains(c.Destination, "systemd") || strings.Contains(c.Destination, "/etc/charly/") {
				t.Errorf("%s: must NOT ship %s (no systemd)", format, c.Destination)
			}
		}
	}
}

// TestRenderSystemdUnit_NonAutostarting — the rendered unit carries [Install]
// (so `systemctl enable` works when the operator opts in) but the renderer
// emits no enable — the non-autostarting contract; the preset disables it.
func TestRenderSystemdUnit_NonAutostarting(t *testing.T) {
	u := &spec.PackagingSystemdUnit{
		Name:        "charly-mcp",
		Scope:       "system",
		Exec:        "/usr/bin/charly mcp serve --listen 127.0.0.1:18765",
		Description: "test",
	}
	rendered := renderSystemdUnit(u, "multi-user.target")
	for _, want := range []string{"[Unit]", "ExecStart=", "[Install]", "WantedBy=multi-user.target", "Restart=on-failure"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered unit lacks %q: %s", want, rendered)
		}
	}
	if strings.Contains(rendered, "[Unit]\nExecStart") && !strings.Contains(rendered, "Type=simple") {
		t.Errorf("rendered unit lacks Type=simple: %s", rendered)
	}
	preset := renderPreset([]string{"charly-mcp.service"})
	if !strings.Contains(preset, "disable charly-mcp.service") {
		t.Errorf("preset must disable the unit: %s", preset)
	}
}

// TestRenderConfig_ValidProject — the rendered config is a minimal valid
// project (version + charly-mcp candy with the bake refs + a no-op plan).
func TestRenderConfig_ValidProject(t *testing.T) {
	cfg := &spec.PackagingConfig{
		Path:        "/etc/charly/charly.yml",
		Version:     "2026.250.0001",
		Description: "System-wide charly MCP server project",
		Plugins:     []string{"@github.com/opencharly/plugin-mcp/candy/plugin-mcp:v1"},
	}
	body, err := renderConfig(cfg)
	if err != nil {
		t.Fatalf("renderConfig: %v", err)
	}
	// The rendered config is a BARE minimal project: the version stamp is the
	// whole file (a candy node would need install content — see the live
	// validate test). The plugins/description are packaging metadata, not
	// rendered file content.
	s := string(body)
	if !strings.Contains(s, "version: 2026.250.0001") {
		t.Errorf("rendered config lacks the version: %s", s)
	}
	if strings.Contains(s, "charly-mcp:") {
		t.Errorf("rendered config carries a candy node (a bare project is what validates): %s", s)
	}
}

// contentsDestinations returns the set of content destinations.
func contentsDestinations(info *nfpm.Info) map[string]bool {
	out := make(map[string]bool)
	for _, c := range info.Contents {
		out[c.Destination] = true
	}
	return out
}

// keysOf returns the keys of a map, joined for a failure message.
func keysOf(m map[string]bool) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return strings.Join(keys, ", ")
}
