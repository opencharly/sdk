package kit

import (
	"fmt"

	"github.com/opencharly/sdk/spec"
)

// entrypoint.go — the runtime entrypoint-resolution mechanism (K4 lane B: relocated from
// charly/start.go's resolveEntrypointFromMeta + charly/service.go's wellKnownInitDefs, both
// genuinely pure — no project-loader dependency, just *spec.BoxMetadata field reads + a frozen
// lookup table). Consumed directly by candy/plugin-deploy-pod (pod_lifecycle_resolve.go's move)
// AND by charly core's config_image.go (group 3, not moving yet) via the kit_aliases.go
// passthrough.

// wellKnownInitDefs is the legacy fallback for pre-init_def-label images — the build-resolved init
// contract now travels in the ai.opencharly.init_def label, so resolveInitDefFromMeta /
// ResolveEntrypointFromMeta read it label-first and only consult this table when meta.InitDef is
// absent. Because the build-resolved def now travels in the label, init systems declared ONLY in
// the embedded init: vocabulary (charly/charly.yml) work at runtime too — they no longer need an
// entry here. This table is frozen at the two init systems that predate the label; do NOT add new
// ones (declare them in the vocabulary instead, where they bake into the label).
var wellKnownInitDefs = map[string]*spec.ResolvedInit{
	"supervisord": {
		Entrypoint:     []string{"supervisord", "-n", "-c", "/etc/supervisord.conf"},
		ManagementTool: "supervisorctl",
		ManagementCommands: map[string]string{
			"status":  "status",
			"start":   "start {{.Service}}",
			"stop":    "stop {{.Service}}",
			"restart": "restart {{.Service}}",
		},
	},
	"systemd": {
		// Systemd-on-bootc boots via VM init; container has no entrypoint.
		Entrypoint:     nil,
		ManagementTool: "systemctl",
		ManagementCommands: map[string]string{
			"status":  "--user status {{.Service}}",
			"start":   "--user start {{.Service}}",
			"stop":    "--user stop {{.Service}}",
			"restart": "--user restart {{.Service}}",
		},
	},
}

// ResolveEntrypointFromMeta determines the entrypoint from image metadata (runtime mode).
// Label-first: the build-resolved init contract is baked into the
// ai.opencharly.init_def label (meta.InitDef), so any init system declared in
// the embedded `init:` vocabulary — including custom ones — now reaches
// runtime. wellKnownInitDefs is consulted only for pre-init_def-label images
// (built before the label existed; their labels cannot be re-baked).
func ResolveEntrypointFromMeta(meta *spec.BoxMetadata) []string {
	if meta.Init == "" {
		return []string{"sleep", "infinity"}
	}
	if meta.InitDef != nil {
		// The baked entrypoint is authoritative. An empty entrypoint means
		// the container boots via the image's own init (systemd-on-bootc),
		// exactly as the legacy registry encoded — fall through to the
		// image default rather than overriding with sleep infinity.
		return meta.InitDef.Entrypoint
	}
	if def, ok := wellKnownInitDefs[meta.Init]; ok {
		return def.Entrypoint
	}
	return []string{"sleep", "infinity"}
}

// ResolveInitDefFromMeta returns the init contract for management-command
// rendering. Label-first: the build-resolved def is baked into the
// ai.opencharly.init_def label, so any vocabulary-declared init
// system — including custom ones — resolves at runtime. Falls back to
// wellKnownInitDefs only for pre-init_def-label images (built before the
// label existed).
func ResolveInitDefFromMeta(meta *spec.BoxMetadata) (*spec.ResolvedInit, error) {
	if meta.InitDef != nil {
		return &spec.ResolvedInit{
			Entrypoint:         meta.InitDef.Entrypoint,
			FallbackEntrypoint: meta.InitDef.FallbackEntrypoint,
			ManagementTool:     meta.InitDef.ManagementTool,
			ManagementCommands: meta.InitDef.ManagementCommands,
		}, nil
	}
	if def, ok := wellKnownInitDefs[meta.Init]; ok {
		return def, nil
	}
	return nil, fmt.Errorf("unknown init system %q; cannot determine management commands (image predates the ai.opencharly.init_def label — rebuild it to bake the init contract)", meta.Init)
}
