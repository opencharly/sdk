package deploykit

import (
	"path/filepath"
	"strings"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/vmshared"
)

// deploy_volume.go — the kind-blind deploy-VOLUME naming + backing resolver folded out
// of charly/deploy.go + charly/volumes.go + charly/enc.go to sdk/deploykit (P13/C15 +
// P11), so an SDK consumer (candy/plugin-deploy-pod's generateQuadlet) references the
// volume wire types + the deterministic per-deploy naming + the pure backing resolution
// without importing charly core.
//
// What lives here: the VolumeMount RESOLVED-STATE struct (P13, from charly/volumes.go) +
// the two PURE per-deploy naming helpers (DeployVolumePrefix / DeployStorageDir, P13, from
// charly/deploy.go) + (P11 — the enc-model cutover this file's P13 header earmarked) the
// enc-coupled volume RESOLVER: ResolveVolumeBacking / resolveVolumeHostPath + the pure enc
// path cluster (EncryptedVolumeName / EncryptedCipherDir / EncryptedPlainDir). VolumeMount +
// ResolvedBindMount (the latter defined in quadlet.go with the P11 enc move) are plain-Go,
// NOT CUE-sourced: they are NEVER marshaled — the ai.opencharly.volume OCI label is
// []LabelVolumeEntry {name,path}, and VolumeMount is the DEPLOY-RESOLVED form built from a
// LabelVolumeEntry + the box name at decode. So they are resolved runtime state and deploykit
// plain-Go is their correct permanent home — the wire mandate does not apply.
// ResolveVolumeBacking is a PURE resolver: the HOST (config-resolve seam) loads labelVolumes
// (ExtractMetadata) + deployVolumes (charly.yml) and calls it; the pod plugin consumes the
// result. What STILL lives in charly core: scopeVolumesToDeployKey (reads *BoxMetadata — folds
// with the BoxMetadata cutover) and the STATEFUL enc glue (loadEncryptedVolume / encPlanFor /
// encStatus — deploy state, reached via the config-resolve seam, per Ruling C).

// VolumeMount is a resolved named-volume mount (charly-<deploy>-<name> → container path).
type VolumeMount struct {
	VolumeName    string // e.g. "charly-openclaw-data"
	ContainerPath string // e.g. "/home/user/.openclaw" (~ expanded)
}

// DeployVolumePrefix is the named-volume prefix for a deploy: the deploy's container
// name plus a dash, so EVERY distinctly-named deploy (base, Pattern-B, instance, or
// kind:check bed) gets its own volume namespace. Two deploys never share a named
// volume unless they share a container name (which they can't). Single source of truth
// for volume naming — ResolveVolumeBacking, removeVolumes, and scopeVolumesToDeployKey
// all key off it.
func DeployVolumePrefix(deployKey, instance string) string {
	return kit.ContainerNameInstance(deployKey, instance) + "-"
}

// DeployStorageDir is the per-deploy directory component for bind-auto paths and
// encrypted-volume directories. Like DeployVolumePrefix it is unique per deploy (base
// vs instance vs Pattern-B vs bed). For a base deploy with no instance it is just the
// deploy key; an instance appends "-<instance>".
func DeployStorageDir(deployKey, instance string) string {
	if instance == "" {
		return deployKey
	}
	return deployKey + "-" + instance
}

// EncryptedVolumeName returns the directory name for an encrypted volume:
// charly-<box>-<name>.
func EncryptedVolumeName(boxName, name string) string {
	return "charly-" + boxName + "-" + name
}

// EncryptedCipherDir returns the cipher (encrypted-blob) directory for an encrypted
// bind mount: <storagePath>/<EncryptedVolumeName>/cipher.
func EncryptedCipherDir(storagePath, boxName, name string) string {
	return filepath.Join(storagePath, EncryptedVolumeName(boxName, name), "cipher")
}

// EncryptedPlainDir returns the plain (FUSE mount-point) directory for an encrypted
// bind mount: <storagePath>/<EncryptedVolumeName>/plain.
func EncryptedPlainDir(storagePath, boxName, name string) string {
	return filepath.Join(storagePath, EncryptedVolumeName(boxName, name), "plain")
}

// resolveVolumeHostPath computes the host-side path for a bind/encrypted deploy volume —
// the single home for the host-path strategy (R3), shared by ResolveVolumeBacking's two
// passes (label-matched + deploy-only), which previously carried byte-identical copies of
// this switch differing only in the name argument.
//
//	encrypted + explicit Host:  <Host>/plain
//	encrypted + default:        <encStoragePath>/<storageDir>/plain  (per-deploy)
//	bind + explicit Host:       <Host>
//	bind + default:             <volumesPath>/<storageDir>/<name>    (per-deploy)
func resolveVolumeHostPath(dv vmshared.DeployVolumeConfig, name, storageDir, encStoragePath, volumesPath string) string {
	switch {
	case dv.Type == "encrypted":
		if dv.Host != "" {
			return filepath.Join(kit.ExpandHostHome(dv.Host), "plain")
		}
		return EncryptedPlainDir(encStoragePath, storageDir, name)
	case dv.Host != "":
		return kit.ExpandHostHome(dv.Host)
	default:
		return filepath.Join(volumesPath, storageDir, name)
	}
}

// ResolveVolumeBacking splits image volumes into named volumes and bind mounts based on
// the deploy's charly.yml volume configuration. Volumes without a deploy override stay
// named volumes; a type=bind/encrypted override becomes a ResolvedBindMount; deploy-only
// volumes (carrying Path, absent from any label) are added as binds. PURE — the host
// resolves labelVolumes (ExtractMetadata) + deployVolumes (charly.yml) and calls this in
// the config-resolve seam; the pod plugin consumes the result.
func ResolveVolumeBacking(boxName, instance string, labelVolumes []VolumeMount, deployVolumes []vmshared.DeployVolumeConfig, home string, encStoragePath string, volumesPath string) ([]VolumeMount, []ResolvedBindMount) {
	deployByName := make(map[string]vmshared.DeployVolumeConfig, len(deployVolumes))
	for _, dv := range deployVolumes {
		deployByName[dv.Name] = dv
	}

	matched := make(map[string]bool)

	var volumes []VolumeMount
	var bindMounts []ResolvedBindMount

	for _, vol := range labelVolumes {
		// Extract the short name from the deploy-scoped prefix (charly-<deploy>-<name>).
		shortName := strings.TrimPrefix(vol.VolumeName, DeployVolumePrefix(boxName, instance))

		dv, hasOverride := deployByName[shortName]
		if hasOverride {
			matched[shortName] = true
		}

		if hasOverride && (dv.Type == "bind" || dv.Type == "encrypted") {
			hostPath := resolveVolumeHostPath(dv, shortName, DeployStorageDir(boxName, instance), encStoragePath, volumesPath)
			bindMounts = append(bindMounts, ResolvedBindMount{
				Name:      shortName,
				HostPath:  hostPath,
				ContPath:  vol.ContainerPath,
				Encrypted: dv.Type == "encrypted",
			})
		} else {
			volumes = append(volumes, vol)
		}
	}

	// Add deploy-only volumes (not in any candy, must carry Path).
	for _, dv := range deployVolumes {
		if matched[dv.Name] || dv.Path == "" {
			continue
		}
		containerPath := kit.ExpandPath(dv.Path, home)
		if dv.Type == "bind" || dv.Type == "encrypted" {
			hostPath := resolveVolumeHostPath(dv, dv.Name, DeployStorageDir(boxName, instance), encStoragePath, volumesPath)
			bindMounts = append(bindMounts, ResolvedBindMount{
				Name:      dv.Name,
				HostPath:  hostPath,
				ContPath:  containerPath,
				Encrypted: dv.Type == "encrypted",
			})
		} else {
			volumes = append(volumes, VolumeMount{
				VolumeName:    DeployVolumePrefix(boxName, instance) + dv.Name,
				ContainerPath: containerPath,
			})
		}
	}

	return volumes, bindMounts
}
