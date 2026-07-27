package deploykit

import (
	"strings"

	"github.com/opencharly/sdk/spec"
)

// volume_collect.go — the box-VOLUME aggregator (relocated from charly/volumes.go in the
// core-min wave-3 build-cluster split). CollectBoxVolume is a PURE candy-chain walk (spec.Config
// + layers, via BoxCandyChain) producing the deploykit.VolumeMount list, shared by the host
// projector, the build render's data-image label emission, and box-inspect.

// CollectBoxVolume resolves all volumes for a box by traversing the full box chain
// (box -> base -> base's base) and collecting volume declarations from all candies.
// Volumes are deduplicated by name (first declaration wins — outermost box takes priority).
func CollectBoxVolume(cfg *spec.Config, layers map[string]CandyModel, boxName string, home string, excludeNames map[string]bool) ([]VolumeMount, error) {
	// Collect all candy names from the box chain (outermost first) via the
	// shared base-chain walk; propagate a resolution error as before.
	allCandyNames, err := BoxCandyChain(cfg, layers, boxName)
	if err != nil {
		return nil, err
	}

	// Collect volumes, dedup by name (first wins), skip excluded names
	seen := make(map[string]bool)
	var mounts []VolumeMount
	for _, candyName := range allCandyNames {
		layer, ok := layers[candyName]
		if !ok || !layer.HasVolumes() {
			continue
		}
		for _, vol := range layer.Volume() {
			if seen[vol.Name] || excludeNames[vol.Name] {
				continue
			}
			seen[vol.Name] = true
			mounts = append(mounts, VolumeMount{
				VolumeName:    "charly-" + boxName + "-" + vol.Name,
				ContainerPath: expandVolumeHome(vol.Path, home),
			})
		}
	}

	// Sort by volume name for deterministic output
	sortVolumeMounts(mounts)
	return mounts, nil
}

// expandVolumeHome replaces ~ and $HOME with the resolved home directory.
func expandVolumeHome(path, home string) string {
	if strings.HasPrefix(path, "~/") {
		return home + path[1:]
	}
	if path == "~" {
		return home
	}
	path = strings.ReplaceAll(path, "$HOME", home)
	return path
}

// sortVolumeMounts sorts volume mounts by name for deterministic output.
func sortVolumeMounts(mounts []VolumeMount) {
	for i := 0; i < len(mounts)-1; i++ {
		for j := i + 1; j < len(mounts); j++ {
			if mounts[i].VolumeName > mounts[j].VolumeName {
				mounts[i], mounts[j] = mounts[j], mounts[i]
			}
		}
	}
}
