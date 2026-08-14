package packagekit

// packaging.go — parse the `packaging:` section from a charly.yml into the
// spec.Packaging type (yaml.v3 into the CUE-generated struct — no hand-transcribed
// copy). The packaging section is the SINGLE source of truth for all distro-specific
// package metadata (name, description, maintainer, per-format deps, variants); the
// generate-packages plugin reads ONLY this file, never a deps table or PKGBUILD/spec/
// debian-control.

import (
	"fmt"
	"os"

	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// LoadPackaging reads the `packaging:` section from a charly.yml on disk.
func LoadPackaging(path string) (*spec.Packaging, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return ParsePackaging(data)
}

// ParsePackaging parses the `packaging:` section from charly.yml bytes. A candy
// charly.yml is `candy-name: {candy: {…}}` — the candy name is dynamic, so the
// parser walks the top-level keys and returns the first `candy.packaging` block.
func ParsePackaging(data []byte) (*spec.Packaging, error) {
	var root map[string]struct {
		Candy struct {
			Packaging *spec.Packaging `yaml:"packaging"`
		} `yaml:"candy"`
	}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse charly.yml: %w", err)
	}
	for _, c := range root {
		if c.Candy.Packaging != nil {
			return c.Candy.Packaging, nil
		}
	}
	return nil, fmt.Errorf("no packaging: section in charly.yml")
}
