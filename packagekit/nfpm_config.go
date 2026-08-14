package packagekit

// nfpm_config.go — build nfpm.Info structs (struct-direct, no yaml round-trip)
// from the packaging section + the build inputs. Contents = the binary at
// /usr/bin/charly + the variant's plugins at /usr/lib/charly/plugins/ (+ their
// .providers manifests).

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/goreleaser/nfpm/v2"
	"github.com/goreleaser/nfpm/v2/files"
	"github.com/opencharly/spec/spec"
)

// BuildOptions is the input to a package build: the binary + plugins to package,
// the version/arch/variant, the formats, and the signing keys.
type BuildOptions struct {
	Binary     string   // the charly binary to package
	PluginsDir string   // the plugins dir to package
	Version    string   // the CalVer
	Arch       string   // GOARCH name (amd64/arm64)
	Variant    string   // the variant name ("" = the format's default)
	Formats    []string // the nFPM formats to build
	Out        string   // the output dir
	SigningKey string   // deb/rpm GPG key file (passphrase via NFPM_PASSPHRASE)
	APKKey     string   // apk RSA key file (passphrase via NFPM_APK_PASSPHRASE)
	MSIXPFX    string   // msix PFX cert file (passphrase via NFPM_MSIX_PASSPHRASE)
	MSIXLogo   string   // msix logo file (required by the msix packager)
}

// BuildInfo constructs the nfpm.Info for one (format, variant) from the packaging
// section + the build options. A variant naming a plugin absent from the plugins
// dir fails loudly (R4 — never a silently-dropped plugin).
func BuildInfo(pkg *spec.Packaging, format string, opts BuildOptions) (*nfpm.Info, error) {
	name, plugins, err := ResolveVariant(pkg, format, opts.Variant)
	if err != nil {
		return nil, err
	}
	f := pkg.Formats[format]
	if f == nil {
		return nil, fmt.Errorf("packaging.formats has no %q entry", format)
	}
	contents, err := buildContents(opts.Binary, opts.PluginsDir, plugins)
	if err != nil {
		return nil, err
	}
	arch := ArchMap(format, opts.Arch)

	info := &nfpm.Info{
		Overridables: nfpm.Overridables{
			Depends:    f.Depends,
			Recommends: f.Recommends,
			Suggests:   f.Suggests,
			Contents:   contents,
		},
		Name:        name,
		Arch:        arch,
		Version:     opts.Version,
		Description: pkg.Description,
		Maintainer:  pkg.Maintainer,
		Homepage:    pkg.Homepage,
		License:     pkg.License,
		Section:     pkg.Section,
		Priority:    pkg.Priority,
	}

	// Per-format arch + signing.
	switch format {
	case "deb":
		info.Deb.Arch = arch
		if opts.SigningKey != "" {
			info.Deb.Signature.KeyFile = opts.SigningKey
		}
	case "rpm":
		info.RPM.Arch = arch
		if opts.SigningKey != "" {
			info.RPM.Signature.KeyFile = opts.SigningKey
		}
	case "apk":
		info.APK.Arch = arch
		if opts.APKKey != "" {
			info.APK.Signature.KeyFile = opts.APKKey
			info.APK.Signature.KeyName = "charly"
		}
	case "archlinux":
		info.ArchLinux.Arch = arch
	case "ipk":
		info.IPK.Arch = arch
	case "msix":
		info.MSIX.Arch = arch
		info.MSIX.Publisher = f.Publisher
		info.MSIX.Properties.DisplayName = pkg.Name
		info.MSIX.Properties.PublisherDisplayName = pkg.Name
		info.MSIX.Properties.Logo = opts.MSIXLogo
		info.MSIX.Applications = []nfpm.MSIXApplication{{
			ID:         pkg.Name,
			Executable: "charly.exe",
			VisualElements: nfpm.MSIXVisualElements{
				DisplayName: pkg.Name,
				Description: pkg.Description,
			},
		}}
		if opts.MSIXPFX != "" {
			info.MSIX.Signature.PFXFile = opts.MSIXPFX
		}
	}

	return info, nil
}

// buildContents assembles the package contents: the binary at /usr/bin/charly +
// the variant's plugins at /usr/lib/charly/plugins/ (+ their .providers
// manifests, when present).
func buildContents(binary, pluginsDir string, plugins []string) (files.Contents, error) {
	if _, err := os.Stat(binary); err != nil {
		return nil, fmt.Errorf("binary %s: %w", binary, err)
	}
	contents := files.Contents{
		{
			Source:      binary,
			Destination: "/usr/bin/charly",
			FileInfo: &files.ContentFileInfo{
				Mode: 0o755,
			},
		},
	}
	for _, p := range plugins {
		bin := filepath.Join(pluginsDir, p)
		if _, err := os.Stat(bin); err != nil {
			return nil, fmt.Errorf("variant plugin %q: %s: %w", p, bin, err)
		}
		contents = append(contents, &files.Content{
			Source:      bin,
			Destination: filepath.Join("/usr/lib/charly/plugins/", p),
			FileInfo:    &files.ContentFileInfo{Mode: 0o755},
		})
		prov := bin + ".providers"
		if _, err := os.Stat(prov); err == nil {
			contents = append(contents, &files.Content{
				Source:      prov,
				Destination: filepath.Join("/usr/lib/charly/plugins/", p+".providers"),
				FileInfo:    &files.ContentFileInfo{Mode: 0o644},
			})
		}
	}
	return contents, nil
}
