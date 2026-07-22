// doctor.cue — wire types for the externalized `charly doctor` command plugin
// (candy/plugin-doctor; SDD conversion, per the standing operator directive: a
// hand-written wire struct not yet CUE-sourced is conversion-in-progress, never a
// sanctioned exception). NOT authoring kinds (never in #Node/#Op) — pure generated
// host<->plugin wire structs. The command LOGIC (the whole host-dependency
// report: check list, verdicts, human/JSON formatting, exit code, and the pure
// host ops — binary/file probes it runs itself) lives in the plugin. Only the
// genuine HOST-HARDWARE subsystem + core-owned data it cannot itself hold is
// reached via the generic "hostprobe" HostBuild kind: the GPU/VFIO/device
// detection primitives (the C11 shims, multi-caller with deploy/vm), the
// credential-store health (verb:credential, which lazy-connects host-side), and
// the core install-hint / device tables. The seam returns RAW FACTS ONLY — zero
// formatting or verdict logic crosses into core.

// #CredentialHealth is the credential-store health snapshot. Rendered into the
// doctor "secret storage" checks by the plugin.
#CredentialHealth: {
	backend_name!:       string @go(BackendName)
	configured_backend!: string @go(ConfiguredBackend)
	keyring_available!:  bool   @go(KeyringAvailable)
	keyring_locked!:     bool   @go(KeyringLocked)
	plaintext_count!:    int    @go(PlaintextCount,type=int)
	no_session!:         bool   @go(NoSession)
	coll_err?:           string @go(CollErr)
	healthy_colls?: [...string] @go(HealthyColls)
	broken_colls?: [...string] @go(BrokenColls)
	index_total!: int @go(IndexTotal,type=int)
	index_missing?: [...string] @go(IndexMissing)
}

// #HostProbeDevice is one host device-pattern probe result (the doctor
// "devices" section).
#HostProbeDevice: {
	pattern!:     string @go(Pattern)
	path!:        string @go(Path)
	present!:     bool   @go(Present)
	description!: string @go(Description)
}

// #HostProbeDistro is the host distro identity + package manager (drives
// install-hint rendering).
#HostProbeDistro: {
	id!:      string @go(ID)
	name!:    string @go(Name)
	manager!: string @go(Manager)
}

// #HostProbeRequest is the "hostprobe" HostBuild kind request. Engine hints
// which container engine's GPU run-flags to compute (empty → the host
// resolves it).
#HostProbeRequest: {
	engine?: string @go(Engine)
}

// #HostProbeReply is the "hostprobe" HostBuild kind reply — RAW host facts the
// plugin renders into the report. All fields are best-effort (a probe failure
// leaves its field zero/empty, mirroring the shims).
//
// group_accessible is RESHAPED from the former hand type's `map[int]bool` to a
// string-keyed map: `cue exp gengotypes` has no int-keyed-map construct (an
// int-keyed CUE map degrades to an empty struct — the documented CAN/CANNOT
// exception). encoding/json ALREADY converts a Go `map[int]bool`'s keys to
// their decimal string form on the wire (Go's own int->string key rule for
// map marshaling), so `map[string]bool` with the SAME decimal-string keys is
// BYTE-IDENTICAL JSON — a pure representation fix, zero wire-format change.
// The two Go call sites that constructed/read the int-keyed map
// (charly/host_build_hostprobe.go, candy/plugin-doctor/command.go) are updated
// in lockstep to key by strconv.Itoa(iommuGroup).
#HostProbeReply: {
	gpu!:             bool   @go(GPU)
	amd_gpu!:         bool   @go(AMDGPU)
	amd_gfx_version?: string @go(AMDGFXVersion)
	gpu_flags?: [...string] @go(GPUFlags)
	vfio?:               #VFIOReport @go(Vfio,optional=nillable)
	memlock_soft!:       int         @go(MemlockSoft,type=uint64)
	memlock_hard!:       int         @go(MemlockHard,type=uint64)
	vfio_pci_available!: bool        @go(VfioPciAvailable)
	group_accessible?: {[string]: bool} @go(GroupAccessible,type=map[string]bool)
	devices?: [...#HostProbeDevice] @go(Devices)
	distro!: #HostProbeDistro @go(Distro)
	install_hints?: {[string]: {[string]: string}} @go(InstallHints,type=map[string]map[string]string)
	distro_family_map?: {[string]: string} @go(DistroFamilyMap)
	config_path?:        string            @go(ConfigPath)
	credential?:         #CredentialHealth @go(Credential,optional=nillable)
	credential_err?:     string            @go(CredentialErr)
	error?:              string            @go(Error)
}
