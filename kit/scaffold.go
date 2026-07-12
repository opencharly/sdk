package kit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// scaffold.go — the project/candy/box authoring ENGINE (the `charly box new` machinery), relocated
// from charly core so BOTH the core CLI (`charly box new`) AND the Wave-2 command:box plugin drive
// ONE shared authoring library (R3). Pure fs + yaml.v3; the only charly-layout knowledge is the kit
// layout constants (UnifiedFileName/DefaultCandyDir/DefaultBoxDir) kit already owns. The functions do
// the filesystem work and return; the CALLER owns user-facing output (so kit stays presentation-free).
//
// Wall-clock stays out of kit: ScaffoldCandy takes the candy's `version:` CalVer as a param (the
// caller passes charly's ComputeCalVer()), mirroring how the migrate helpers avoid reading the clock.

// scaffoldCharlyYAML is the seed charly.yml written into a fresh project. The project is immediately
// usable — the default distro/builder/init/resource build vocabulary AND sidecar templates are embedded
// in the charly binary, so there is no build vocabulary to copy or wire. The caller substitutes
// __SCHEMA_VERSION__ via ScaffoldProject (LatestSchemaVersion()).
const scaffoldCharlyYAML = `# charly.yml — unified project root: the single file a project needs.
# See https://github.com/opencharly/charly for documentation.
#
# Box (image) and candy (layer) definitions are DISCOVERED per name:
#   box/<name>/charly.yml   — one box per directory
#   candy/<name>/charly.yml — one candy per directory
# The default distro/builder/init build vocabulary is EMBEDDED in the charly
# binary; declare distro:/builder:/init:/resource: here only to EXTEND or
# OVERRIDE it.
#
# Cross-kind name reuse is permitted — a single name (e.g. my-app) MAY exist
# simultaneously as a candy, a box, a pod, a vm, a k8s, a local, AND a deploy
# entry. charly verbs disambiguate by command context.

version: __SCHEMA_VERSION__

discover:
  - path: box
    recursive: true
  - path: candy
    recursive: true

defaults:
  registry: ghcr.io/example
  tag: auto
  platform:
    - linux/amd64
  build: [rpm]
`

// scaffoldGitignore keeps the build artefact dir + common scratch files out of git so a fresh
// project is committable as-is.
const scaffoldGitignore = `# Build artefacts
.build/

# Editor / OS
.DS_Store
*.swp
`

// ScaffoldCandy creates a new candy directory at dir/<DefaultCandyDir>/<name> with a placeholder
// manifest in the compact name-first node form. ADE mandates a description + at least one deterministic
// check: step, so the scaffold ships a minimal passing pair the author replaces. calver stamps the
// candy's mandatory version:. Errors if the candy already exists. The caller prints the created path.
func ScaffoldCandy(dir, name, calver string) error {
	candyDir := filepath.Join(dir, DefaultCandyDir, name)

	if _, err := os.Stat(candyDir); err == nil {
		return fmt.Errorf("candy %q already exists at %s", name, candyDir)
	}

	if err := os.MkdirAll(candyDir, 0755); err != nil {
		return fmt.Errorf("creating candy directory: %w", err)
	}

	candyYml := filepath.Join(candyDir, UnifiedFileName)
	candyContent := fmt.Sprintf(`# %s candy config
%s:
    candy:
        version: %s
        description: |
            TODO: one-line purpose of the %s candy
        # Add packages:  charly candy add-rpm %s <pkg>   (also add-deb / add-pac / add-aur)
        plan:
            - check: the /etc/os-release marker exists (replace with a real check)
              file: /etc/os-release
`, name, name, calver, name, name)
	if err := os.WriteFile(candyYml, []byte(candyContent), 0644); err != nil {
		return fmt.Errorf("creating %s: %w", UnifiedFileName, err)
	}
	return nil
}

// ScaffoldProject creates an empty charly project at dir. Idempotency: errors out if dir already
// contains an charly.yml so we never silently clobber an existing project. The dir itself may exist.
// The seed's schema version is stamped to the current HEAD (LatestSchemaVersion).
func ScaffoldProject(dir string) error {
	if dir == "" {
		return fmt.Errorf("project directory must be specified")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating project directory: %w", err)
	}
	charlyPath := filepath.Join(dir, UnifiedFileName)
	if _, err := os.Stat(charlyPath); err == nil {
		return fmt.Errorf("charly.yml already exists at %s; refusing to overwrite", charlyPath)
	}
	seed := strings.ReplaceAll(scaffoldCharlyYAML, "__SCHEMA_VERSION__", LatestSchemaVersion().String())
	if err := os.WriteFile(charlyPath, []byte(seed), 0o644); err != nil {
		return fmt.Errorf("writing charly.yml: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, DefaultBoxDir), 0o755); err != nil {
		return fmt.Errorf("creating box/: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, DefaultCandyDir), 0o755); err != nil {
		return fmt.Errorf("creating candy/: %w", err)
	}
	gitignorePath := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
		if err := os.WriteFile(gitignorePath, []byte(scaffoldGitignore), 0o644); err != nil {
			return fmt.Errorf("writing .gitignore: %w", err)
		}
	}
	return nil
}

// AddBox writes a new box to its discovered per-box file box/<name>/charly.yml as a node-form IMAGE —
// `<name>: {candy: {base: …}}`. The base argument is the value of the image's `base:` field (an
// external URL or the name of another box). If layers is non-nil it populates the image's `candy:`
// composition list. Errors if box/<name>/charly.yml exists.
func AddBox(dir, name, base string, layers []string) error {
	if name == "" {
		return fmt.Errorf("box name must be specified")
	}
	dest := filepath.Join(dir, DefaultBoxDir, name, UnifiedFileName)
	if FileExists(dest) {
		return fmt.Errorf("box %q already exists at %s", name, dest)
	}
	// The candy: value (the image body) — name is the NODE KEY, not a field.
	inner := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	inner.Content = append(inner.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "base"},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: base},
	)
	if len(layers) > 0 {
		candiesNode := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, l := range layers {
			candiesNode.Content = append(candiesNode.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: l},
			)
		}
		inner.Content = append(inner.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "candy"},
			candiesNode,
		)
	}
	// node-form: <name>: {candy: <inner>}
	candyDisc := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	candyDisc.Content = append(candyDisc.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "candy"},
		inner,
	)
	wrapper := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	wrapper.Content = append(wrapper.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name},
		candyDisc,
	)
	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{wrapper}}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("creating box directory: %w", err)
	}
	return SaveYAMLNodeFile(dest, doc)
}
