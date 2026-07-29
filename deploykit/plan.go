package deploykit

// plan.go — thin re-exports of the plan-level IR mechanisms + the local
// ExtractStringSlice helper.
//
// HomeToken + WireView / PlanFromView / ResolveHome / GateEnabled RELOCATED to
// github.com/opencharly/spec/spec (install_plan_view.go, #55 step-4) — they type-switch
// the concrete step vocabulary, which now lives in spec, so spec owns the whole in-proc
// IR + its mechanisms. deploykit's own code + out-of-tree plugins read the aliases
// unchanged. ScopeFromName likewise lives in spec (spec.ScopeFromName).

import "github.com/opencharly/spec/spec"

// HomeToken is the deferred-home placeholder resolved by ResolveHome at emit time.
const HomeToken = spec.HomeToken

var (
	WireView     = spec.WireView
	PlanFromView = spec.PlanFromView
	ResolveHome  = spec.ResolveHome
	GateEnabled  = spec.GateEnabled
)

// ExtractStringSlice returns m[key] as []string or nil if absent.
// Accepts []string and []interface{} (as produced by yaml.v3) inputs.
func ExtractStringSlice(m map[string]any, key string) []string {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok {
		return nil
	}
	switch t := v.(type) {
	case []string:
		out := make([]string, len(t))
		copy(out, t)
		return out
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
