package kit

// sidecar_naming.go — the pure sidecar/pod container-naming helpers (K4: relocated from
// charly/sidecar.go — genuinely pure string composition over ContainerName/ContainerNameInstance,
// no project-loader dependency). Consumed directly by candy/plugin-deploy-pod and by charly core's
// remaining callers (config_image.go, quadlet.go), which now import kit directly (K3 ZERO-ALIASES
// — no alias file).

// PodName returns the container name for a pod's primary container.
func PodName(boxName string) string {
	return ContainerName(boxName)
}

// SidecarContainerName returns the container name for a named sidecar.
func SidecarContainerName(boxName, sidecarName string) string {
	return ContainerName(boxName) + "-" + sidecarName
}

// SidecarContainerNameInstance returns the container name for a named sidecar, instance-aware.
func SidecarContainerNameInstance(boxName, instance, sidecarName string) string {
	return ContainerNameInstance(boxName, instance) + "-" + sidecarName
}
