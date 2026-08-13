package packagekit

// arch_map.go — the per-format arch mapping from the nFPM arch-mapping docs
// (https://nfpm.goreleaser.com/docs/arch-mapping/). The plugin takes a GOARCH name
// (amd64/arm64) and maps it per format:
//
//	deb:       amd64 → amd64, arm64 → arm64   (Go-style passthrough)
//	rpm/apk:   amd64 → x86_64, arm64 → aarch64
//	archlinux: amd64 → x86_64, arm64 → aarch64
//	ipk:       amd64 → x86_64, arm64 → arm64
//	msix:      amd64 → x64,    arm64 → arm64
//
// The mapping is applied to Info.Arch for deb/ipk and to the per-format Arch field
// (RPM.Arch / APK.Arch / ArchLinux.Arch / MSIX.Arch) for the others — nFPM's
// PrepareForPackager reads the per-format field when set.

// ArchMap returns the nFPM arch name for a GOARCH name + format.
func ArchMap(format, goarch string) string {
	switch format {
	case "deb":
		return goarch
	case "rpm", "apk", "archlinux":
		switch goarch {
		case "amd64":
			return "x86_64"
		case "arm64":
			return "aarch64"
		}
	case "ipk":
		switch goarch {
		case "amd64":
			return "x86_64"
		case "arm64":
			return "arm64"
		}
	case "msix":
		switch goarch {
		case "amd64":
			return "x64"
		case "arm64":
			return "arm64"
		}
	}
	return goarch
}
