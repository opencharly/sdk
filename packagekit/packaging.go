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
//
// The document is decoded into a yaml.Node rather than a map[string]struct{…}
// because a REAL candy charly.yml carries a top-level `version: <calver>` SCALAR
// alongside the `candy:` map. Unmarshalling the whole document into a typed map
// fails on that scalar before ever reaching the candy body, so `charly
// generate-packages` errored on every real candy. Walking the node lets a
// top-level scalar be tolerated while the `candy:` map is decoded into the
// CUE-generated spec.Packaging type.
func ParsePackaging(data []byte) (*spec.Packaging, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse charly.yml: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, fmt.Errorf("no packaging: section in charly.yml")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("no packaging: section in charly.yml")
	}
	// Walk top-level keys; a top-level scalar (e.g. `version:`) is skipped, and
	// a mapping keyed by a candy name is decoded for its `candy.packaging` block.
	for i := 0; i+1 < len(root.Content); i += 2 {
		val := root.Content[i+1]
		if val.Kind != yaml.MappingNode {
			continue
		}
		var body struct {
			Candy struct {
				Packaging *spec.Packaging `yaml:"packaging"`
			} `yaml:"candy"`
		}
		if err := val.Decode(&body); err != nil {
			return nil, fmt.Errorf("parse charly.yml: %w", err)
		}
		if body.Candy.Packaging != nil {
			return body.Candy.Packaging, nil
		}
	}
	return nil, fmt.Errorf("no packaging: section in charly.yml")
}
