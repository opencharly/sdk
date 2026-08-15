package deploykit

import (
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// render_prep.go — the HOST-SIDE render-prep pass (#67 render-DRIVE move, relocated
// from charly/render_prep.go + charly/render_baked_metadata.go in K3-U3). Fills the
// build-render caches on *ResolvedBox by reading the scanned candy set (spec.CandyReader)
// + Config/InitConfig — the EXACT computations generateContainerfile previously inlined.
// The host (charly's toDeploykit) constructs a Generator carrying Config/InitConfig/Boxes/
// Candies/Dir and drives RenderPrepAll before the resolved-project envelope is projected;
// the projector copies the filled caches into the envelope (ResolvedBoxView.BakedMetadata/
// RenderCandyOrder/InitSystem/InitDef/ActiveInits/Caps). The plugin-build render Generator
// (NewRenderGeneratorFromProject) never runs this pass — it reads the pre-computed caches.

// RenderPrepBox fills the build-render caches on g.Boxes[boxName] by reading the
// live graph. Returns the first error (caps missing etc.).
func (g *Generator) RenderPrepBox(boxName string) error {
	img := g.Boxes[boxName]
	if img == nil {
		return fmt.Errorf("render-prep: box %q not found", boxName)
	}

	// 1. parentCandies (for GlobalOrderForBox).
	var parentCandies map[string]bool
	if !img.IsExternalBase {
		var err error
		parentCandies, err = CandyProvidedByBox(img.Base, g.Boxes, g.Candies)
		if err != nil {
			return err
		}
	}

	// 2. candyOrder — the per-box cache-optimal candy order.
	candyOrder, err := g.GlobalOrderForBox(img.Candy, parentCandies)
	if err != nil {
		return err
	}
	img.RenderCandyOrder = candyOrder

	// 3. caps — the candy-contributed capability surface.
	caps, capsErr := buildkit.AggregateCandyCapabilities(g.Candies, candyOrder)
	if capsErr != nil {
		return capsErr
	}
	img.CandyCaps = caps
	if missing := buildkit.CheckRequiredCapabilities(g.Candies, candyOrder, caps); len(missing) > 0 {
		return buildkit.CandyCapabilitiesError(g.Candies, candyOrder, missing)
	}

	// 4. activeInits — the active init systems for this composition.
	if g.InitConfig != nil {
		img.ActiveInits = g.InitConfig.ActiveInit(g.Candies, candyOrder)
	}

	// 5. initSystem + initDef — the resolved init system name + definition.
	if g.InitConfig != nil {
		img.InitSystem, img.InitDef = g.InitConfig.ResolveInitSystem(g.Candies, candyOrder, "")
	}

	// 6. BakedMetadata — the fully-baked OCI-label wire set.
	img.BakedMetadata = g.buildBakedMetadata(boxName, candyOrder)

	return nil
}

// RenderPrepAll runs RenderPrepBox for every box in the generator's box set.
// Called by the host build-prep seam before projecting the envelope.
func (g *Generator) RenderPrepAll() error {
	for name := range g.Boxes {
		if err := g.RenderPrepBox(name); err != nil {
			return fmt.Errorf("render-prep %s: %w", name, err)
		}
	}
	return nil
}

// buildBakedMetadata runs the former writeLabels data-gathering for boxName into a *spec.BakedLabelSet
// (the BUILD-side wire-form carrier). candyOrder is the render candy order (g.GlobalOrderForBox result)
// — it must equal the order the deploykit render iterates so the per-candy-ordered label lists
// (services, secrets, routes, env) are byte-identical. The candy-declared arbitrary OCI labels
// (caps.OCILabels) and the bootc flag (caps.PreserveUser) ride this struct (the formatter emits them);
// the two render-gating booleans (PreserveUser/NeedsRootAfterInit) ride img.CandyCaps separately via the
// #AggregatedCandyCapsView for generateContainerfile's final-USER directive.
//
//nolint:gocyclo // linear OCI-label gather — one branch per label field (the former writeLabels data half); no shared abstraction across the ~40 independent labels.
func (g *Generator) buildBakedMetadata(boxName string, candyOrder []string) *spec.BakedLabelSet {
	img := g.Boxes[boxName]
	meta := &spec.BakedLabelSet{}

	// Always-present scalars (the formatter emits them unconditionally).
	meta.Version = img.EffectiveVersion
	// ai.opencharly.box carries the box's LEAF name — the same identifier the image ref is built
	// from (buildkit.ResolveBox descends into the namespace and names FullTag `<registry>/<leaf>`),
	// NOT this map key, which for an imported box is namespace-qualified (`fedora.fedora-nonfree`).
	// The label and the ref name MUST agree: every consumer keys the two together — the local-image
	// resolver's label family (spec/container.ResolveLocalImage) and `charly clean`'s retention
	// groups. When they disagreed, a namespaced build was invisible to a resolve of its own ref
	// name and an older bare-labelled image won, so `charly check box <box>` certified a stale
	// artifact and passed.
	meta.Box = spec.LeafName(boxName)
	meta.UID = img.UID
	meta.GID = img.GID
	meta.User = img.User
	meta.Home = img.Home

	// Conditional scalars (the formatter omits them when empty).
	meta.Registry = img.Registry
	if img.CandyCaps != nil && img.CandyCaps.PreserveUser {
		meta.Bootc = true
	}
	// Candy-contributed arbitrary OCI labels (capabilities.oci_labels, e.g. the bootc-config
	// dev.containers.bootc=true). Emitted sorted by the formatter so Containerfile diffs stay stable.
	if img.CandyCaps != nil && len(img.CandyCaps.OCILabels) > 0 {
		meta.OCILabels = img.CandyCaps.OCILabels
	}
	meta.Network = img.Network

	// Platform identity + builder-pool coordination.
	meta.Distro = img.Distro
	meta.BuildFormat = img.BuildFormats
	meta.Builder = map[string]string(img.Builder)
	meta.Build = img.BuilderCapabilities

	// Ports (CollectBoxPorts) + per-candy port protocols.
	boxPorts, _ := CollectBoxPorts(g.Config, g.Candies, boxName)
	meta.Port = boxPorts
	portProtos := make(map[string]string)
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		for _, ps := range layer.PortSpecs() {
			if ps.Protocol != "" && ps.Protocol != "http" {
				portProtos[strconv.Itoa(ps.Port)] = ps.Protocol
			}
		}
	}
	profiles := map[string]spec.TerminalProfile{}
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		for name, profile := range layer.TerminalProfiles() {
			profiles[name] = profile
		}
	}
	if len(profiles) > 0 {
		meta.TerminalProfiles = profiles
	}
	meta.PortProto = portProtos

	// Volumes: short form names (without charly-<image>- prefix). The wire form is
	// LabelVolumeEntry{Name, Path} (the ai.opencharly.volume label shape), NOT the deploy-side
	// VolumeMount{VolumeName, ContainerPath} — the formatter emits the wire form byte-for-byte.
	volumes, _ := CollectBoxVolume(g.Config, g.Candies, boxName, img.Home, nil)
	if len(volumes) > 0 {
		labelVols := make([]spec.LabelVolumeEntry, 0, len(volumes))
		for _, v := range volumes {
			shortName := strings.TrimPrefix(v.VolumeName, "charly-"+boxName+"-")
			labelVols = append(labelVols, spec.LabelVolumeEntry{Name: shortName, Path: v.ContainerPath})
		}
		meta.Volume = labelVols
	}

	// Aliases: collected from candies + image-level config.
	aliases, _ := CollectBoxAlias(g.Config, g.Candies, boxName)
	meta.Alias = collectedAliasesToLabel(aliases)

	// Security: collected from candies + image config.
	meta.Security = CollectSecurity(g.Config, g.Candies, boxName)

	// Image-level env vars.
	imgCfg, _ := g.Config.BoxConfig(boxName)
	meta.Env = imgCfg.Env

	// Hooks.
	meta.Hook = CollectHooks(g.Config, g.Candies, boxName)

	// Description: three-section plan-shaped self-description.
	meta.Description = CollectDescriptions(g.Config, g.Candies, boxName)

	// Shell-init manifest.
	meta.Shell = CollectShell(g.Config, g.Candies, boxName)

	// Skills: the composed candies' skill definitions (ai.opencharly.skills).
	meta.Skills = CollectSkills(g.Config, g.Candies, boxName)

	// Init system label: active init system name + per-init service list.
	if g.InitConfig != nil {
		labelInitSystem, labelInitDef := g.InitConfig.ResolveInitSystem(g.Candies, candyOrder, "")
		if labelInitSystem != "" && labelInitDef != nil {
			meta.Init = labelInitSystem
			meta.InitDef = &spec.CapabilityInitDef{
				Entrypoint:         labelInitDef.Entrypoint,
				FallbackEntrypoint: labelInitDef.FallbackEntrypoint,
				ManagementTool:     labelInitDef.ManagementTool,
				ManagementCommands: labelInitDef.ManagementCommands,
			}
			// Per-init service-name list (legacy candy-name summary).
			var serviceNames []string
			for _, candyName := range candyOrder {
				layer := g.Candies[candyName]
				if layer.HasInit(labelInitSystem) {
					serviceNames = append(serviceNames, candyName)
				}
			}
			meta.ServiceNames = serviceNames
			meta.InitLabelKey = labelInitDef.LabelKey
			// Structured per-entry service spec.
			var capServices []spec.CapabilityService
			for _, candyName := range candyOrder {
				layer := g.Candies[candyName]
				for i := range layer.Service() {
					e := &layer.Service()[i]
					capServices = append(capServices, spec.CapabilityService{
						Name:             e.Name,
						Scope:            e.EffectiveScope(),
						Enable:           e.Enable,
						UsePackaged:      e.UsePackaged,
						Exec:             e.Exec,
						Env:              e.Env,
						Restart:          e.Restart,
						WorkingDirectory: e.WorkingDirectory,
						User:             e.User,
						After:            e.After,
						Before:           e.Before,
						Stdout:           e.Stdout,
						StopTimeout:      e.StopTimeout,
						Kind:             e.Kind,
						Events:           e.Events,
						AutoStart:        e.AutoStart,
						StartRetries:     e.StartRetries,
						StartSec:         e.StartSecs,
						StopSignal:       e.StopSignal,
						ExitCode:         e.ExitCode,
						Priority:         e.Priority,
						Init:             labelInitSystem,
						Candy:            candyName,
					})
				}
			}
			meta.Service = capServices
		}
	}

	// Port relay: collected from candies.
	var portRelay []int
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		portRelay = append(portRelay, layer.RelayPorts()...)
	}
	meta.PortRelay = portRelay

	// Secrets: collected from candies (metadata only, no values). Dedup by name+env.
	var labelSecrets []spec.LabelSecretEntry
	secretSeen := make(map[string]bool)
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		for _, s := range layer.Secret() {
			key := s.Name + ":" + s.Env
			if secretSeen[key] {
				continue
			}
			secretSeen[key] = true
			target := s.Target
			if target == "" {
				target = "/run/secrets/" + s.Name
			}
			labelSecrets = append(labelSecrets, spec.LabelSecretEntry{Name: s.Name, Target: target, Env: s.Env})
		}
	}
	meta.Secret = labelSecrets

	// Env provides.
	envProvides := make(map[string]string)
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		if layer.HasEnvProvides() {
			maps.Copy(envProvides, layer.EnvProvides())
		}
	}
	if len(envProvides) > 0 {
		meta.EnvProvide = envProvides
	}

	// Env requires / accepts.
	meta.EnvRequire = sortedEnvDepsFromCandies(g.Candies, candyOrder, func(l spec.CandyReader) ([]spec.EnvDependency, bool) {
		if l.HasEnvRequires() {
			return l.EnvRequire(), true
		}
		return nil, false
	})
	meta.EnvAccept = sortedEnvDepsFromCandies(g.Candies, candyOrder, func(l spec.CandyReader) ([]spec.EnvDependency, bool) {
		if l.HasEnvAccepts() {
			return l.EnvAccept(), true
		}
		return nil, false
	})
	meta.SecretRequire = sortedEnvDepsFromCandies(g.Candies, candyOrder, func(l spec.CandyReader) ([]spec.EnvDependency, bool) {
		if l.HasSecretRequires() {
			return l.SecretRequire(), true
		}
		return nil, false
	})
	meta.SecretAccept = sortedEnvDepsFromCandies(g.Candies, candyOrder, func(l spec.CandyReader) ([]spec.EnvDependency, bool) {
		if l.HasSecretAccepts() {
			return l.SecretAccept(), true
		}
		return nil, false
	})

	// MCP provides (dedup by name, sorted).
	mcpProvidesMap := make(map[string]spec.MCPServerYAML)
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		if layer.HasMCPProvides() {
			for _, mcp := range layer.MCPProvide() {
				mcpProvidesMap[mcp.Name] = mcp
			}
		}
	}
	if len(mcpProvidesMap) > 0 {
		names := make([]string, 0, len(mcpProvidesMap))
		for name := range mcpProvidesMap {
			names = append(names, name)
		}
		kit.SortStrings(names)
		mcpProvides := make([]spec.MCPServerYAML, 0, len(names))
		for _, name := range names {
			mcpProvides = append(mcpProvides, mcpProvidesMap[name])
		}
		meta.MCPProvide = mcpProvides
	}

	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		if layer.HasAgentProvides() {
			meta.AgentProvide = append(meta.AgentProvide, layer.AgentProvide()...)
		}
	}

	// MCP requires / accepts.
	meta.MCPRequire = sortedEnvDepsFromCandies(g.Candies, candyOrder, func(l spec.CandyReader) ([]spec.EnvDependency, bool) {
		if l.HasMCPRequires() {
			return l.MCPRequire(), true
		}
		return nil, false
	})
	meta.MCPAccept = sortedEnvDepsFromCandies(g.Candies, candyOrder, func(l spec.CandyReader) ([]spec.EnvDependency, bool) {
		if l.HasMCPAccepts() {
			return l.MCPAccept(), true
		}
		return nil, false
	})

	// Routes: collected from candies.
	var routes []spec.LabelRouteEntry
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		if layer.HasRoute() {
			rc, err := layer.Route()
			if err == nil && rc != nil {
				port, _ := strconv.Atoi(rc.Port)
				routes = append(routes, spec.LabelRouteEntry{Host: rc.Host, Port: port})
			}
		}
	}
	meta.Route = routes

	// Candy env vars: merged from all candies + builder runtime contributions.
	var envConfigs []*kit.EnvConfig
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		if env, _ := layer.EnvConfig(); env != nil {
			envConfigs = append(envConfigs, env)
		}
	}
	envConfigs = append(envConfigs, g.CollectBuilderRuntimeEnv(candyOrder, img)...)
	if len(envConfigs) > 0 {
		merged := kit.MergeEnvConfigs(envConfigs)
		if len(merged.Vars) > 0 {
			meta.EnvCandy = merged.Vars
		}
		meta.PathAppend = merged.PathAppend
	}

	// Status + info: worst-of status + first-line info parts.
	effectiveStatus := buildkit.StatusWorking
	var infoParts []string
	if img.Info != "" {
		infoParts = append(infoParts, img.Info)
	}
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		cs := buildkit.CandyStatus(layer)
		effectiveStatus = buildkit.WorstStatus(effectiveStatus, cs)
		if li := DescriptionInfo(layer.GetDescription()); li != "" && cs != "working" {
			infoParts = append(infoParts, candyName+": "+li)
		}
	}
	meta.Status = buildkit.ResolveStatus(effectiveStatus)
	meta.CheckLevel = kit.ResolveCheckLevel(img.CheckLevel)
	if len(infoParts) > 0 {
		meta.Info = strings.ReplaceAll(strings.Join(infoParts, "; "), "\n", " ")
	}

	// Candy versions.
	candyVersions := make(map[string]string)
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		if v := layer.GetVersion(); v != "" {
			candyVersions[candyName] = v
		}
	}
	meta.CandyVersion = candyVersions

	// Data entries: staging paths for deploy-time provisioning (full image-chain walk).
	meta.DataEntries = g.collectChainDataEntries(boxName)

	// Data image flag.
	if imgCfg.DataImage {
		meta.DataImage = true
	}

	return meta
}

// collectChainDataEntries walks the full image chain (like CollectBoxVolume) collecting data
// entries from candies in parent/intermediate images — the former writeLabels data-entries block.
func (g *Generator) collectChainDataEntries(boxName string) []spec.LabelDataEntry {
	var dataEntries []spec.LabelDataEntry
	seenDataCandies := make(map[string]bool)
	current := boxName
	for {
		imgDef, ok := g.Config.BoxConfig(current)
		if !ok {
			break
		}
		resolved, _ := ResolveCandyOrder(imgDef.Candy, g.Candies, nil)
		for _, candyName := range resolved {
			if seenDataCandies[candyName] {
				continue
			}
			seenDataCandies[candyName] = true
			layer, ok := g.Candies[candyName]
			if !ok || !layer.HasData() {
				continue
			}
			for _, d := range layer.Data() {
				staging := "/data/" + d.Volume + "/"
				if d.Dest != "" {
					staging += d.Dest
					if !strings.HasSuffix(staging, "/") {
						staging += "/"
					}
				}
				dataEntries = append(dataEntries, spec.LabelDataEntry{Volume: d.Volume, Staging: staging, Candy: candyName, Dest: d.Dest})
			}
		}
		if baseImg, isInternal := g.Config.BoxConfig(imgDef.Base); isInternal && baseImg.IsEnabled() {
			current = imgDef.Base
		} else {
			break
		}
	}
	return dataEntries
}

// collectedAliasesToLabel converts the CollectBoxAlias result to the label sub-shape.
func collectedAliasesToLabel(aliases []spec.CollectedAlias) []spec.CollectedAlias {
	if len(aliases) == 0 {
		return nil
	}
	out := make([]spec.CollectedAlias, len(aliases))
	copy(out, aliases)
	return out
}

// sortedEnvDepsFromCandies collects per-candy env-dependency lists (dedup by name, last wins) and
// returns them sorted — the former writeLabels envRequires/accepts/mcp blocks (via sortedEnvDeps).
func sortedEnvDepsFromCandies(layers map[string]spec.CandyReader, candyOrder []string, pick func(spec.CandyReader) ([]spec.EnvDependency, bool)) []spec.EnvDependency {
	m := make(map[string]spec.EnvDependency)
	for _, candyName := range candyOrder {
		layer := layers[candyName]
		if deps, ok := pick(layer); ok {
			for _, dep := range deps {
				m[dep.Name] = dep
			}
		}
	}
	if len(m) == 0 {
		return nil
	}
	return sortedEnvDeps(m)
}

// sortedEnvDeps returns a deterministic slice from a name-keyed map, sorted by Name.
func sortedEnvDeps(m map[string]spec.EnvDependency) []spec.EnvDependency {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]spec.EnvDependency, 0, len(m))
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out
}
