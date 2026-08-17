package kit

import (
	"fmt"

	"github.com/opencharly/spec/spec"
)

// entrypoint.go — the runtime entrypoint-resolution mechanism (K4 lane B: relocated from
// charly/start.go's resolveEntrypointFromMeta + charly/service.go's init resolution, both
// genuinely pure — no project-loader dependency, just *spec.BoxMetadata field reads).
// Consumed directly by candy/plugin-deploy-pod (pod_lifecycle_resolve.go's move)
// AND by charly core's config_image.go (group 3, not moving yet) via the kit_aliases.go
// passthrough.

// ResolveEntrypointFromMeta determines the entrypoint from image metadata (runtime mode).
// The build-resolved init contract is baked into the ai.opencharly.init_def label
// (meta.InitDef), so EVERY init system declared in the embedded `init:` vocabulary reaches
// runtime — including openrc and any custom one — with no table to register in.
func ResolveEntrypointFromMeta(meta *spec.BoxMetadata) []string {
	if meta.Init == "" {
		return []string{"sleep", "infinity"}
	}
	if meta.InitDef == nil {
		// No baked init contract: the image predates the label. There is no table to
		// fall back to any more, and guessing an entrypoint for an unknown init system
		// is how a container ends up running the wrong supervisor. Keep it alive so the
		// operator can exec in and rebuild.
		return []string{"sleep", "infinity"}
	}
	// The baked entrypoint is authoritative. An EMPTY entrypoint is meaningful, not
	// missing: the container boots via the image's own init (systemd-on-bootc), so fall
	// through to the image default rather than overriding it.
	return meta.InitDef.Entrypoint
}

// ResolveInitDefFromMeta returns the init contract for management-command rendering, read
// from the ai.opencharly.init_def label. An image without that label errors: it predates the
// label and must be rebuilt.
func ResolveInitDefFromMeta(meta *spec.BoxMetadata) (*spec.ResolvedInit, error) {
	if meta.InitDef != nil {
		return &spec.ResolvedInit{
			Entrypoint:         meta.InitDef.Entrypoint,
			FallbackEntrypoint: meta.InitDef.FallbackEntrypoint,
			ManagementTool:     meta.InitDef.ManagementTool,
			ManagementCommands: meta.InitDef.ManagementCommands,
		}, nil
	}
	return nil, fmt.Errorf("image carries no ai.opencharly.init_def label for init system %q; "+
		"cannot determine management commands — rebuild the image to bake the init contract", meta.Init)
}
