package deploykit

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
)

// SystemEnableContext is the render context for an init system's
// system_enable_template (the distro-shipped units to enable). Relocated (P8).
type SystemEnableContext struct {
	Units []string
}

// InitRenderAssemblyTemplate renders an init system's assembly_template (the step
// that assembles contributed unit fragments). Relocated from charly (P8).
func InitRenderAssemblyTemplate(def *spec.ResolvedInit) (string, error) {
	if def.AssemblyTemplate == "" {
		return "", nil
	}
	return buildkit.RenderTemplate("assembly", def.AssemblyTemplate, nil)
}

// InitRenderSystemEnableTemplate renders the system_enable_template for the given
// distro-shipped units. Relocated from charly (P8).
func InitRenderSystemEnableTemplate(def *spec.ResolvedInit, units []string) (string, error) {
	if def.SystemEnableTemplate == "" || len(units) == 0 {
		return "", nil
	}
	ctx := SystemEnableContext{Units: units}
	return buildkit.RenderTemplate("system-enable", def.SystemEnableTemplate, ctx)
}

// InitRenderPostAssemblyTemplate renders the post_assembly_template (e.g. bootc
// container lint). Relocated from charly (P8).
func InitRenderPostAssemblyTemplate(def *spec.ResolvedInit) (string, error) {
	if def.PostAssemblyTemplate == "" {
		return "", nil
	}
	return buildkit.RenderTemplate("post-assembly", def.PostAssemblyTemplate, nil)
}

// RelayContext is the template context for relay_template rendering.
type RelayContext struct {
	Port      int
	CandyName string
	Index     int
}

// StageFragmentContext is the template context for stage_fragment_copy rendering.
type StageFragmentContext struct {
	BoxName     string
	FragmentDir string
	FileName    string
}

// InitHasRelayTemplate reports whether this init definition has a relay template.
// Relocated from charly (P8).
func InitHasRelayTemplate(def *spec.ResolvedInit) bool {
	return def.RelayTemplate != ""
}

// InitRenderStageFragmentCopy renders the stage_fragment_copy template.
// Relocated from charly (P8).
func InitRenderStageFragmentCopy(def *spec.ResolvedInit, boxName, fileName string) (string, error) {
	if def.StageFragmentCopy == "" {
		return "", nil
	}
	ctx := StageFragmentContext{
		BoxName:     boxName,
		FragmentDir: def.FragmentDir,
		FileName:    fileName,
	}
	return buildkit.RenderTemplate("stage-fragment-copy", def.StageFragmentCopy, ctx)
}

// InitRenderRelayTemplate renders the relay_template for a port relay.
// Relocated from charly (P8).
func InitRenderRelayTemplate(def *spec.ResolvedInit, port int, candyName string, index int) (string, error) {
	if def.RelayTemplate == "" {
		return "", fmt.Errorf("init system has no relay_template")
	}
	ctx := RelayContext{Port: port, CandyName: candyName, Index: index}
	result, err := buildkit.RenderTemplate("relay", def.RelayTemplate, ctx)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return result, nil
}

// MapToKeyValueSlice deterministically sorts a map into []spec.KeyValue for
// template iteration. Matches the ServiceRenderContext contract. Relocated (P8).
func MapToKeyValueSlice(m map[string]string) []spec.KeyValue {
	if len(m) == 0 {
		return nil
	}
	out := make([]spec.KeyValue, 0, len(m))
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, spec.KeyValue{Key: k, Value: m[k]})
	}
	return out
}

// EmitInitAssembly emits the assembly_template, system_enable_template, and
// post_assembly_template RUN steps for each active init system. Relocated from
// charly (P8); byte-identical. initHasFragments gates the assembly step (a
// fragment-less init contributed no scratch stage to bind-mount from).
func (g *Generator) EmitInitAssembly(b *strings.Builder, img *buildkit.ResolvedBox, candyOrder []string, activeInits map[string]*spec.ResolvedInit, initHasFragments map[string]bool) error {
	for initName, def := range activeInits {
		// assembly_template bind-mounts from the scratch stage emitted above;
		// skip it when no fragments were contributed (stage was not emitted).
		// system_enable_template and post_assembly_template are independent
		// and still run below.
		if initHasFragments[initName] {
			assembly, err := InitRenderAssemblyTemplate(def)
			if err != nil {
				return fmt.Errorf("rendering assembly for %s: %w", initName, err)
			}
			if assembly != "" {
				b.WriteString(assembly)
				if !strings.HasSuffix(assembly, "\n") {
					b.WriteString("\n")
				}
				b.WriteString("\n")
			}
		}

		// System-level service enablement (e.g., systemctl enable sshd).
		// Collect every use_packaged: entry across the candy chain — these
		// are the distro-shipped systemd units the init system must enable.
		var systemUnits []string
		for _, candyName := range candyOrder {
			layer := g.Candies[candyName]
			for i := range layer.Service() {
				entry := &layer.Service()[i]
				if entry.IsPackaged() && entry.EffectiveScope() == "system" &&
					ServiceEntryAppliesToDistro(entry, img.Distro) {
					systemUnits = append(systemUnits, entry.UsePackaged)
				}
			}
		}
		sysEnable, err := InitRenderSystemEnableTemplate(def, systemUnits)
		if err != nil {
			return fmt.Errorf("rendering system enable for %s: %w", initName, err)
		}
		if sysEnable != "" {
			b.WriteString(sysEnable)
			if !strings.HasSuffix(sysEnable, "\n") {
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}

		// Post-assembly step (e.g., bootc container lint)
		postAssembly, err := InitRenderPostAssemblyTemplate(def)
		if err != nil {
			return fmt.Errorf("rendering post-assembly for %s: %w", initName, err)
		}
		if postAssembly != "" {
			b.WriteString(postAssembly)
			if !strings.HasSuffix(postAssembly, "\n") {
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
	}
	return nil
}

// EmitInitFragmentStages emits the per-init scratch stages that COPY service
// fragments, relay configs, and detected service files, and returns the per-init
// map of whether any fragment content was contributed (consumed by
// EmitInitAssembly). Relocated from charly (P8); byte-identical.
func (g *Generator) EmitInitFragmentStages(b *strings.Builder, boxName string, img *buildkit.ResolvedBox, candyOrder []string, activeInits map[string]*spec.ResolvedInit) (map[string]bool, error) {
	initHasFragments := map[string]bool{}
	for initName, def := range activeInits {
		initCandyOrder := candyOrder
		if !img.IsExternalBase {
			full := CollectAllBoxCandies(boxName, g.Boxes, g.Candies)
			if len(full) > 0 {
				initCandyOrder = full
			}
		}
		if err := g.GenerateInitFragments(boxName, initName, def, initCandyOrder); err != nil {
			return nil, err
		}

		// Pre-scan the candy chain to decide whether this init has any fragment
		// content. If not, skip both the scratch stage emission and the
		// assembly_template RUN (see EmitInitAssembly).
		hasFragments := false
		for _, candyName := range initCandyOrder {
			layer := g.Candies[candyName]
			if def.Model == "fragment_assembly" && layer.HasInit(initName) {
				hasFragments = true
				break
			}
			if InitHasRelayTemplate(def) && len(layer.RelayPorts()) > 0 {
				hasFragments = true
				break
			}
			if def.Model == "file_copy" && len(layer.ServiceFiles()) > 0 {
				hasFragments = true
				break
			}
		}
		initHasFragments[initName] = hasFragments
		if !hasFragments {
			continue
		}

		// Emit scratch stage with COPY lines for fragments
		fmt.Fprintf(b, "FROM scratch AS %s\n", def.StageName)
		if def.StageHeaderCopy != "" {
			headerCopy, err := g.RewriteHeaderCopyForRemote(def.StageHeaderCopy)
			if err != nil {
				return nil, err
			}
			b.WriteString(headerCopy + "\n")
		}
		for i, candyName := range initCandyOrder {
			layer := g.Candies[candyName]
			// Service content fragments (fragment_assembly model)
			if def.Model == "fragment_assembly" && layer.HasInit(initName) {
				// Use the SHORT name (not the map key) — a remote candy's key is
				// a slashed github ref that would create bogus nested dirs.
				fileName := fmt.Sprintf("%02d-%s.conf", i+1, layer.GetName())
				copyLine, err := InitRenderStageFragmentCopy(def, boxName, fileName)
				if err != nil {
					return nil, fmt.Errorf("rendering stage fragment copy for %s/%s: %w", initName, candyName, err)
				}
				b.WriteString(copyLine + "\n")
			}
			// Relay fragments
			if InitHasRelayTemplate(def) && len(layer.RelayPorts()) > 0 {
				for _, port := range layer.RelayPorts() {
					confName := fmt.Sprintf("%02d-relay-%d.conf", i+1, port)
					copyLine, err := InitRenderStageFragmentCopy(def, boxName, confName)
					if err != nil {
						return nil, fmt.Errorf("rendering relay copy for %s/%s port %d: %w", initName, candyName, port, err)
					}
					b.WriteString(copyLine + "\n")
				}
			}
			// File copy model: copy detected service files
			if def.Model == "file_copy" && len(layer.ServiceFiles()) > 0 {
				for _, svcPath := range layer.ServiceFiles() {
					svcName := filepath.Base(svcPath)
					copyLine, err := InitRenderStageFragmentCopy(def, boxName, svcName)
					if err != nil {
						return nil, fmt.Errorf("rendering service file copy for %s/%s: %w", initName, candyName, err)
					}
					b.WriteString(copyLine + "\n")
				}
			}
		}
		b.WriteString("\n")
	}
	return initHasFragments, nil
}

// GenerateInitFragments writes init system config fragments to
// .build/<image>/<fragmentDir>/. Schema-driven: iterates each candy's service:
// list and renders every entry that binds to this init via per-entry routing
// (use_packaged → systemd; custom exec → any init with a service_template).
// Relocated from charly (P8); byte-identical. The service render itself crosses
// to candy/plugin-init via the RenderService seam.
func (g *Generator) GenerateInitFragments(boxName, initName string, def *spec.ResolvedInit, candyOrder []string) error {
	fragDir := filepath.Join(g.BuildDir, boxName, def.FragmentDir)
	if err := os.MkdirAll(fragDir, 0755); err != nil {
		return err
	}
	img := g.Boxes[boxName]

	for i, candyName := range candyOrder {
		layer := g.Candies[candyName]
		idx := i + 1

		if def.Model == "fragment_assembly" {
			// Concatenate every service entry in this candy that binds to this init
			// into ONE fragment file per candy, matching the Containerfile's
			// stage_fragment_copy naming convention (NN-<candy>.conf).
			var candyBuf strings.Builder
			for j := range layer.Service() {
				entry := &layer.Service()[j]
				// Per-distro filter: skip entries whose distro: list excludes
				// this box's distro (the modular virtqemud/virtnetworkd vs
				// monolithic libvirtd split — see ServiceEntryAppliesToDistro).
				if img != nil && !ServiceEntryAppliesToDistro(entry, img.Distro) {
					continue
				}
				// Per-entry routing: only render entries this init can handle.
				if entry.IsPackaged() {
					if def.ServiceSchema == nil || !def.ServiceSchema.SupportsPackaged {
						continue
					}
				} else {
					if def.ServiceSchema == nil || def.ServiceSchema.ServiceTemplate == "" {
						continue
					}
				}
				ctx := spec.ServiceRenderContext{
					Name:             entry.Name,
					Candy:            candyName,
					Exec:             entry.Exec,
					Env:              entry.Env,
					EnvList:          MapToKeyValueSlice(entry.Env),
					Restart:          entry.Restart,
					WorkingDirectory: entry.WorkingDirectory,
					User:             entry.User,
					After:            entry.After,
					Before:           entry.Before,
					Stdout:           entry.Stdout,
					StopTimeout:      entry.StopTimeout,
					Scope:            entry.EffectiveScope(),
				}
				rendered, err := g.RenderService(entry, def, ctx)
				if err != nil {
					return fmt.Errorf("rendering service %s/%s/%s: %w", initName, candyName, entry.Name, err)
				}
				content := rendered.UnitText
				if content == "" {
					content = rendered.DropinText
				}
				if content == "" {
					continue
				}
				if candyBuf.Len() > 0 && !strings.HasSuffix(candyBuf.String(), "\n\n") {
					if !strings.HasSuffix(candyBuf.String(), "\n") {
						candyBuf.WriteString("\n")
					}
					candyBuf.WriteString("\n")
				}
				candyBuf.WriteString(content)
				if !strings.HasSuffix(content, "\n") {
					candyBuf.WriteString("\n")
				}
			}
			if candyBuf.Len() > 0 {
				// Short name, not the slashed remote map key (see scratch-stage note).
				fragFile := filepath.Join(fragDir, fmt.Sprintf("%02d-%s.conf", idx, layer.GetName()))
				if err := kit.AtomicWriteFile(fragFile, []byte(candyBuf.String()), 0644); err != nil {
					return err
				}
			}
		}

		// Port relay fragments (unchanged — use candy position in filename to
		// match Containerfile's stage_fragment_copy naming).
		if InitHasRelayTemplate(def) && len(layer.RelayPorts()) > 0 {
			for _, port := range layer.RelayPorts() {
				content, err := InitRenderRelayTemplate(def, port, candyName, idx)
				if err != nil {
					return fmt.Errorf("rendering relay for %s/%s port %d: %w", initName, candyName, port, err)
				}
				confName := fmt.Sprintf("%02d-relay-%d.conf", idx, port)
				fragFile := filepath.Join(fragDir, confName)
				if err := kit.AtomicWriteFile(fragFile, []byte(content), 0644); err != nil {
					return err
				}
			}
		}

		// File copy model: copy detected service files (systemd *.service globs).
		if def.Model == "file_copy" {
			for _, svcPath := range layer.ServiceFiles() {
				content, err := os.ReadFile(svcPath)
				if err != nil {
					return fmt.Errorf("reading service file %s: %w", svcPath, err)
				}
				destFile := filepath.Join(fragDir, filepath.Base(svcPath))
				if err := kit.AtomicWriteFile(destFile, content, 0644); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
