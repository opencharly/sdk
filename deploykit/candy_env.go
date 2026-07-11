package deploykit

import (
	"fmt"
	"slices"
	"strings"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
)

// escapeContainerfileEnvValue escapes `$` to `\$` in an ENV value (so refs to
// runtime-injected vars survive verbatim) EXCEPT `${PATH}`. Relocated (P8).
func escapeContainerfileEnvValue(v string) string {
	const sentinel = "\x00CHARLY_PATH_REF\x00"
	v = strings.ReplaceAll(v, "${PATH}", sentinel)
	v = strings.ReplaceAll(v, "$", "\\$")
	v = strings.ReplaceAll(v, sentinel, "${PATH}")
	return v
}

// WriteCandyEnv emits the merged candy env + builder runtime env as sorted ENV
// directives (plus a PATH-append). Relocated from charly (P8); byte-identical
// (sortStrings → slices.Sort is the same lexicographic order).
func (g *Generator) WriteCandyEnv(b *strings.Builder, candyOrder []string, img *buildkit.ResolvedBox) {
	var configs []*kit.EnvConfig

	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		if layer.HasEnv() {
			cfg, err := layer.EnvConfig()
			if err == nil && cfg != nil {
				configs = append(configs, cfg)
			}
		}
	}

	configs = append(configs, g.CollectBuilderRuntimeEnv(candyOrder, img)...)

	if len(configs) == 0 {
		return
	}

	merged := kit.MergeEnvConfigs(configs)
	expanded := kit.ExpandEnvConfig(merged, img.Home)

	if len(expanded.Vars) > 0 || len(expanded.PathAppend) > 0 {
		b.WriteString("# Layer environment variables\n")
	}

	keys := make([]string, 0, len(expanded.Vars))
	for key := range expanded.Vars {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		fmt.Fprintf(b, "ENV %s=\"%s\"\n", key, escapeContainerfileEnvValue(expanded.Vars[key]))
	}

	if len(expanded.PathAppend) > 0 {
		pathAdditions := strings.Join(expanded.PathAppend, ":")
		fmt.Fprintf(b, "ENV PATH=\"%s:${PATH}\"\n", pathAdditions)
	}

	if len(expanded.Vars) > 0 || len(expanded.PathAppend) > 0 {
		b.WriteString("\n")
	}
}

// WriteExpose emits EXPOSE directives for an image's aggregated ports (via the
// CollectBoxPorts seam). Relocated from charly (P8); byte-identical.
func (g *Generator) WriteExpose(b *strings.Builder, boxName string) {
	ports, _ := g.CollectBoxPorts(boxName)
	if len(ports) == 0 {
		return
	}
	b.WriteString("# Exposed ports\n")
	for _, port := range ports {
		fmt.Fprintf(b, "EXPOSE %s\n", port)
	}
	b.WriteString("\n")
}
