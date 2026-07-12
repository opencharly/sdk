package kit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// yaml.go — generic comment-preserving yaml.Node utilities shared by plugins and charly core (no
// charly/candy specifics). SetByDotPath backs `charly box set` (core) and `charly candy set`
// (candy/plugin-candy); MappingChild is the shared mapping-node lookup both the candy plugin and
// `charly box scaffold` use. Living in kit keeps ONE copy (R3) without walling generic yaml logic in
// core behind a seam — a plugin editing yaml owns that itself.

// SetByDotPath edits the file at path, navigating into the YAML structure via dotpath (dot-separated
// keys) and replacing the leaf value with valueYAML (parsed as YAML so callers can pass scalars,
// lists, or maps). Comments and key order are preserved.
func SetByDotPath(path, dotpath, valueYAML string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	// Parse the value as YAML so callers can use lists, maps, scalars.
	var valueNode yaml.Node
	if err := yaml.Unmarshal([]byte(valueYAML), &valueNode); err != nil {
		return fmt.Errorf("parsing value %q as YAML: %w", valueYAML, err)
	}
	// yaml.Unmarshal wraps in a DocumentNode; peel it.
	leaf := &valueNode
	if leaf.Kind == yaml.DocumentNode && len(leaf.Content) > 0 {
		leaf = leaf.Content[0]
	}

	parts := strings.Split(dotpath, ".")
	if len(parts) == 0 || parts[0] == "" {
		return fmt.Errorf("empty dotpath")
	}
	doc := &root
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		doc = doc.Content[0]
	}
	if err := setNodeByPath(doc, parts, leaf); err != nil {
		return fmt.Errorf("setting %s in %s: %w", dotpath, path, err)
	}

	out, err := yaml.Marshal(&root)
	if err != nil {
		return fmt.Errorf("marshalling %s: %w", path, err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// setNodeByPath walks parts through a mapping tree, creating intermediate mapping nodes as needed, and
// replaces the leaf with newValue. Errors if any intermediate node is not a mapping.
func setNodeByPath(node *yaml.Node, parts []string, newValue *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("expected mapping at path segment %q, got kind %d", parts[0], node.Kind)
	}
	key := parts[0]
	rest := parts[1:]

	// Find existing child at key.
	childIdx := -1
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			childIdx = i + 1
			break
		}
	}

	if len(rest) == 0 {
		// Leaf assignment.
		if childIdx >= 0 {
			node.Content[childIdx] = newValue
			return nil
		}
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			newValue,
		)
		return nil
	}

	// Recurse into existing mapping or create one.
	if childIdx >= 0 {
		return setNodeByPath(node.Content[childIdx], rest, newValue)
	}
	newChild := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		newChild,
	)
	return setNodeByPath(newChild, rest, newValue)
}

// SaveYAMLNodeFile marshals a node tree back to an arbitrary file path, preserving comments + key
// order (the yaml.v3 Node round-trip). Shared by the scaffold/authoring engine (kit.AddBox) and
// charly core's box add-candy/rm-candy verbs — ONE marshal-to-file home (R3).
func SaveYAMLNodeFile(path string, root *yaml.Node) error {
	data, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("marshalling %s: %w", filepath.Base(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", filepath.Base(path), err)
	}
	return nil
}

// MappingChild looks up a key in a mapping node. Returns the value node or nil if missing. yaml mapping
// nodes store [key, value, key, value, …].
func MappingChild(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
