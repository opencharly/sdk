package spec

// vm_build_wire.go — the "vm-build" HostBuild seam: a command plugin asks the HOST to run the VM-disk
// build engine (privileged pacstrap/bootc/cloud-image → qcow2) it cannot hold. The build ENGINE stays
// core (RunPrivileged / BuildCloudImage / BuildBootcVM / BuildBootstrapVM + LoadBuildConfigForBox — the
// privileged-runner + box-store Mechanisms, NOT externalized), exactly as the box-build engine stayed
// core behind HostBuild("image") in P8. The plugin's `charly vm build` command is THIN: it forwards its
// parsed flags in VmBuildRequest and the host-builder runs the whole build in-process (compiled-in, so
// build progress flows to the shared stdout/stderr). The action noun "vm-build" is class-generic (F11).

// VmBuildRequest carries the `charly vm build` command flags (the former VmBuildCmd fields). The host
// resolves the kind:vm entity + build config + dispatches the per-source-kind engine itself.
type VmBuildRequest struct {
	Box       string `json:"box"`
	Size      string `json:"size,omitempty"`
	RootSize  string `json:"root_size,omitempty"`
	Tag       string `json:"tag,omitempty"`
	Type      string `json:"type,omitempty"`
	Transport string `json:"transport,omitempty"`
	Console   bool   `json:"console,omitempty"`
}

// VmBuildReply is the "vm-build" host-builder reply — empty; the build prints its own progress to the
// shared stdio and signals failure via the host-builder error return (surfaced over the reverse channel).
type VmBuildReply struct{}
