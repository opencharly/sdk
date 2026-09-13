// Command charly-lib-gen generates the source of the shared charly-lib host
// binary from a DATA pins list — the generic, plugin-agnostic aggregation step.
//
// charly-lib is ONE static binary that hosts many out-of-process plugins and
// dispatches them by argv[0] (see github.com/opencharly/sdk/charlylib). The host
// must import each plugin's cmd/serve package, but hardcoding those imports
// would couple the shared host to specific plugins. So this generator takes a
// pins list (the SAME `<name>@<tag>` format charly's
// scripts/host-command-plugins.txt already uses) and emits:
//
//   - go.mod  — requiring the sdk + each pinned plugin module at its Go tag
//   - main.go — a charlylib.Registry literal registering each plugin
//
// Adding a plugin therefore changes only the data (the pins list), never the
// shared library and never charly core. Deterministic: the same pins + sdk
// version produce byte-identical output (guarded by the reproducibility test).
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// defaultGoVersion is the go directive stamped into the generated go.mod.
const defaultGoVersion = "1.26.4"

// pin is one parsed `<name>@<tag>` entry with its derived module path + Go
// module version.
type pin struct {
	name    string // e.g. "plugin-clean" (the binary base name)
	module  string // e.g. "github.com/opencharly/plugin-clean/candy/plugin-clean"
	tag     string // e.g. "v2026.239.1615" (the superproject-style git tag)
	version string // e.g. "v0.2026239.1615" (the plugin's Go module version)
}

func main() {
	var (
		pinsPath   = flag.String("pins", "", "path to the <name>@<tag> pins list (required)")
		outDir     = flag.String("out", "", "output directory for the generated module (required)")
		modulePath = flag.String("module", "github.com/opencharly/charly-lib", "module path of the generated host binary")
		sdkVersion = flag.String("sdk-version", "", "github.com/opencharly/sdk Go module version to require (required)")
		goVersion  = flag.String("go", defaultGoVersion, "go directive for the generated go.mod")
		check      = flag.Bool("check", false, "do not write; verify <out> already matches what would be generated (reproducibility gate)")
	)
	flag.Parse()

	for name, v := range map[string]string{"pins": *pinsPath, "out": *outDir, "sdk-version": *sdkVersion} {
		if strings.TrimSpace(v) == "" {
			fmt.Fprintf(os.Stderr, "charly-lib-gen: -%s is required\n", name)
			os.Exit(2)
		}
	}

	pins, err := parsePins(*pinsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "charly-lib-gen: %v\n", err)
		os.Exit(1)
	}
	if len(pins) == 0 {
		fmt.Fprintf(os.Stderr, "charly-lib-gen: %s lists no plugins\n", *pinsPath)
		os.Exit(1)
	}

	files := map[string][]byte{
		"go.mod":  renderGoMod(*modulePath, *sdkVersion, *goVersion, pins),
		"main.go": renderMain(*pinsPath, pins),
	}

	if *check {
		if err := checkGenerated(*outDir, files); err != nil {
			fmt.Fprintf(os.Stderr, "charly-lib-gen: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "charly-lib-gen: mkdir %s: %v\n", *outDir, err)
		os.Exit(1)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(*outDir, name), data, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "charly-lib-gen: write %s: %v\n", name, err)
			os.Exit(1)
		}
	}
}

// parsePins reads `<name>@<tag>` lines, ignoring blanks and `#` comments, and
// derives each plugin's module path + Go module version. Duplicate names are an
// error (a registry key would silently collide).
func parsePins(path string) ([]pin, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	seen := map[string]bool{}
	var pins []pin
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, tag, ok := strings.Cut(line, "@")
		if !ok || name == "" || tag == "" {
			return nil, fmt.Errorf("%s:%d: %q is not `<name>@<tag>`", path, i+1, line)
		}
		if seen[name] {
			return nil, fmt.Errorf("%s:%d: duplicate plugin %q", path, i+1, name)
		}
		seen[name] = true
		version, err := goModuleVersion(tag)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: plugin %q: %w", path, i+1, name, err)
		}
		pins = append(pins, pin{
			name:    name,
			module:  "github.com/opencharly/" + name + "/candy/" + name,
			tag:     tag,
			version: version,
		})
	}
	sort.Slice(pins, func(i, j int) bool { return pins[i].name < pins[j].name })

	// importAlias maps `-`→`_`, so names differing only by that would collide as
	// Go import identifiers; fail loudly rather than emit ambiguous code.
	aliases := map[string]string{}
	for _, p := range pins {
		a := importAlias(p.name)
		if other, dup := aliases[a]; dup {
			return nil, fmt.Errorf("plugins %q and %q both map to Go import alias %q", other, p.name, a)
		}
		aliases[a] = p.name
	}
	return pins, nil
}

// goModuleVersion maps a superproject-style git tag `vYYYY.DDD.HHMM` to the Go
// module version scheme the plugin repos use, `v0.<YYYYDDD>.<HHMM with leading
// zeros stripped>` (matching the sdk/spec convention; e.g. v2026.239.1615 →
// v0.2026239.1615).
func goModuleVersion(tag string) (string, error) {
	if !strings.HasPrefix(tag, "v") {
		return "", fmt.Errorf("tag %q is not vYYYY.DDD.HHMM", tag)
	}
	t := tag[1:]
	parts := strings.Split(t, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("tag %q is not vYYYY.DDD.HHMM", tag)
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return "", fmt.Errorf("tag %q has a non-numeric segment %q", tag, p)
		}
		nums[i] = n
	}
	if nums[0] < 1000 || nums[1] < 1 || nums[2] < 1 {
		return "", fmt.Errorf("tag %q is out of the vYYYY.DDD.HHMM range", tag)
	}
	return fmt.Sprintf("v0.%04d%03d.%d", nums[0], nums[1], nums[2]), nil
}

// renderGoMod emits the host module's go.mod. Requires are sorted (deterministic)
// with sdk first, then plugins.
func renderGoMod(module, sdkVersion, goVersion string, pins []pin) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by charly-lib-gen. DO NOT EDIT.\n\n")
	fmt.Fprintf(&b, "module %s\n\n", module)
	fmt.Fprintf(&b, "go %s\n\n", goVersion)
	fmt.Fprintf(&b, "require (\n")
	fmt.Fprintf(&b, "\tgithub.com/opencharly/sdk %s\n", sdkVersion)
	for _, p := range pins {
		fmt.Fprintf(&b, "\t%s %s\n", p.module, p.version)
	}
	fmt.Fprintf(&b, ")\n")
	return []byte(b.String())
}

// renderMain emits the argv[0]-dispatch registry literal. The header names the
// pins file's BASE name (not its path) so output is machine-independent and the
// reproducibility gate is stable.
func renderMain(pinsPath string, pins []pin) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by charly-lib-gen from %s. DO NOT EDIT.\n", filepath.Base(pinsPath))
	fmt.Fprintf(&b, "//\n")
	fmt.Fprintf(&b, "// This binary hosts every pinned plugin and dispatches it by the base name\n")
	fmt.Fprintf(&b, "// it was invoked as (argv[0]); the package installs `plugin-<word>` symlinks\n")
	fmt.Fprintf(&b, "// pointing here. See github.com/opencharly/sdk/charlylib.\n")
	fmt.Fprintf(&b, "package main\n\n")
	fmt.Fprintf(&b, "import (\n")
	for _, p := range pins {
		fmt.Fprintf(&b, "\t%s %q\n", importAlias(p.name), p.module)
	}
	fmt.Fprintf(&b, "\n\t%q\n", "github.com/opencharly/sdk/charlylib")
	fmt.Fprintf(&b, ")\n\n")
	fmt.Fprintf(&b, "func main() {\n")
	fmt.Fprintf(&b, "\tcharlylib.Run(charlylib.Registry{\n")
	for _, p := range pins {
		a := importAlias(p.name)
		fmt.Fprintf(&b, "\t\t%q: {Provider: %s.NewProvider, Meta: %s.NewMeta, CLI: %s.CliMain},\n", p.name, a, a, a)
	}
	fmt.Fprintf(&b, "\t})\n")
	fmt.Fprintf(&b, "}\n")
	return []byte(b.String())
}

// importAlias turns a plugin name into a stable, collision-free Go import alias
// (e.g. plugin-clean → plugin_clean).
func importAlias(name string) string {
	return strings.ReplaceAll(name, "-", "_")
}

// checkGenerated verifies outDir's files already equal the generated bytes.
func checkGenerated(outDir string, files map[string][]byte) error {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		got, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			return fmt.Errorf("check %s: %w", name, err)
		}
		if string(got) != string(files[name]) {
			return fmt.Errorf("%s is stale; regenerate (the committed file differs from the generator output)", name)
		}
	}
	return nil
}
