package deploykit

// compile_service_steps.go — K5-A item 1 increment B (compile-seam ctx-threading):
// replaces the core-init()-set deploykit.CompileServiceSteps func var with an explicit
// ctx/exec-threaded function, mirroring compile_construct_step.go's constructOpStep.
//
// The filtering/scope/packaged-vs-custom decision logic is FULLY PORTABLE (a pure
// function of the candy's service: list + the target distro/init) and lives here
// directly. ONLY rendering a systemd CUSTOM entry's unit text needs the host: the
// former charly/service_render.go:RenderService wraps TWO registry consults a plugin
// cannot do itself (candy/plugin-init's OpResolve + the M16 egress gate), so that ONE
// case reaches back via the "render-service" HostBuild seam
// (spec.RenderServiceRequest/Reply, sdk/schema/seam.cue) — the packaged-unit case and
// the supervisord case never touch the wire at all.
//
// The former LoadBuildConfigForBox fallback (a per-call lazy lookup "for a caller that
// compiles outside the deploy-compile seam") is DELETED, not ported: it was already
// dead in production (candy/plugin-bundle's compileDeployPlans always receives
// hostCtx.ActiveInit pre-resolved by charly/bundle_compile_seam.go's
// preresolveActiveInitInto BEFORE calling BuildDeployPlan) and every test exercising
// it relied on it FAILING (an empty temp dir with no charly.yml) — never on it
// succeeding. Preferring the preresolved ActiveInit is therefore the ONLY path now,
// matching what production always did.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/opencharly/sdk"
	"github.com/opencharly/sdk/spec"
)

// renderServiceSeamKind names the HostBuild seam charly/host_build_render_service.go
// serves.
const renderServiceSeamKind = "render-service"

// CompileServiceSteps lowers a candy's service: list into install steps.
func CompileServiceSteps(ctx context.Context, ex *sdk.Executor, layer CandyModel, img *ResolvedBox, hostCtx HostContext) ([]InstallStep, error) {
	var out []InstallStep
	initIsSystemd := hostCtx.MachineVenue
	distros := ServiceRenderDistros(img, hostCtx)

	// Detect mixed-entry pairs: which names have a use_packaged form? Only
	// entries that APPLY to this target's distro count — a Fedora/Arch-only
	// packaged form must not suppress a Debian/Ubuntu exec sibling of the same
	// name (see ServiceEntryAppliesToDistro).
	namesWithPackaged := map[string]bool{}
	for i := range layer.Service() {
		if layer.Service()[i].IsPackaged() && ServiceEntryAppliesToDistro(&layer.Service()[i], distros) {
			namesWithPackaged[layer.Service()[i].Name] = true
		}
	}

	// Service home, like shell-snippet home, must be the DESTINATION user's home —
	// not the build host's. For host/vm deploys defer it via the {{.Home}} token
	// (InstallPlan.ResolveHome substitutes the real guest/host home at emit); for a
	// container-systemd build the image's resolved Home is the runtime home.
	renderCtx := spec.ServiceRenderContext{Candy: layer.GetName(), SystemUnitDir: "/etc/systemd/system"}
	svcHome := img.Home
	if hostCtx.MachineVenue {
		svcHome = HomeToken
	}
	if svcHome != "" {
		renderCtx.Home = svcHome
		renderCtx.UserUnitDir = svcHome + "/.config/systemd/user"
	}

	for i := range layer.Service() {
		entry := &layer.Service()[i]
		// Per-distro filter: an entry with a distro: list renders only on the
		// named distros (see ServiceEntryAppliesToDistro).
		if !ServiceEntryAppliesToDistro(entry, distros) {
			continue
		}
		scope := spec.ScopeSystem
		if entry.EffectiveScope() == "user" {
			scope = spec.ScopeUser
		}

		if entry.IsPackaged() {
			// supervisord can't consume systemd packaged units.
			if !initIsSystemd {
				continue
			}
			out = append(out, &ServicePackagedStep{
				Unit:        EnsureServiceSuffix(entry.UsePackaged),
				TargetScope: scope,
				Enable:      entry.Enable,
				CandyName:   layer.GetName(),
			})
			continue
		}

		// Custom-exec entry. On systemd targets, if a same-name use_packaged
		// sibling exists, the packaged form wins — skip the custom entry
		// entirely (mixed-pair polymorphism).
		if initIsSystemd && namesWithPackaged[entry.Name] {
			continue
		}

		step := &ServiceCustomStep{
			Name:        fmt.Sprintf("charly-%s-%s", layer.GetName(), entry.Name),
			TargetScope: scope,
			Enable:      entry.Enable,
			CandyName:   layer.GetName(),
		}

		// On systemd targets, pre-render the unit text now so the executor
		// doesn't need a lazy fallback. On supervisord targets, the supervisord
		// init pipeline renders its own fragment — leave UnitText empty.
		if initIsSystemd && hostCtx.ActiveInitName == "systemd" && hostCtx.ActiveInit != nil {
			entryClone := *entry
			entryClone.Name = step.Name
			rendered, rerr := renderServiceViaSeam(ctx, ex, &entryClone, hostCtx.ActiveInit, renderCtx)
			if rerr == nil && rendered != nil {
				step.UnitText = rendered.UnitText
				step.UnitPath = rendered.UnitPath
			}
		}

		out = append(out, step)
	}
	return out, nil
}

// renderServiceViaSeam renders a ServiceEntry into a RenderedService via the
// "render-service" HostBuild seam — the ONE genuinely host-only piece of the former
// charly/service_render.go:RenderService (candy/plugin-init's OpResolve + the M16
// egress gate). A nil def or nil ex is a caller bug (CompileServiceSteps only calls
// this when both are already known non-nil); returns a clear error rather than
// panicking.
func renderServiceViaSeam(ctx context.Context, ex *sdk.Executor, entry *spec.ServiceEntry, def *spec.ResolvedInit, rctx spec.ServiceRenderContext) (*spec.RenderedService, error) {
	if entry == nil {
		return nil, fmt.Errorf("renderServiceViaSeam: nil entry")
	}
	if def == nil {
		return nil, fmt.Errorf("renderServiceViaSeam: nil init def")
	}
	if ex == nil {
		return nil, fmt.Errorf("render-service: no host reverse channel (command not compiled-in?)")
	}
	reqJSON, err := json.Marshal(spec.RenderServiceRequest{Entry: *entry, Init: *def, Ctx: rctx})
	if err != nil {
		return nil, fmt.Errorf("render-service: marshal request: %w", err)
	}
	resJSON, err := ex.HostBuild(ctx, renderServiceSeamKind, reqJSON)
	if err != nil {
		return nil, fmt.Errorf("render-service: %w", err)
	}
	var reply spec.RenderServiceReply
	if len(resJSON) > 0 {
		if err := json.Unmarshal(resJSON, &reply); err != nil {
			return nil, fmt.Errorf("render-service: decode reply: %w", err)
		}
	}
	return reply.Rendered, nil
}
