package spec

// vm_wire.go — the resolve-to-envelope form of the `vm` substrate value (Cutover L).
// candy/plugin-substrate resolves an authored `vm:` template into a ResolvedVm the
// kernel's vm build/deploy consumers read without importing the concrete spec.Vm; the
// vm-specific render (libvirt XML / cloud-init) stays in sdk/vmshared behind the
// lifecycle seam. ResolvedVm mirrors spec.Vm's fields so every consumer moves via the
// one VmSpec alias flip.
type ResolvedVm struct {
	Source    VmSource       `yaml:"source,omitempty" json:"source"`
	DiskSize  string         `yaml:"disk_size,omitempty" json:"disk_size,omitempty"`
	Ram       string         `yaml:"ram,omitempty" json:"ram,omitempty"`
	Cpus      int            `yaml:"cpu,omitempty" json:"cpu,omitempty"`
	Machine   string         `yaml:"machine,omitempty" json:"machine,omitempty"`
	Firmware  string         `yaml:"firmware,omitempty" json:"firmware"`
	Backend   string         `yaml:"backend,omitempty" json:"backend"`
	Autostart bool           `yaml:"autostart,omitempty" json:"autostart"`
	Network   *VmNetwork     `yaml:"network,omitempty" json:"network,omitempty"`
	SSH       *VmSSH         `yaml:"ssh,omitempty" json:"ssh,omitempty"`
	CloudInit *VmCloudInit   `yaml:"cloud_init,omitempty" json:"cloud_init,omitempty"`
	Libvirt   *LibvirtDomain `yaml:"libvirt,omitempty" json:"libvirt,omitempty"`
	Plan      []Step         `yaml:"plan,omitempty" json:"plan,omitempty"`
	Snapshots []VmSnapshot   `yaml:"snapshot,omitempty" json:"snapshot,omitempty"`
	Raw       RawBody        `yaml:"-" json:"raw,omitempty"`
}
