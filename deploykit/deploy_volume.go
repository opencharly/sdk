package deploykit

import (
	"path/filepath"
	"strings"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/vmshared"
	"github.com/opencharly/spec/spec"
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
// path cluster (EncryptedVolumeName / EncryptedCipherDir / EncryptedPlainDir). VolumeMount is
// CUE-sourced in spec now (P2B, task #60) and ALIASED here: not because the WIRE mandate applies
// (it is never marshaled — the ai.opencharly.volume label is []LabelVolumeEntry {name,path}, and
// VolumeMount is the DEPLOY-RESOLVED form built from one at decode), but because spec.BoxMetadata
// CONTAINS []VolumeMount and spec sits below deploykit — a spec type cannot reference a deploykit
// type, so containment forces VolumeMount down to spec. ResolvedBindMount (quadlet.go, the P11 enc
// move) is NOT contained by any spec type, so it stays plain-Go here.
// ResolveVolumeBacking is a PURE resolver: the HOST (config-resolve seam) loads labelVolumes
// (ExtractMetadata) + deployVolumes (charly.yml) and calls it; the pod plugin consumes the
// result. What STILL lives in charly core: scopeVolumesToDeployKey (reads *BoxMetadata — folds
// with the BoxMetadata cutover) and the STATEFUL enc glue (loadEncryptedVolume / encPlanFor /
// encStatus — deploy state, reached via the config-resolve seam, per Ruling C).

// VolumeMount is a resolved named-volume mount (charly-<deploy>-<name> → container path).
// CUE-sourced in spec (boxmetadata.cue, P2B) + aliased here; consumers keep using
// deploykit.VolumeMount unchanged. DISTINCT from spec.ResolvedVolumeMount (the box-aggregate
// `charly box inspect --format volumes` entry): same two fields, different concept + call sites —
// do NOT conflate them.
type VolumeMount = spec.VolumeMount

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

// SiblingVolumePrefixes returns the volume prefixes of every deploy in dc OTHER
// than (deployKey, instance).
//
// A base deploy's prefix (charly-<base>-) is itself a prefix of its sibling
// instances' volume names (charly-<base>-<instance>-<vol>), so any teardown or
// listing that scopes by prefix ALONE would touch a live sibling instance's
// volumes — e.g. `charly remove --purge githubrunner` deleting the state of
// githubrunner/no-1..4. Callers pass the result to VolumeNameBelongsTo so the
// operation stays confined to the deploy's OWN volumes. Returns nil for a nil
// or empty config (the genuinely-orphaned case, where no siblings are known).
func SiblingVolumePrefixes(dc *DeployConfig, deployKey, instance string) []string {
	if dc == nil {
		return nil
	}
	self := DeployVolumePrefix(deployKey, instance)
	var out []string
	for key := range dc.Deploy {
		b, i := ParseDeployKey(key)
		if p := DeployVolumePrefix(b, i); p != self {
			out = append(out, p)
		}
	}
	return out
}

// VolumeNameBelongsTo reports whether name is a named volume (or per-deploy
// encrypted-volume directory) OWNED by (deployKey, instance): it carries this
// deploy's volume prefix AND no sibling deploy claims it MORE SPECIFICALLY.
//
// A shorter prefix matches a longer name, so mere sibling exclusion is wrong:
// for instance no-2, the base deploy's prefix (charly-<base>-) also matches
// charly-<base>-no-2-state. The rule is therefore LONGEST PREFIX WINS — name
// belongs to this deploy iff its prefix matches and no sibling prefix matches
// with a strictly greater length. That confining rule is what keeps a base
// deploy from claiming its instances' volumes while an instance still owns its
// own; the trailing dash in DeployVolumePrefix prevents a no-1 vs no-10
// near-miss. Pure — the single owner of the "is this mine?" decision shared by
// podman-volume teardown, encrypted-dir teardown, and volume listing (R3).
func VolumeNameBelongsTo(name, deployKey, instance string, siblingPrefixes []string) bool {
	self := DeployVolumePrefix(deployKey, instance)
	if !strings.HasPrefix(name, self) {
		return false
	}
	for _, sp := range siblingPrefixes {
		if sp != "" && len(sp) > len(self) && strings.HasPrefix(name, sp) {
			return false
		}
	}
	return true
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
