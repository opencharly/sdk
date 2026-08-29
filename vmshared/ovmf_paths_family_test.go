package vmshared

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// ovmfPathTable still reads the consumer's embedded vocabulary through the
// UnmarshalEmbeddedDefaults hook, which is nil in a bare unit test. Supplying a minimal
// fixture is what makes the candidate path testable at all.
//
// Worth noting: this hook used to be needed for BOTH halves of the OVMF lookup. The
// distro->family half now comes from spec.DistroOvmfFamilies, so only the path data
// still depends on an embedded file.
const ovmfPathsFixture = `
ovmf_paths:
  fedora:
    secure:
      - code: /usr/share/OVMF/OVMF_CODE.secboot.fd
        vars: /usr/share/OVMF/OVMF_VARS.secboot.fd
    nonsecure:
      - code: /usr/share/OVMF/OVMF_CODE.fd
        vars: /usr/share/OVMF/OVMF_VARS.fd
  arch:
    secure:
      - code: /usr/share/edk2/x64/OVMF_CODE.secboot.4m.fd
        vars: /usr/share/edk2/x64/OVMF_VARS.4m.fd
    nonsecure:
      - code: /usr/share/edk2/x64/OVMF_CODE.4m.fd
        vars: /usr/share/edk2/x64/OVMF_VARS.4m.fd
  debian:
    secure:
      - code: /usr/share/OVMF/OVMF_CODE_4M.secboot.fd
        vars: /usr/share/OVMF/OVMF_VARS_4M.fd
    nonsecure:
      - code: /usr/share/OVMF/OVMF_CODE_4M.fd
        vars: /usr/share/OVMF/OVMF_VARS_4M.fd
`

func withEmbeddedOvmfPaths(t *testing.T) {
	t.Helper()
	prev := UnmarshalEmbeddedDefaults
	UnmarshalEmbeddedDefaults = func(dst any) {
		if err := yaml.Unmarshal([]byte(ovmfPathsFixture), dst); err != nil {
			t.Fatalf("decoding the ovmf_paths fixture: %v", err)
		}
	}
	t.Cleanup(func() { UnmarshalEmbeddedDefaults = prev })
}

// The distro -> OVMF family table now comes from spec.DistroOvmfFamilies instead of an
// ovmf_distro_aliases: directive parsed out of an embedded charly.yml. Two things had to
// survive that move, and one thing had to change.
func TestOvmfDistroAliasesComeFromTheDistroVocabulary(t *testing.T) {
	got := ovmfDistroAliases()

	// Survives: every mapping the old hand-maintained table carried.
	for id, want := range map[string]string{
		"fedora": "fedora", "rhel": "fedora", "centos": "fedora", "rocky": "fedora",
		"debian": "debian", "ubuntu": "debian",
		"arch": "arch", "manjaro": "arch", "endeavouros": "arch",
	} {
		if got[id] != want {
			t.Errorf("ovmfDistroAliases()[%q] = %q, want %q", id, got[id], want)
		}
	}

	// Survives: an unlisted distro has NO entry, which is what selects the
	// all-families union rather than a wrong firmware layout.
	if fam, ok := got["nixos"]; ok {
		t.Errorf("an unknown distro resolved to %q; the union fallback is gone", fam)
	}

	// Changes: cachyos now has a family. The old table predated the distro, so a
	// CachyOS host fell through to the union.
	if got["cachyos"] != "arch" {
		t.Errorf("ovmfDistroAliases()[cachyos] = %q, want arch", got["cachyos"])
	}
}

// The candidate selection must actually consume the table — a family that resolves but
// selects nothing would be worse than no entry at all.
func TestOvmfCandidatesUseTheFamily(t *testing.T) {
	withEmbeddedOvmfPaths(t)
	cachy := ovmfCandidatesForDistro("cachyos", false)
	arch := ovmfCandidatesForDistro("arch", false)
	if len(cachy) == 0 {
		t.Fatal("cachyos yields no OVMF candidates")
	}
	// Same family must yield the same candidates; that is what the mapping BUYS.
	if len(cachy) != len(arch) || (len(cachy) > 0 && cachy[0] != arch[0]) {
		t.Errorf("cachyos candidates %v differ from arch's %v despite sharing a family",
			cachy, arch)
	}
	// An unknown distro still gets the union, which is strictly larger than one family.
	if unknown := ovmfCandidatesForDistro("nixos", false); len(unknown) <= len(arch) {
		t.Errorf("unknown distro got %d candidates, want more than one family's %d — "+
			"the union fallback is what makes an unlisted distro still bootable",
			len(unknown), len(arch))
	}
}
