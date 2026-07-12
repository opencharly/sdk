package deploykit

import "github.com/opencharly/sdk/kit"

// deploy_volume.go — the kind-blind deploy-VOLUME naming + backing TYPES folded out
// of charly/deploy.go + charly/volumes.go + charly/enc.go to sdk/deploykit (P13/C15),
// so an SDK consumer (candy/plugin-deploy-pod's generateQuadlet) references the volume
// wire types + the deterministic per-deploy naming without importing charly core.
//
// What moved here: the VolumeMount RESOLVED-STATE struct (from charly/volumes.go — the
// volume slice) + the two PURE per-deploy naming helpers (DeployVolumePrefix /
// DeployStorageDir, from charly/deploy.go). VolumeMount is plain-Go, NOT CUE-sourced: it is
// NEVER marshaled — the ai.opencharly.volume OCI label is []LabelVolumeEntry {name,path}
// (charly/generate.go/labels.go), and VolumeMount is the DEPLOY-RESOLVED form built from a
// LabelVolumeEntry + the box name at decode (VolumeName="charly-<box>-<name>"). So it is
// resolved runtime state (the same category as ResolvedBindMount), and deploykit plain-Go is
// its correct permanent home — the wire mandate does not apply. What lives ELSEWHERE:
// ResolvedBindMount relocates to deploykit with the enc-model cutover (charly/enc.go, C6
// single-owner); ResolveVolumeBacking + resolveVolumeHostPath (coupled to enc.go's
// encryptedPlainDir/encryptedVolumeName cluster) and scopeVolumesToDeployKey (reads
// *BoxMetadata) fold with their owning cutovers. This file carries the SDK-clean pieces the
// pod plugin's generateQuadlet needs now.

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
