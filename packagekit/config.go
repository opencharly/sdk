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
// charly.yml: a BARE minimal project (version only). The MCP server's `mcp`
// command word resolves from the baked /usr/lib/charly/plugins/plugin-mcp
// .providers manifest — the project exists so build-mode tools
// (box.list.boxes etc.) resolve a local project instead of a network fallback,
// and a bare version project is what `charly box validate` accepts (a candy
// node would need install content, which a system config has none of).
type packagedConfigDoc struct {
	Version string `yaml:"version"`
}

// renderConfig renders the minimal project charly.yml for the packaged config.
func renderConfig(cfg *spec.PackagingConfig) ([]byte, error) {
	data, err := yaml.Marshal(packagedConfigDoc{Version: cfg.Version})
	if err != nil {
		return nil, fmt.Errorf("render config: %w", err)
	}
	return data, nil
}
