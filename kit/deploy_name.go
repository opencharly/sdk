package kit

import "strings"

// SanitizeDeployName turns a deploy name like "vm:arch" or "stack.web.db"
// into a shell-safe, path-safe, kubeconfig-context-safe identifier.
// Colons and dots are replaced with dashes; that keeps the semantics
// identifiable ("vm:arch" → "vm-arch") without breaking file paths.
//
// P12a: relocated from charly/k3s_post.go, which had the sole definition for
// four core call sites (K3sPostProvision, checkvars.go's DEPLOY_NAME var,
// data.go's artifact key, check_cmd.go's vm DEPLOY_NAME) — promoted here so
// this file's own ResolveCheckVarsRuntime can call it without a core import.

func SanitizeDeployName(s string) string {
	r := strings.NewReplacer(":", "-", ".", "-", "/", "-")
	return r.Replace(s)
}

// BareVolumeName strips the "charly-<box>[-<instance>]-" prefix from a resolved
// volume name.
//
// P12a: relocated from sdk/deploykit/quadlet.go (a pure string helper with no
// deploykit-specific coupling) so this file's mergeRuntimeVars can call it
// without a kit→deploykit import (deploykit already imports kit — the reverse
// direction would cycle). deploykit's own caller now calls kit.BareVolumeName.
func BareVolumeName(volumeName, boxName, instance string) string {
	if instance != "" {
		if p := "charly-" + boxName + "-" + instance + "-"; strings.HasPrefix(volumeName, p) {
			return volumeName[len(p):]
		}
	}
	if p := "charly-" + boxName + "-"; strings.HasPrefix(volumeName, p) {
		return volumeName[len(p):]
	}
	return volumeName
}
