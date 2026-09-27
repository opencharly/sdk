package packagekit

import (
	"strings"
	"testing"
)

const fixtureCharlyYAML = `charly:
    candy:
        version: 2026.225.1200
        description: The full charly toolchain on a deployment.
        packaging:
            name: charly
            description: The charly CLI + toolchain
            maintainer: OpenCharly <dev@opencharly.ai>
            homepage: https://opencharly.ai
            license: MIT
            section: utils
            priority: optional
            variants:
                default:
                    description: The default charly plugin set
                    plugins: [plugin-secrets, plugin-udev, plugin-preempt, plugin-feature, plugin-vm, plugin-doctor, plugin-clean, plugin-settings, plugin-candy]
                minimal:
                    description: A minimal charly plugin set
                    plugins: [plugin-doctor, plugin-clean]
            formats:
                deb:
                    depends: [libc6]
                    default_variant: default
                archlinux:
                    depends: [glibc]
                    optdepends:
                        qemu-full: full-system VM support
                        libvirt: libvirt VM support
                rpm:
                    depends: [glibc]
`

func TestParsePackaging(t *testing.T) {
	pkg, err := ParsePackaging([]byte(fixtureCharlyYAML))
	if err != nil {
		t.Fatalf("ParsePackaging: %v", err)
	}
	if pkg.Name != "charly" {
		t.Errorf("Name = %q, want charly", pkg.Name)
	}
	if pkg.Maintainer != "OpenCharly <dev@opencharly.ai>" {
		t.Errorf("Maintainer = %q", pkg.Maintainer)
	}
	if len(pkg.Variants) != 2 {
		t.Errorf("Variants = %d entries, want 2", len(pkg.Variants))
	}
	if len(pkg.Variants["default"].Plugins) != 9 {
		t.Errorf("default variant plugins = %d, want 9", len(pkg.Variants["default"].Plugins))
	}
	arch := pkg.Formats["archlinux"]
	if arch == nil {
		t.Fatal("no archlinux format entry")
	}
	if arch.OptDepends["qemu-full"] != "full-system VM support" {
		t.Errorf("archlinux optdepends qemu-full = %q", arch.OptDepends["qemu-full"])
	}
}

func TestParsePackagingMissing(t *testing.T) {
	_, err := ParsePackaging([]byte("charly:\n    candy:\n        version: 2026.225.1200\n"))
	if err == nil {
		t.Fatal("expected error for missing packaging section")
	}
	if !strings.Contains(err.Error(), "no packaging") {
		t.Errorf("error = %v, want 'no packaging'", err)
	}
}

// fixtureRealCandyCharlyYAML mirrors a REAL candy charly.yml: a top-level
// `version: <calver>` SCALAR appears FIRST, before the `charly:` candy key.
// This is the shape that broke ParsePackaging — the old implementation
// unmarshalled the whole document into map[string]struct{…} and failed on the
// scalar before reaching the candy body, so `charly generate-packages` errored
// on every real candy (reproduced against layer-charly and plugin-check). The
// earlier fixture hid the bug by indenting `version:` INSIDE `candy:`.
const fixtureRealCandyCharlyYAML = `version: 2026.261.1747
charly:
    candy:
        version: 2026.243.0100
        description: The full charly toolchain on a deployment.
        packaging:
            name: charly
            description: The charly CLI + toolchain
            maintainer: OpenCharly <dev@opencharly.ai>
            variants:
                default:
                    description: The default charly plugin set
                    plugins: [plugin-secrets, plugin-udev]
                minimal:
                    description: A minimal charly plugin set
                    plugins: [plugin-doctor]
`

func TestParsePackaging_TopLevelScalarVersion(t *testing.T) {
	pkg, err := ParsePackaging([]byte(fixtureRealCandyCharlyYAML))
	if err != nil {
		t.Fatalf("ParsePackaging with top-level version scalar: %v", err)
	}
	if pkg.Name != "charly" {
		t.Errorf("Name = %q, want charly", pkg.Name)
	}
	if pkg.Maintainer != "OpenCharly <dev@opencharly.ai>" {
		t.Errorf("Maintainer = %q", pkg.Maintainer)
	}
	if len(pkg.Variants) != 2 {
		t.Errorf("Variants = %d entries, want 2", len(pkg.Variants))
	}
	if len(pkg.Variants["default"].Plugins) != 2 {
		t.Errorf("default variant plugins = %d, want 2", len(pkg.Variants["default"].Plugins))
	}
}
