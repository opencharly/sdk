package packagekit

// build.go — call the nfpm packagers (deb/rpm/apk/archlinux/ipk/msix) via the
// library API. The blank imports register each packager with nfpm.Get.

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/goreleaser/nfpm/v2"
	_ "github.com/goreleaser/nfpm/v2/apk"
	_ "github.com/goreleaser/nfpm/v2/arch"
	_ "github.com/goreleaser/nfpm/v2/deb"
	_ "github.com/goreleaser/nfpm/v2/ipk"
	_ "github.com/goreleaser/nfpm/v2/msix"
	_ "github.com/goreleaser/nfpm/v2/rpm"
	"github.com/opencharly/spec/spec"
)

// Build builds the requested formats for one (variant, arch) and returns the
// written package file paths. Formats defaults to all registered nFPM formats.
func Build(pkg *spec.Packaging, opts BuildOptions) ([]string, error) {
	if len(opts.Formats) == 0 {
		opts.Formats = nfpm.Enumerate()
	}
	if err := os.MkdirAll(opts.Out, 0o755); err != nil {
		return nil, fmt.Errorf("create out dir: %w", err)
	}
	var written []string
	for _, format := range opts.Formats {
		path, err := buildOne(pkg, format, opts)
		if err != nil {
			return nil, err
		}
		written = append(written, path)
	}
	return written, nil
}

// buildOne builds a single format and returns the written package path.
func buildOne(pkg *spec.Packaging, format string, opts BuildOptions) (string, error) {
	packager, err := nfpm.Get(format)
	if err != nil {
		return "", err
	}
	info, err := BuildInfo(pkg, format, opts)
	if err != nil {
		return "", err
	}
	applyPassphrases(info)
	info = nfpm.WithDefaults(info)
	if err := nfpm.Validate(info); err != nil {
		return "", fmt.Errorf("validate %s package: %w", format, err)
	}
	if err := nfpm.PrepareForPackager(info, format); err != nil {
		return "", fmt.Errorf("prepare %s package: %w", format, err)
	}
	path := filepath.Join(opts.Out, packager.ConventionalFileName(info))
	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", path, err)
	}
	if err := packager.Package(info, f); err != nil {
		_ = f.Close() // best-effort close on the error path; the package error is primary
		return "", fmt.Errorf("build %s package: %w", format, err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close %s: %w", path, err)
	}
	// nFPM does not emit Arch optdepends; inject them into the .pkg.tar.zst.
	if format == "archlinux" {
		if err := InjectOptDepends(path, pkg.Formats[format].OptDepends); err != nil {
			return "", err
		}
	}
	return path, nil
}

// applyPassphrases mirrors nFPM's Parse-time env expansion for the
// struct-direct path: buildOne calls WithDefaults (not Parse), so the
// NFPM_*_PASSPHRASE env vars would otherwise never be read and a
// passphrase-protected signing key would fail with "key is encrypted but no
// passphrase was provided". Precedence matches nFPM: the general
// NFPM_PASSPHRASE fills deb/rpm/apk, and the per-format
// NFPM_DEB/RPM/APK/MSIX_PASSPHRASE overrides it.
func applyPassphrases(info *nfpm.Info) {
	general := os.Getenv("NFPM_PASSPHRASE")
	info.Deb.Signature.KeyPassphrase = general
	info.RPM.Signature.KeyPassphrase = general
	info.APK.Signature.KeyPassphrase = general
	if v := os.Getenv("NFPM_DEB_PASSPHRASE"); v != "" {
		info.Deb.Signature.KeyPassphrase = v
	}
	if v := os.Getenv("NFPM_RPM_PASSPHRASE"); v != "" {
		info.RPM.Signature.KeyPassphrase = v
	}
	if v := os.Getenv("NFPM_APK_PASSPHRASE"); v != "" {
		info.APK.Signature.KeyPassphrase = v
	}
	if v := os.Getenv("NFPM_MSIX_PASSPHRASE"); v != "" {
		info.MSIX.Signature.KeyPassphrase = v
	}
}
