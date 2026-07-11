package deploykit

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/spec"
)

// WriteCandySteps writes the RUN steps for a single candy (the per-candy render
// call-graph: ENV/packages/localpkg/tasks/builders/shell/user-reset sharing the
// asUser state). Relocated from charly (P8); byte-identical. skipRootReset
// prevents emitting USER root after user-mode steps (the last candy when no
// post-candy root steps follow); returns true if the candy ended in user mode.
//
//nolint:gocyclo // candy-build step sequence; conditionally ordered, cohesive
func (g *Generator) WriteCandySteps(b *strings.Builder, candyName string, img *buildkit.ResolvedBox, skipRootReset bool) (bool, error) {
	layer := g.Candies[candyName]
	stageName := layer.GetName() // short name used as scratch stage alias

	fmt.Fprintf(b, "# Layer: %s\n", candyName)

	// Track if we've switched to user mode
	asUser := false

	// 0. ENV from vars: + ARCH (ARG TARGETARCH) — emitted once per candy
	// before packages/tasks so Docker's variable substitution sees the
	// values in subsequent directives (COPY dests, RUN commands, etc.).
	if layer.HasTasks() || len(layer.Vars()) > 0 {
		EmitVarsEnv(b, layer.Vars())
	}

	// 1. System packages — resolved by THE shared distro-specificity cascade
	// (ResolveCascadePackages — the SAME resolver the deploy path uses, so build
	// and deploy can never diverge). It folds the candy's top-level `package:`
	// base, unions every matching distro tag section over img.Distro, and
	// resolves repo/options/etc. most-specific-wins. Emits the PRIMARY format
	// (img.Pkg) install; non-primary build formats (the `aur` builder) emit from
	// their own format section below.
	if pkgs, raw, matched := ResolveCascadePackages(layer, img); (matched || len(pkgs) > 0) && img.DistroDef != nil {
		if formatDef := img.DistroDef.Format[img.Pkg]; formatDef != nil {
			ctx := buildkit.NewInstallContext(raw, formatDef.CacheMount)
			if rendered, err := buildkit.RenderTemplate(img.Pkg+"-install", formatDef.InstallTemplate, ctx); err == nil {
				b.WriteString(rendered)
			}
		}
	}

	// Non-primary build formats (e.g. `aur` for build: [pac, aur]) are secondary
	// BUILD formats consumed by a multi-stage builder, NOT distro package tags, so
	// they emit from their own format section (the cascade above owns img.Pkg).
	for _, format := range img.BuildFormats {
		if format == img.Pkg {
			continue // primary format handled by the cascade above
		}
		section := layer.FormatSection(format)
		if section == nil || len(section.Packages) == 0 {
			continue
		}
		if img.DistroDef == nil || img.DistroDef.Format == nil {
			continue
		}
		formatDef := img.DistroDef.Format[format]
		if formatDef == nil {
			continue
		}
		if builderDef, ok := img.BuilderConfig.Builder[format]; ok && !builderDef.Inline {
			// Format with builder: use the format's install_template (e.g., aur COPY + pacman -U)
			ctx := &spec.InstallContext{
				CacheMounts: formatDef.CacheMount,
				Packages:    section.Packages,
				StageName:   fmt.Sprintf("%s-%s-build", layer.GetName(), format),
			}
			if rendered, err := buildkit.RenderTemplate(format+"-install", formatDef.InstallTemplate, ctx); err == nil {
				b.WriteString(rendered)
			}
		} else {
			ctx := buildkit.NewInstallContext(section.Raw, formatDef.CacheMount)
			if rendered, err := buildkit.RenderTemplate(format+"-install", formatDef.InstallTemplate, ctx); err == nil {
				b.WriteString(rendered)
			}
		}
	}

	// 2.5 localpkg: install the candy's OS package in the IMAGE build — the same
	// dep-resolving install the deploy LocalPkgInstallStep performs, so a localpkg
	// candy (the `charly` toolchain) installs as a proper, OS-tracked,
	// dependency-resolving package on EVERY distro image, not a curl'd raw binary.
	// CompileLocalPkgStep resolves the target distro's localpkg-capable format and
	// the candy's source; RenderLocalPkgImageInstall (shared with OCITarget — R3)
	// then picks the binary source by box type: a PRODUCTION box downloads the
	// PUBLISHED release; a DISPOSABLE check bed (g.DevLocalPkg) builds the
	// IN-DEVELOPMENT package from local source. Emits "" when the format declares
	// no localpkg contract (the candy's own task: install is the fallback).
	if step := CompileLocalPkgStep(layer, img, HostContext{}); step != nil {
		if s, ok := step.(*LocalPkgInstallStep); ok {
			run, err := g.RenderLocalPkgImageInstall(s, g.DevLocalPkg, filepath.Join(g.BuildDir, img.Name), img.Name)
			if err != nil {
				// RenderLocalPkgImageInstall's contract is fail-loudly, never a
				// silent fallback. Emitting the error as a Containerfile COMMENT
				// (the old behavior) and continuing MASKED the failure: a
				// --dev-local-pkg build whose makepkg failed shipped an image with
				// NO /usr/bin/charly, only surfacing downstream as failing charly
				// checks (the check-charly-selftest-pod defect). Hard-error here.
				return asUser, fmt.Errorf("candy %q: rendering localpkg install for image %q (dev=%v): %w", candyName, img.Name, g.DevLocalPkg, err)
			}
			b.WriteString(run)
		}
	}

	// 2a. tasks: list (new path — replaces both root.yml and user.yml).
	// Validator rejects candies that have both tasks: and root.yml/user.yml.
	if layer.HasTasks() {
		boxName := img.Name
		buildDir := filepath.Join(g.BuildDir, boxName)
		contextRelPrefix := filepath.ToSlash(filepath.Join(".build", boxName))
		finalUser, err := g.EmitTasks(b, layer, img, layer.RunOps(), buildDir, contextRelPrefix)
		if err != nil {
			// Phase 0: log but continue; validator should catch this earlier.
			fmt.Fprintf(b, "# emitTasks error: %v\n", err)
		}
		if finalUser != "0" && finalUser != "root" {
			// Tasks ended in non-root state; reset for builders/user.yml that
			// follow in existing code paths (they assume USER=root at entry).
			b.WriteString("USER root\n")
		}
	}

	// 4. Inline builders (cargo, etc.) — an EXTERNALIZED inline builder (cargo) renders its
	// in-candy RUN via its plugin's OpResolve build leg (C10, InlineFragment); a custom
	// (non-externalized) inline builder renders from the embedded builder: vocabulary
	// install_template.
	if img.BuilderConfig != nil {
		for _, bName := range img.BuilderConfig.BuilderNames() {
			bDef := img.BuilderConfig.Builder[bName]
			if !bDef.Inline {
				continue
			}
			external := g.ExternalizedBuilders[bName]
			if !external && bDef.InstallTemplate == "" {
				continue
			}
			if !g.CandyNeedsBuilder(img, layer, bDef) {
				continue
			}
			if !asUser {
				fmt.Fprintf(b, "USER %d\n", img.UID)
				asUser = true
			}
			ctx := &spec.BuildStageContext{
				LayerStage:  stageName,
				UID:         img.UID,
				GID:         img.GID,
				CacheMounts: bDef.CacheMount,
			}
			if external {
				frag, err := g.ResolveInlineBuilder(candyName, bName, bDef, ctx, img)
				if err != nil {
					return asUser, err
				}
				b.WriteString(frag)
				continue
			}
			rendered, err := buildkit.RenderTemplate(bName+"-inline", bDef.InstallTemplate, ctx)
			if err != nil {
				return asUser, fmt.Errorf("candy %q: rendering inline builder %q: %w", candyName, bName, err)
			}
			b.WriteString(rendered)
		}
	}

	// 5. Shell-init snippets from the candy manifest `shell:` block.
	// Reuses the InstallPlan compiler so the box-build generator and the
	// pod-overlay path share one source of truth for selection-rule +
	// destination resolution. We emit the resulting steps inline as
	// RUN-heredoc directives (parallel to how candy/plugin-installstep's
	// shell-snippet OpEmit renders the pod-overlay fragment — same
	// sha256-derived end-marker).
	if shellSteps := CompileShellSnippetSteps(layer, img, HostContext{}); len(shellSteps) > 0 {
		// Shell snippets are root-owned system drop-ins; reset to root
		// before emission so RUN runs as root.
		if asUser {
			b.WriteString("USER root\n")
			asUser = false
		}
		for _, step := range shellSteps {
			s, ok := step.(*ShellSnippetStep)
			if !ok || s == nil || s.Snippet == "" {
				continue
			}
			h := sha256.Sum256([]byte(s.Snippet))
			marker := fmt.Sprintf("CHARLY_SHELL_%s_%x", strings.ToUpper(s.Shell), h[:4])
			fmt.Fprintf(b,
				"RUN mkdir -p %s && cat > %s <<'%s'\n%s\n%s\n",
				shellQuote(filepath.Dir(s.Destination)),
				shellQuote(s.Destination),
				marker,
				s.Snippet,
				marker,
			)
		}
	}

	// Reset to root for next candy (skip for last candy when no root steps follow)
	if asUser && !skipRootReset {
		b.WriteString("USER root\n")
	}

	b.WriteString("\n")
	return asUser, nil
}
