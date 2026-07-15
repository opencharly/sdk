package deploykit

import (
	"fmt"
	"strings"

	"github.com/opencharly/sdk/spec"
)

// HomeToken is the deferred-home placeholder resolved by ResolveHome at emit time.
const HomeToken = "{{.Home}}"

func ScopeFromName(name string) Scope {
	switch name {
	case "user":
		return ScopeUser
	case "user-profile":
		return ScopeUserProfile
	default:
		return ScopeSystem
	}
}

// WireView projects the rich in-core spec.InstallPlan onto the JSON-roundtrippable
// spec.InstallPlanView the host marshals into an external deploy/step provider's
// op.Params. The Steps interface slice round-trips through the SINGLE StepsToView /
// stepsFromView converter (step_view.go) — an external deploy/step plugin walks the
// same ordered step IR the in-proc DeployTargets walk and EXECUTES it on the venue (R3;
// proven by the step-IR round-trip test). The remaining fields are identity + provenance.
//
// A free function (not a method on spec.InstallPlan) because it type-switches the
// concrete step vocabulary via StepsToView — that lives in deploykit, so spec never
// imports deploykit.
func WireView(p *spec.InstallPlan) spec.InstallPlanView {
	if p == nil {
		return spec.InstallPlanView{}
	}
	return spec.InstallPlanView{
		DeployID:        p.DeployID,
		Box:             p.Box,
		Version:         p.Version,
		Distro:          p.Distro,
		Candy:           p.Candy,
		CandiesIncluded: p.CandiesIncluded,
		AddCandies:      p.AddCandies,
		BuilderImage:    p.BuilderImage,
		Meta:            p.Meta,
		Steps:           StepsToView(p.Steps),
	}
}

// PlanFromView re-materializes the rich in-core *spec.InstallPlan from its JSON-roundtrippable
// spec.InstallPlanView wire form — the REVERSE of WireView, used by the host to reconstruct
// []*InstallPlan from the command:bundle plugin's OpCompile reply (K4-B). Steps round-trip
// through the SINGLE stepsFromView converter (step_view.go), already proven round-trip-faithful
// by TestStepView_RoundTrip. The host re-materialized plan is byte-equivalent to the former
// in-proc compile output (the K4-B parity golden proves it via DeepEqual against the OLD
// host-compile path).
func PlanFromView(v spec.InstallPlanView) (*spec.InstallPlan, error) {
	steps, err := stepsFromView(v.Steps)
	if err != nil {
		return nil, fmt.Errorf("re-materialize plan %q: %w", v.Candy, err)
	}
	return &spec.InstallPlan{
		DeployID:        v.DeployID,
		Box:             v.Box,
		Version:         v.Version,
		Distro:          v.Distro,
		Candy:           v.Candy,
		CandiesIncluded: v.CandiesIncluded,
		AddCandies:      v.AddCandies,
		BuilderImage:    v.BuilderImage,
		Meta:            v.Meta,
		Steps:           steps,
	}, nil
}

// ResolveHome substitutes the deferred HomeToken with a concrete home in
// every home-bearing step field, in place. Each DeployTarget calls this once
// at emit time with the home of its real destination: img.Home for the
// OCI/pod-overlay build, the host home for the external local deploy, the GUEST home
// (SSH executor ResolveHome) for the external vm deploy. Idempotent — fields without
// the token are left untouched, so a second call is a no-op.
//
// Covered fields: ShellHookStep env values + PathAdd, ShellSnippetStep Snippet
// + Destination + PathAppend, FileStep.Dest. OpStep cmd/content bodies are
// intentionally NOT touched — `~`/`$HOME` there shell-expand at runtime on the
// destination as the deploy user, which is already correct on every venue.
// BuilderStep is also untouched — its home is resolved separately by
// renderBuilderScript against the builder/guest home (see execBuilder).
//
// A free function (not a method on spec.InstallPlan) because it type-switches the
// concrete step vocabulary, which lives in deploykit.
func ResolveHome(p *spec.InstallPlan, home string) {
	if p == nil || home == "" {
		return
	}
	sub := func(s string) string { return strings.ReplaceAll(s, HomeToken, home) }
	for _, step := range p.Steps {
		switch s := step.(type) {
		case *ShellHookStep:
			for k, v := range s.EnvVars {
				s.EnvVars[k] = sub(v)
			}
			for i, pth := range s.PathAdd {
				s.PathAdd[i] = sub(pth)
			}
		case *ShellSnippetStep:
			s.Snippet = sub(s.Snippet)
			s.Destination = sub(s.Destination)
			for i, pth := range s.PathAppend {
				s.PathAppend[i] = sub(pth)
			}
		case *FileStep:
			s.Dest = sub(s.Dest)
		case *ServiceCustomStep:
			// The systemd unit is pre-rendered at compile with {{.Home}} for
			// host/vm targets (see compileServiceSteps); resolve it — and the
			// user-scope unit install path — against the destination home here.
			s.UnitText = sub(s.UnitText)
			s.UnitPath = sub(s.UnitPath)
		case *OpStep:
			// Home-relative copy/download dest (tokenized at compile). The
			// Task body itself (cmd/content) is left alone — those shell-expand
			// $HOME at runtime as the deploy user.
			s.To = sub(s.To)
		}
	}
}

// GateEnabled returns whether the given gate is permitted under opts.
// GateNone is always enabled; named gates require the corresponding
// opt-in flag.
func GateEnabled(g Gate, opts EmitOpts) bool {
	switch g {
	case GateNone:
		return true
	case GateAllowRepoChanges:
		return opts.AllowRepoChanges || opts.AssumeYes
	case GateAllowRootTasks:
		return opts.AllowRootTasks || opts.AssumeYes
	case GateWithServices:
		return opts.WithServices || opts.AssumeYes
	}
	return false
}

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
