package packagekit

// config.go — render the packaging section's config (a system-wide project
// charly.yml, e.g. /etc/charly/charly.yml) into nfpm files.Contents. The
// rendered file is a minimal valid project: version + a `charly-mcp` candy
// node carrying the plugin bake refs + a no-op plan, so the systemd-started
// MCP server resolves a local project instead of falling back to a network
// fetch of opencharly/charly.

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/goreleaser/nfpm/v2/files"
	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// configContents renders pkg.Config to a minimal valid project charly.yml and
// appends it at pkg.Config.Path (e.g. /etc/charly/charly.yml). The rendered
// body is written to a temp dir (nfpm contents reference on-disk sources) and
// referenced by Destination; the temp dir lives for the build process.
func configContents(pkg *spec.Packaging) (files.Contents, error) {
	cfg := pkg.Config
	if cfg == nil {
		return nil, nil
	}
	if cfg.Path == "" || cfg.Version == "" {
		return nil, fmt.Errorf("packaging.config: path and version are required")
	}
	body, err := renderConfig(cfg)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "charly-config-*")
	if err != nil {
		return nil, fmt.Errorf("create config temp dir: %w", err)
	}
	src := filepath.Join(dir, "charly.yml")
	if err := os.WriteFile(src, body, 0o644); err != nil {
		return nil, fmt.Errorf("write config: %w", err)
	}
	return files.Contents{
		{
			Source:      src,
			Destination: cfg.Path,
			FileInfo:    &files.ContentFileInfo{Mode: 0o644},
		},
	}, nil
}

// packagedConfigDoc is the rendered shape of the packaged system-wide
// charly.yml: version + a `charly-mcp` candy node (the ONE UnifiedFile format
// — a plain project, nothing special about it).
type packagedConfigDoc struct {
	Version   string `yaml:"version"`
	CharlyMCP struct {
		Candy struct {
			Version     string               `yaml:"version"`
			Description string               `yaml:"description,omitempty"`
			BakePlugin  []string             `yaml:"bake_plugin,omitempty"`
			Plan        []packagedConfigStep `yaml:"plan,omitempty"`
		} `yaml:"candy"`
	} `yaml:"charly-mcp"`
}

// packagedConfigStep is the no-op plan step: a `run:` step with a `command:
// "true" input in the build context, so `charly box validate` passes on the
// shipped config.
type packagedConfigStep struct {
	Run     string   `yaml:"run"`
	Command string   `yaml:"command"`
	Context []string `yaml:"context"`
}

// renderConfig renders the minimal project charly.yml for the packaged config.
func renderConfig(cfg *spec.PackagingConfig) ([]byte, error) {
	doc := packagedConfigDoc{Version: cfg.Version}
	doc.CharlyMCP.Candy.Version = cfg.Version
	doc.CharlyMCP.Candy.Description = cfg.Description
	doc.CharlyMCP.Candy.BakePlugin = cfg.Plugins
	doc.CharlyMCP.Candy.Plan = []packagedConfigStep{{
		Run:     "no-op — this project exists so the systemd MCP server resolves a local project",
		Command: "true",
		Context: []string{"build"},
	}}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("render config: %w", err)
	}
	return data, nil
}
