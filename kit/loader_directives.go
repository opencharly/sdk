package kit

// loader_directives.go — the kind-blind config-loader document DIRECTIVES: the
// `import:` field (ImportEntry/ImportList) and the `discover:` field
// (DiscoverConfig/ScanSpec) + AnchorScanSpecs. Relocated here (from charly/unified.go,
// where sdk/loaderkit had also grown a faithful-port duplicate) so charly core AND
// sdk/loaderkit share ONE copy (R3). Behavior-identical: same types, same YAML
// shapes, same error messages.

import (
	"fmt"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ImportEntry is one parsed `import:` list item. A flat entry (Namespace == "")
// merges the referenced file into the current root namespace; a namespaced
// entry mounts the referenced project under Namespace.
type ImportEntry struct {
	Namespace string // "" = flat import into the current root namespace
	Ref       string // local path or `@host/org/repo[/sub/path]:version`
}

// ImportList is the `import:` field type. Custom YAML decoding accepts a list
// whose items are either a bare string (flat) or a single-key mapping
// `alias: ref` (namespaced child import).
type ImportList []ImportEntry

// UnmarshalYAML decodes the mixed-shape import list.
func (il *ImportList) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		return fmt.Errorf("import: must be a list (got kind=%v)", node.Kind)
	}
	out := make(ImportList, 0, len(node.Content))
	for i, item := range node.Content {
		switch item.Kind {
		case yaml.ScalarNode:
			if item.Value == "" {
				return fmt.Errorf("import[%d]: empty ref", i)
			}
			out = append(out, ImportEntry{Ref: item.Value})
		case yaml.MappingNode:
			if len(item.Content) != 2 {
				return fmt.Errorf("import[%d]: a namespaced entry must be a single-key map `alias: ref`", i)
			}
			alias := item.Content[0].Value
			ref := item.Content[1].Value
			if alias == "" || ref == "" {
				return fmt.Errorf("import[%d]: namespaced entry needs both an alias and a ref", i)
			}
			out = append(out, ImportEntry{Namespace: alias, Ref: ref})
		default:
			return fmt.Errorf("import[%d]: each item must be a string ref or a single-key `alias: ref` map (got kind=%v)", i, item.Kind)
		}
	}
	*il = out
	return nil
}

// MarshalYAML emits each entry compactly: a flat entry as a scalar string, a
// namespaced entry as a single-key `alias: ref` map — the same shapes
// UnmarshalYAML accepts (round-trip safe; used by migrators that write configs).
func (il ImportList) MarshalYAML() (any, error) { //nolint:unparam // error return kept for interface/API stability
	seq := &yaml.Node{Kind: yaml.SequenceNode}
	for _, e := range il {
		if e.Namespace == "" {
			seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: e.Ref})
			continue
		}
		seq.Content = append(seq.Content, &yaml.Node{
			Kind: yaml.MappingNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: e.Namespace},
				{Kind: yaml.ScalarNode, Value: e.Ref},
			},
		})
	}
	return seq, nil
}

// DiscoverConfig is a FLAT list of generic scan specs. Each spec scans a path
// for directories containing its manifest; every discovered manifest is parsed
// as a multi-document stream and routed by SHAPE (the kind-key it carries), so
// one discover root can surface candies, boxes, deploys — any kind. There is no
// kind dimension and no hardcoded path/filename: discovery is fully configured
// in charly.yml.
type DiscoverConfig []ScanSpec

// ScanSpec describes one discovery root. Accepts string shorthand
// ("candy" → {Path: "candy", Recursive: true}) or the explicit object form
// ({path: X, recursive: false}). Empty Path is invalid.
type ScanSpec struct {
	Path      string `yaml:"path" json:"path"`
	Recursive bool   `yaml:"recursive" json:"recursive"`
	// Manifest is the per-directory manifest filename to look for. Empty
	// defaults to UnifiedFileName; configurable per spec in charly.yml.
	Manifest string `yaml:"manifest,omitempty" json:"manifest,omitempty"`
}

// UnmarshalYAML accepts the string shorthand where Recursive defaults to true,
// and the object form where Recursive defaults to true when omitted.
func (s *ScanSpec) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		s.Path = node.Value
		s.Recursive = true
		s.Manifest = UnifiedFileName
		return nil
	}
	// Object form — decode with `recursive` defaulting to true when absent.
	// yaml.v3 has no direct "default true"; we interpret missing as true by
	// looking at the raw node and only clearing Recursive when the field is
	// explicitly set to false.
	var raw struct {
		Path      string `yaml:"path" json:"path"`
		Recursive *bool  `yaml:"recursive" json:"recursive"`
		Manifest  string `yaml:"manifest" json:"manifest"`
	}
	if err := node.Decode(&raw); err != nil {
		return err
	}
	s.Path = raw.Path
	if raw.Recursive == nil {
		s.Recursive = true
	} else {
		s.Recursive = *raw.Recursive
	}
	s.Manifest = raw.Manifest
	if s.Manifest == "" {
		s.Manifest = UnifiedFileName
	}
	return nil
}

// AnchorScanSpecs returns a copy of `specs` with every relative Path
// resolved to an absolute path against `srcDir`. Absolute paths are
// kept verbatim. Empty srcDir leaves specs unchanged so the
// root-file merge (called with rootDir == workspace) is a no-op.
func AnchorScanSpecs(specs []ScanSpec, srcDir string) []ScanSpec {
	if srcDir == "" || len(specs) == 0 {
		return specs
	}
	out := make([]ScanSpec, len(specs))
	for i, s := range specs {
		out[i] = s
		if s.Path != "" && !filepath.IsAbs(s.Path) {
			out[i].Path = filepath.Join(srcDir, s.Path)
		}
	}
	return out
}
