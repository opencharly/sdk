package kit

import (
	"regexp"
	"sort"
	"strings"

	"github.com/opencharly/sdk/spec"
)

// checkvars_expand.go — the ${NAME[:arg]} check-variable expansion grammar, shared with every
// plugin candy that runs a plan. The check SEMANTICS that consult the core VerbCatalog
// (EffectiveDo / EffectiveContexts / InContext) stay in package main; only this
// vocabulary-independent expansion + runtime-var classification lives here.

// TestVarRefPattern matches a ${NAME} or ${NAME:arg} reference (uppercase-underscore names).
var TestVarRefPattern = regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)(?::([^}]+))?\}`)

// ExpandTestVars substitutes ${NAME} and ${NAME:arg} references using the supplied
// environment map.
//
// Keys in env for plain refs use just the name: env["HOME"] = "/home/user". Keys for
// parameterized refs combine name and argument with a colon: env["HOST_PORT:6379"] = "16379",
// env["VOLUME_PATH:workspace"] = "/var/lib/…".
//
// Returns the substituted string and a list of unresolved refs (in encounter order,
// deduplicated). The caller decides whether unresolved refs are an error (build-time
// validation) or a skip reason (runtime).
func ExpandTestVars(s string, env map[string]string) (string, []string) {
	seen := map[string]bool{}
	var missing []string
	out := TestVarRefPattern.ReplaceAllStringFunc(s, func(match string) string {
		sub := TestVarRefPattern.FindStringSubmatch(match)
		name, arg := sub[1], sub[2]
		key := name
		if arg != "" {
			key = name + ":" + arg
		}
		if v, ok := env[key]; ok {
			return v
		}
		if !seen[key] {
			seen[key] = true
			missing = append(missing, key)
		}
		return match // leave unresolved refs visible in output
	})
	return out, missing
}

// TestVarRefs returns the set of ${NAME[:arg]} references in s, as their fully-qualified keys
// (matching the env-map format used by ExpandTestVars). Used by the validator to catch typos
// at config time.
func TestVarRefs(s string) []string {
	matches := TestVarRefPattern.FindAllStringSubmatch(s, -1)
	var out []string
	seen := map[string]bool{}
	for _, m := range matches {
		key := m[1]
		if m[2] != "" {
			key = m[1] + ":" + m[2]
		}
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

// RuntimeOnlyVarPrefixes lists variable name prefixes that are only resolvable against a
// running container. scope:"build" checks must not reference these.
var RuntimeOnlyVarPrefixes = []string{
	"HOST_PORT",
	"VOLUME_PATH",
	"VOLUME_CONTAINER_PATH",
	"CONTAINER_IP",
	"CONTAINER_NAME",
	"INSTANCE",
	"ENV_",
	// The step id is populated only at plan-run execution time, so it is effectively
	// runtime-only.
	"STEP_ID",
	// VM live-check intent: how many <hostdev> the VM's spec declares. Resolved only against a
	// live VM deployment (check_cmd.go VM path), so a build-scope check must not reference it.
	"VM_HOSTDEV_COUNT",
	// The sanitized deploy name of the deployment under check — the same value
	// K3sPostProvision uses for the kubeconfig context + ClusterProfile name, so a deploy-scope
	// k8s check can address its own cluster generically via cluster: "${DEPLOY_NAME}". Resolved
	// only against a live deployment.
	"DEPLOY_NAME",
	// Cross-member address var (check_members.go): the unified ${HOST:<member>} (+ optional
	// :port) lets a driven probe (a check with `on:`, or a sibling bundle member) reach a
	// SEPARATE member. Resolved only against running deployments, so a build-scope check must
	// not reference it.
	"HOST",
}

// IsRuntimeOnlyVar reports whether the given variable key (as returned by TestVarRefs) refers
// to a runtime-only value. The check matches on name prefix because parameterized vars share a
// common prefix with their arg.
func IsRuntimeOnlyVar(key string) bool {
	name := key
	if before, _, ok := strings.Cut(key, ":"); ok {
		name = before
	}
	for _, p := range RuntimeOnlyVarPrefixes {
		if name == p || strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// ExpandOpVars rewrites every ${...} reference on an Op in place using the supplied
// environment map. Returns the combined (sorted) list of unresolved refs encountered across
// all string fields.
func ExpandOpVars(c *spec.Op, env map[string]string) []string {
	seen := map[string]bool{}
	var missing []string
	record := func(unresolved []string) {
		for _, k := range unresolved {
			if !seen[k] {
				seen[k] = true
				missing = append(missing, k)
			}
		}
	}
	for _, p := range c.StringFields() {
		if *p == "" {
			continue
		}
		replaced, unresolved := ExpandTestVars(*p, env)
		*p = replaced
		record(unresolved)
	}
	// A plugin verb (http/interface/addr/process/port/dns/…) carries its authored fields in the
	// opaque PluginInput map, NOT in StringFields. Expand ${VAR} references in every string
	// within it so an http URL's / addr's ${HOST_PORT:N} (and any other runtime var) resolves
	// at runtime exactly as it did before the verb left #Op. The map analogue of the
	// StringFields walk; ONE generic path for every plugin verb (R3).
	if len(c.PluginInput) > 0 {
		_, unresolved := ExpandAnyVars(c.PluginInput, env)
		record(unresolved)
	}
	sort.Strings(missing)
	return missing
}

// CollectAnyStrings returns every string within a plugin_input value (scalar string / nested
// map / list), depth-first. The READ-ONLY analogue of ExpandAnyVars: it lets the ${HOST:…}
// cross-member scan (collectHostRefs) reach a plugin verb's authored fields, which live in the
// opaque PluginInput map rather than StringFields.
func CollectAnyStrings(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case map[string]any:
		var out []string
		for _, e := range x {
			out = append(out, CollectAnyStrings(e)...)
		}
		return out
	case []any:
		var out []string
		for _, e := range x {
			out = append(out, CollectAnyStrings(e)...)
		}
		return out
	default:
		return nil
	}
}

// ExpandAnyVars expands ${VAR} references in every string within a plugin_input value (scalar
// string / nested map / list), mutating maps and slices in place, and returns the (possibly
// rewritten) value plus the unresolved var names. Non-string scalars pass through untouched.
func ExpandAnyVars(v any, env map[string]string) (any, []string) {
	switch x := v.(type) {
	case string:
		return ExpandTestVars(x, env)
	case map[string]any:
		var missing []string
		for k, e := range x {
			ne, un := ExpandAnyVars(e, env)
			x[k] = ne
			missing = append(missing, un...)
		}
		return x, missing
	case []any:
		var missing []string
		for i, e := range x {
			ne, un := ExpandAnyVars(e, env)
			x[i] = ne
			missing = append(missing, un...)
		}
		return x, missing
	default:
		return v, nil
	}
}
