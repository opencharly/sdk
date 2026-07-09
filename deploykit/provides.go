package deploykit

// provides.go — the deploy-config provides types (part of BundleConfig). The provides
// PIPELINE LOGIC (filter/remove/merge) stays in charly/provides.go, using these via
// alias; the shared Named interface stays charly too (EnvProvideEntry satisfies it
// structurally). MCPProvideEntry is spec-homed (shared with the mcp check verb).

import "github.com/opencharly/sdk/spec"

type MCPProvideEntry = spec.MCPProvideEntry

// EnvProvideEntry is a resolved env_provides entry in charly.yml.
type EnvProvideEntry struct {
	Name   string `yaml:"name" json:"name"`
	Value  string `yaml:"value" json:"value"`
	Source string `yaml:"source" json:"source"`
}

func (e EnvProvideEntry) GetName() string   { return e.Name }
func (e EnvProvideEntry) GetSource() string { return e.Source }

// ProvidesConfig holds all resolved provides entries in charly.yml.
type ProvidesConfig struct {
	Env []EnvProvideEntry `yaml:"env,omitempty" json:"env,omitempty"`
	MCP []MCPProvideEntry `yaml:"mcp,omitempty" json:"mcp,omitempty"`
}
