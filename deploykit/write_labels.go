package deploykit

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
)

// write_labels.go — the FORMAT half of the render's OCI-label emission (#67, the build_resolve
// RENDER-leg death). The former charly/generate.go writeLabels body split at the data/format
// boundary: the HOST gather (charly buildBakedMetadata) reads the live *Candy/*Config graph +
// the Collect* aggregators into a fully-baked *spec.BakedLabelSet (the EXACT wire data, in wire
// form); this file FORMATS it into the LABEL lines byte-for-byte WITHOUT the live graph. Every
// emission statement of the former writeLabels lives on exactly ONE side of the split, so the
// two halves compose to byte-identical Containerfile labels (proven by the render-parity
// byte-golden). The carrier rides buildkit.ResolvedBox.BakedMetadata (live path) /
// ResolvedBoxView.BakedMetadata (the plugin-build drive path, via NewSpecResolvedBox).
//
// This is the single render-home for label emission: charly's writeLabels is DELETED once the
// live path wires buildBakedMetadata + Generator.WriteLabels (R5 timing: before the parity
// byte-golden + bed acceptance run).

// WriteLabels emits the image metadata LABEL block from a fully-baked *spec.BakedLabelSet.
// It is the format-only mirror of the former charly writeLabels: it reads NO live graph, only
// the carrier, so it is placement-agnostic (the live render path and the plugin-build envelope
// path feed it the same struct). LABELs are emitted LAST in the final stage (the caller places
// the call after the final USER directive) so a test/label edit only reruns the LABEL
// instructions (metadata-only) instead of invalidating the buildkit cache for every upstream
// RUN/COPY.
//
//nolint:gocyclo // linear OCI-label formatter — one branch per label field (the byte-exact emit half of the split); no shared abstraction across the labels.
func (g *Generator) WriteLabels(b *strings.Builder, meta *spec.BakedLabelSet, boxName string) {
	b.WriteString("# Image metadata\n")

	// Always-present labels. ai.opencharly.version carries the image's content-derived
	// EffectiveVersion (its dedicated version:, else the highest candy version across the
	// base chain) — NOT the per-build tag. Stable across builds when no candy changed.
	fmt.Fprintf(b, "LABEL %s=%q\n", spec.LabelVersion, meta.Version)
	fmt.Fprintf(b, "LABEL %s=%q\n", spec.LabelBox, meta.Box)
	fmt.Fprintf(b, "LABEL %s=%q\n", spec.LabelUID, fmt.Sprintf("%d", meta.UID))
	fmt.Fprintf(b, "LABEL %s=%q\n", spec.LabelGID, fmt.Sprintf("%d", meta.GID))
	fmt.Fprintf(b, "LABEL %s=%q\n", spec.LabelUser, meta.User)
	fmt.Fprintf(b, "LABEL %s=%q\n", spec.LabelHome, meta.Home)

	// Conditional string labels (omitted when empty)
	if meta.Registry != "" {
		fmt.Fprintf(b, "LABEL %s=%q\n", spec.LabelRegistry, meta.Registry)
	}
	// Bootc-flavored compositions emit the internal round-trip label so deploy-time
	// consumers (labels.go ExtractMetadata) continue to see meta.Bootc=true. The signal is
	// candy-derived (preserve_user) rather than img.Bootc.
	if meta.Bootc {
		fmt.Fprintf(b, "LABEL %s=%q\n", spec.LabelBootc, "true")
	}
	// Candy-contributed OCI labels (capabilities.oci_labels). Includes dev.containers.bootc=true
	// emitted from the bootc-config candy when its preserve_user capability is in the
	// composition. Sorted for determinism so Containerfile diffs stay stable.
	if len(meta.OCILabels) > 0 {
		keys := make([]string, 0, len(meta.OCILabels))
		for k := range meta.OCILabels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(b, "LABEL %s=%q\n", k, meta.OCILabels[k])
		}
	}
	if meta.Network != "" {
		fmt.Fprintf(b, "LABEL %s=%q\n", spec.LabelNetwork, meta.Network)
	}

	// Platform identity + builder-pool coordination labels.
	// No serialized selector union — derive as ["all"] ∪ distro ∪ formats at read time.
	writeJSONLabel(b, spec.LabelPlatformDistro, meta.Distro)
	writeJSONLabel(b, spec.LabelPlatformFormat, meta.BuildFormat)
	if len(meta.Builder) > 0 {
		writeJSONLabel(b, spec.LabelBuilderUse, meta.Builder)
	}
	writeJSONLabel(b, spec.LabelBuilderProvide, meta.Build)

	// JSON array labels (omitted when empty). Ports are inherited from the candy chain
	// (CollectBoxPorts) — boxes no longer declare ports. The label carries bare container
	// ports; the host mapping is resolved at deploy time.
	writeJSONLabel(b, spec.LabelPort, meta.Port)
	writeJSONLabel(b, spec.LabelPortProto, meta.PortProto)

	// Volumes: short form names (LabelVolumeEntry{Name, Path} — the wire form).
	if len(meta.Volume) > 0 {
		writeJSONLabel(b, spec.LabelVolume, meta.Volume)
	}

	// Aliases: collected from candies + image-level config
	writeJSONLabel(b, spec.LabelAlias, meta.Alias)

	// Security: collected from candies + image config (omitted when the struct is empty).
	sec := meta.Security
	if sec.Privileged || sec.CgroupNS != "" || len(sec.CapAdd) > 0 || len(sec.Devices) > 0 || len(sec.SecurityOpt) > 0 || len(sec.GroupAdd) > 0 || sec.ShmSize != "" || len(sec.Mounts) > 0 {
		writeJSONLabel(b, spec.LabelSecurity, sec)
	}

	// Image-level env vars (the wire form is a JSON object — the deploy reader converts to
	// []string KEY=VALUE pairs via envMapToPairs).
	writeJSONLabel(b, spec.LabelEnv, meta.Env)

	// Hooks
	if meta.Hook != nil {
		writeJSONLabel(b, spec.LabelHook, meta.Hook)
	}

	// Description: three-section plan-shaped self-description.
	if meta.Description != nil {
		writeJSONLabel(b, spec.LabelDescription, meta.Description)
	}

	// Shell-init manifest: three-section JSON of per-(origin, shell) contributions.
	if meta.Shell != nil && (len(meta.Shell.Candy) > 0 || len(meta.Shell.Box) > 0 || len(meta.Shell.Deploy) > 0) {
		writeJSONLabel(b, spec.LabelShell, meta.Shell)
	}

	// Init system label: active init system name + per-init service list
	if meta.Init != "" && meta.InitDef != nil {
		fmt.Fprintf(b, "LABEL %s=%q\n", spec.LabelInit, meta.Init)
		// Init definition: bake the runtime-relevant subset of the build-resolved init def so
		// deploy reads the entrypoint + management surface from the image instead of a
		// hardcoded registry. Makes the init system TRUE single-source.
		writeJSONLabel(b, spec.LabelInitDef, meta.InitDef)
		// Per-init service-name list (legacy candy-name summary; kept for `charly service
		// status/restart` CLI ergonomics). Emitted under the init def's DYNAMIC label key.
		if meta.InitLabelKey != "" {
			writeJSONLabel(b, meta.InitLabelKey, meta.ServiceNames)
		}
		// Structured per-entry service spec — source-less deploy reads this instead of
		// relying on charly.yml access at deploy time.
		if len(meta.Service) > 0 {
			writeJSONLabel(b, spec.LabelService, meta.Service)
		}
	}

	// Port relay: collected from candies
	writeJSONLabel(b, spec.LabelPortRelay, meta.PortRelay)

	// Secrets: collected from candies (metadata only, no values)
	if len(meta.Secret) > 0 {
		writeJSONLabel(b, spec.LabelSecret, meta.Secret)
	}

	// Env provides: env vars provided to other containers (service discovery)
	if len(meta.EnvProvide) > 0 {
		writeJSONLabel(b, spec.LabelEnvProvide, meta.EnvProvide)
	}

	// Env requires / accepts / secret requires / accepts / mcp requires / accepts
	if len(meta.EnvRequire) > 0 {
		writeJSONLabel(b, spec.LabelEnvRequire, meta.EnvRequire)
	}
	if len(meta.EnvAccept) > 0 {
		writeJSONLabel(b, spec.LabelEnvAccept, meta.EnvAccept)
	}
	if len(meta.SecretRequire) > 0 {
		writeJSONLabel(b, spec.LabelSecretRequire, meta.SecretRequire)
	}
	if len(meta.SecretAccept) > 0 {
		writeJSONLabel(b, spec.LabelSecretAccept, meta.SecretAccept)
	}
	if len(meta.MCPProvide) > 0 {
		writeJSONLabel(b, spec.LabelMCPProvide, meta.MCPProvide)
	}
	if len(meta.AgentProvide) > 0 {
		writeJSONLabel(b, spec.LabelAgentProvide, meta.AgentProvide)
	}
	if len(meta.TerminalProfiles) > 0 {
		writeJSONLabel(b, spec.LabelTerminalProfiles, meta.TerminalProfiles)
	}
	if len(meta.MCPRequire) > 0 {
		writeJSONLabel(b, spec.LabelMCPRequire, meta.MCPRequire)
	}
	if len(meta.MCPAccept) > 0 {
		writeJSONLabel(b, spec.LabelMCPAccept, meta.MCPAccept)
	}

	// Routes: collected from candies
	writeJSONLabel(b, spec.LabelRoute, meta.Route)

	// Candy env vars: merged from all candies + builder runtime contributions. Both sources
	// funnel into the same OCI labels so deploy-mode consumers of ai.opencharly.path_append
	// and ai.opencharly.env_candy see the full effective env.
	if len(meta.EnvCandy) > 0 {
		writeJSONLabel(b, spec.LabelEnvCandy, meta.EnvCandy)
	}
	writeJSONLabel(b, spec.LabelPathAppend, meta.PathAppend)

	// Skills documentation URL
	if meta.Skill != "" {
		fmt.Fprintf(b, "LABEL %s=%q\n", spec.LabelSkill, meta.Skill)
	}

	// Status and info: the box's effective status is the WORST of its own nominal status and
	// every candy's authored status. Info is the first line of each entity's plain-string
	// description (gathered + newline-collapsed by buildBakedMetadata).
	fmt.Fprintf(b, "LABEL %s=%q\n", spec.LabelStatus, meta.Status)

	// Acceptance-depth rung — the per-box check_level gating `charly check run <bed>`. Always
	// emitted (normalized to the default rung) so a bed runner reading labels never sees empty.
	fmt.Fprintf(b, "LABEL %s=%q\n", spec.LabelCheckLevel, meta.CheckLevel)
	if meta.Info != "" {
		// Single-quote (NOT %q): a description may legitimately mention a ${VAR} (e.g.
		// ${HOST:<subject>}), and the %q double-quoted form lets buildah try to expand it —
		// which fails with "Unsupported modifier" on, e.g., the `<` in ${HOST:<subject>}.
		// kit.ShellQuote matches how every JSON label is emitted (no shell/Dockerfile
		// expansion).
		fmt.Fprintf(b, "LABEL %s=%s\n", spec.LabelInfo, kit.ShellQuote(meta.Info))
	}

	// Candy versions: map of candy name -> CalVer for candies with version set
	writeJSONLabel(b, spec.LabelCandyVersion, meta.CandyVersion)

	// Data entries: staging paths for deploy-time provisioning.
	if len(meta.DataEntries) > 0 {
		writeJSONLabel(b, spec.LabelDataEntries, meta.DataEntries)
	}

	// Data image flag
	if meta.DataImage {
		fmt.Fprintf(b, "LABEL %s=%q\n", spec.LabelDataBox, "true")
	}

	b.WriteString("\n")
}

// writeJSONLabel writes a JSON-encoded LABEL directive. Omits the label if the value is
// nil/empty. The generic form is the verbatim relocation of the former charly/generate.go
// writeJSONLabel (R3: one copy, now in the render home).
func writeJSONLabel[T any](b *strings.Builder, key string, value T) {
	// Check for nil/empty slices and maps via JSON
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	s := string(data)
	if s == "null" || s == "[]" || s == "{}" {
		return
	}
	// Wrap in single-quoted form with proper '\'' escaping so embedded single quotes (common
	// inside test command strings like awk '{print $1}') don't terminate the LABEL value and
	// trip podman's key=value parser.
	fmt.Fprintf(b, "LABEL %s=%s\n", key, kit.ShellQuote(s))
}
