package deploykit

// provides.go — the deploy-config provides types (part of BundleConfig) AND the
// provides PIPELINE LOGIC (filter/template-resolve — K4: relocated from
// charly/provides.go, a genuinely pure mechanism with no project-loader dependency).
// FilterOwnProvides needs the Named generic constraint, already deploykit-native.
// MCPProvideEntry is spec-homed (shared with the mcp check verb). Consumed directly
// by charly core's remaining callers (config_image.go, pod_lifecycle_resolve.go),
// which now import deploykit directly (K3 ZERO-ALIASES — no alias file).

import (
	"strconv"
	"strings"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
)

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

// FilterOwnProvides removes entries injected by the given image (self-exclusion).
// Kept for RemoveBySource and other callers that need strict exclusion.
func FilterOwnProvides[T Named](entries []T, boxName string) []T {
	if boxName == "" {
		return entries
	}
	var result []T
	for _, e := range entries {
		if e.GetSource() != boxName {
			result = append(result, e)
		}
	}
	return result
}

// AcceptedEnvSet builds a set of env var names from env_accepts and env_requires declarations.
// Used to filter which env_provides vars get injected into a consumer.
func AcceptedEnvSet(accepts, requires []spec.EnvDependency) map[string]bool {
	m := make(map[string]bool, len(accepts)+len(requires))
	for _, dep := range accepts {
		m[dep.Name] = true
	}
	for _, dep := range requires {
		m[dep.Name] = true
	}
	return m
}

// ResolveTemplate replaces template placeholders in a string:
//
//	{{.ContainerName}}        -> containerName
//	{{.ContainerPort <N>}}    -> <N> (literal — kept for symmetry/readability)
//	{{.HostPort <N>}}         -> host port mapped to container port <N>
//	                             (looked up in portMap; falls back to <N>
//	                             if not found — caller should validate the
//	                             port is actually published before relying
//	                             on the substitution)
//
// portMap is a {containerPort -> hostPort} table built from the resolved
// port mapping list at env-injection time. nil portMap is accepted (every
// {{.HostPort N}} degrades to the literal container port — useful for
// validation-time substitution before runtime data is available).
func ResolveTemplate(tmpl, containerName string, portMap map[int]int) string {
	out := strings.ReplaceAll(tmpl, "{{.ContainerName}}", containerName)
	out = substPortTemplate(out, "{{.ContainerPort ", "}}", strconv.Itoa)
	out = substPortTemplate(out, "{{.HostPort ", "}}", func(n int) string {
		if portMap != nil {
			if h, ok := portMap[n]; ok {
				return strconv.Itoa(h)
			}
		}
		return strconv.Itoa(n)
	})
	return out
}

// substPortTemplate walks the input, finds every `<prefix><N><suffix>`
// occurrence where N is a numeric argument, and replaces with mapFn(N).
// Unterminated or non-numeric placeholders pass through verbatim — the
// validator (ValidateProvidesTemplate) rejects them at config time.
func substPortTemplate(s, prefix, suffix string, mapFn func(int) string) string {
	var out strings.Builder
	for {
		i := strings.Index(s, prefix)
		if i < 0 {
			out.WriteString(s)
			return out.String()
		}
		out.WriteString(s[:i])
		rest := s[i+len(prefix):]
		before, after, ok := strings.Cut(rest, suffix)
		if !ok {
			// unterminated — pass through verbatim
			out.WriteString(prefix)
			s = rest
			continue
		}
		arg := strings.TrimSpace(before)
		if n, err := strconv.Atoi(arg); err == nil {
			out.WriteString(mapFn(n))
		} else {
			out.WriteString(prefix)
			out.WriteString(before)
			out.WriteString(suffix)
		}
		s = after
	}
}

// ValidateProvidesTemplate checks that only known placeholders are present.
// Allowed:
//
//	{{.ContainerName}}
//	{{.ContainerPort <N>}}   N must parse as a positive integer
//	{{.HostPort <N>}}        N must parse as a positive integer
func ValidateProvidesTemplate(tmpl string) bool {
	stripped := strings.ReplaceAll(tmpl, "{{.ContainerName}}", "")
	stripped = stripPortTemplate(stripped, "{{.ContainerPort ", "}}")
	stripped = stripPortTemplate(stripped, "{{.HostPort ", "}}")
	return !strings.Contains(stripped, "{{") && !strings.Contains(stripped, "}}")
}

// stripPortTemplate removes every well-formed `<prefix><N><suffix>`
// occurrence where N is a numeric argument. Unterminated or non-numeric
// placeholders are LEFT IN — the outer validator's `{{`/`}}` substring
// check then catches them as invalid.
func stripPortTemplate(s, prefix, suffix string) string {
	var out strings.Builder
	for {
		i := strings.Index(s, prefix)
		if i < 0 {
			out.WriteString(s)
			return out.String()
		}
		out.WriteString(s[:i])
		rest := s[i+len(prefix):]
		before, after, ok := strings.Cut(rest, suffix)
		if !ok {
			out.WriteString(prefix)
			s = rest
			continue
		}
		arg := strings.TrimSpace(before)
		if _, err := strconv.Atoi(arg); err != nil {
			// non-numeric — leave verbatim so the outer check catches it
			out.WriteString(prefix)
			out.WriteString(before)
			out.WriteString(suffix)
		}
		// numeric N — drop the whole placeholder
		s = after
	}
}

// PortMapFromMappings builds a {containerPort -> hostPort} lookup table
// from the resolved port mapping list. Mappings that don't parse are
// silently skipped (the loud-skip warning lives in kit.CheckPortAvailability).
func PortMapFromMappings(mappings []string) map[int]int {
	if len(mappings) == 0 {
		return nil
	}
	m := make(map[int]int, len(mappings))
	for _, mapping := range mappings {
		p, ok := kit.ParsePortMapping(mapping)
		if !ok {
			continue
		}
		m[p.Container] = p.Host
	}
	return m
}
