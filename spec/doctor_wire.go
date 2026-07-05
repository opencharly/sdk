package spec

// doctor_wire.go — wire types for the externalized `charly doctor` command plugin (candy/plugin-doctor).
// The command LOGIC (the whole host-dependency report: check list, verdicts, human/JSON formatting, exit
// code, and the pure host ops — binary/file probes it runs itself) lives in the plugin. Only the genuine
// HOST-HARDWARE subsystem + core-owned data it cannot itself hold is reached via the generic "hostprobe"
// HostBuild kind: the GPU/VFIO/device detection primitives (the C11 shims, multi-caller with deploy/vm),
// the credential-store health (verb:credential, which lazy-connects host-side), and the core install-hint
// / device tables. The seam returns RAW FACTS ONLY — zero formatting or verdict logic crosses into core.

// CredentialHealth is the credential-store health snapshot (moved here from charly core — it now has core
// + plugin-doctor consumers, R3). Rendered into the doctor "secret storage" checks by the plugin.
type CredentialHealth struct {
	BackendName       string   `json:"backend_name"`
	ConfiguredBackend string   `json:"configured_backend"`
	KeyringAvailable  bool     `json:"keyring_available"`
	KeyringLocked     bool     `json:"keyring_locked"`
	PlaintextCount    int      `json:"plaintext_count"`
	NoSession         bool     `json:"no_session"`
	CollErr           string   `json:"coll_err,omitempty"`
	HealthyColls      []string `json:"healthy_colls,omitempty"`
	BrokenColls       []string `json:"broken_colls,omitempty"`
	IndexTotal        int      `json:"index_total"`
	IndexMissing      []string `json:"index_missing,omitempty"`
}

// HostProbeDevice is one host device-pattern probe result (the doctor "devices" section).
type HostProbeDevice struct {
	Pattern     string `json:"pattern"`
	Path        string `json:"path"`
	Present     bool   `json:"present"`
	Description string `json:"description"`
}

// HostProbeDistro is the host distro identity + package manager (drives install-hint rendering).
type HostProbeDistro struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Manager string `json:"manager"`
}

// HostProbeRequest is the "hostprobe" HostBuild kind request. Engine hints which container engine's
// GPU run-flags to compute (empty → the host resolves it).
type HostProbeRequest struct {
	Engine string `json:"engine,omitempty"`
}

// HostProbeReply is the "hostprobe" HostBuild kind reply — RAW host facts the plugin renders into the
// report. All fields are best-effort (a probe failure leaves its field zero/empty, mirroring the shims).
type HostProbeReply struct {
	GPU              bool                         `json:"gpu"`
	AMDGPU           bool                         `json:"amd_gpu"`
	AMDGFXVersion    string                       `json:"amd_gfx_version,omitempty"`
	GPUFlags         []string                     `json:"gpu_flags,omitempty"`
	Vfio             *VFIOReport                  `json:"vfio,omitempty"`
	MemlockSoft      uint64                       `json:"memlock_soft"`
	MemlockHard      uint64                       `json:"memlock_hard"`
	VfioPciAvailable bool                         `json:"vfio_pci_available"`
	GroupAccessible  map[int]bool                 `json:"group_accessible,omitempty"`
	Devices          []HostProbeDevice            `json:"devices,omitempty"`
	Distro           HostProbeDistro              `json:"distro"`
	InstallHints     map[string]map[string]string `json:"install_hints,omitempty"`
	DistroFamilyMap  map[string]string            `json:"distro_family_map,omitempty"`
	ConfigPath       string                       `json:"config_path,omitempty"`
	Credential       *CredentialHealth            `json:"credential,omitempty"`
	CredentialErr    string                       `json:"credential_err,omitempty"`
	Error            string                       `json:"error,omitempty"`
}
