---
name: ovmf
description: |-
  UEFI firmware (OVMF_CODE + OVMF_VARS) path resolution for VMs — per-distro path table, per-VM NVRAM copies, secure-boot variants, the empty-strings=skip contract. Use before editing UEFI firmware resolution.
---

# ovmf

Per-distro UEFI firmware path resolution for VMs. `ResolveOvmfPaths` picks the correct OVMF_CODE + OVMF_VARS pair for the host distro + secure-boot setting; `EnsurePerVmNvram` copies the shared OVMF_VARS template to a per-VM writable NVRAM file on first use. `ResolveOvmfForSpec` is the convenience wrapper that reads `spec.Firmware` and dispatches.

**Key contract: when `spec.Firmware == "bios"` or empty, `ResolveOvmfForSpec` returns `("", "", nil)`** — no OVMF package dependency, no per-VM NVRAM file, no `<loader>`/`<nvram>` emission. The libvirt renderer uses these empty strings as a sentinel to skip UEFI entirely.

## Source file

`sdk/vmshared/ovmf_paths.go` — ~185 LOC; standalone, stdlib-only (no dependencies on the rest of `sdk/vmshared/`).

## Per-distro path table

```
Fedora / CentOS / RHEL / Rocky / Alma:
  insecure:  /usr/share/OVMF/OVMF_CODE.fd + /usr/share/OVMF/OVMF_VARS.fd
  secure:    /usr/share/OVMF/OVMF_CODE.secboot.fd + /usr/share/OVMF/OVMF_VARS.secboot.fd
  (fallback): /usr/share/edk2/ovmf/...       — pre-F40

Arch / Manjaro / EndeavourOS:
  insecure:  /usr/share/edk2/x64/OVMF_CODE.4m.fd + /usr/share/edk2/x64/OVMF_VARS.4m.fd
  secure:    /usr/share/edk2/x64/OVMF_CODE.secboot.4m.fd + (same VARS template)
  (fallback): /usr/share/edk2-ovmf/x64/...   — pre-edk2-ovmf-202411 repackage

Debian / Ubuntu:
  insecure:  /usr/share/OVMF/OVMF_CODE_4M.fd + /usr/share/OVMF/OVMF_VARS_4M.fd
  secure:    /usr/share/OVMF/OVMF_CODE_4M.ms.fd + /usr/share/OVMF/OVMF_VARS_4M.ms.fd
  (fallback): /usr/share/OVMF/OVMF_CODE.fd   — older non-4M paths
```

Multiple candidates per distro handle historical path drift. `ResolveOvmfPaths` returns the first candidate pair where both files exist. Clean error when no candidate matches, with a distro-appropriate install hint:

| Distro family | Install hint |
|---|---|
| Fedora / CentOS / RHEL | `sudo dnf install edk2-ovmf` |
| Arch | `sudo pacman -S edk2-ovmf` |
| Debian / Ubuntu | `sudo apt-get install ovmf` |

## Per-VM NVRAM pattern

`EnsurePerVmNvram(templatePath, perVmDir)` copies the OVMF_VARS template to `<perVmDir>/nvram.fd` on first use. **Idempotent**: if the per-VM file already exists, it's preserved (the guest has accumulated UEFI variables into it — boot entries, secure-boot state, etc., which must survive reboot).

The shared template stays read-only. Every VM that boots with UEFI gets its own NVRAM copy; no two VMs share NVRAM state.

Per-VM NVRAM path convention: `~/.local/share/charly/vm/charly-<vm-name>/nvram.fd`.

## Secure-boot lock-in warning

`firmware: uefi-secure` hard-wires the VM to Microsoft UEFI CA keys (enrolled in the secure-boot OVMF_VARS template). Unsigned EFI binaries fail to boot; dual-booting unsigned kernels or self-built EFI loaders requires enrolling custom keys through the OVMF console. Not reversible without a fresh per-VM NVRAM.

`firmware: uefi-insecure` uses the same OVMF binary but boots any EFI payload. Most cloud images ship signed BOOTX64.EFI binaries that work under either mode; test before committing.

## BIOS as an escape valve

From `/charly-vm:arch-cloud-vm` Finding B: UEFI boot with a stale BOOTX64.EFI (embedded grub.cfg older than the image's on-disk `/boot/grub/grub.cfg`) is a common failure mode for distribution cloud images. BIOS boot sidesteps it entirely — GRUB reads `/boot/grub/grub.cfg` from the root filesystem, which is always current.

When `spec.Firmware == "bios"`:

```go
func ResolveOvmfForSpec(spec *VmSpec, vmStateDir string) (codePath, nvramPath string, err error) {
    if spec.Firmware == "" || spec.Firmware == "bios" {
        return "", "", nil            // ← the sentinel
    }
    // ... UEFI path below
}
```

The libvirt renderer (`buildDomainOS` in `libvirt_yaml_bridge.go`) emits `<loader>` + `<nvram>` only when `VmRuntimeParams.OVMFCodePath` / `.NVRAMPath` are non-empty; for BIOS, `ResolveOvmfForSpec` returns empty paths (resolved upstream in `candy/plugin-vm/vm_create_orchestrate.go`) so both are skipped. QEMU then boots via SeaBIOS (shipped with QEMU itself, no separate package). No OVMF on the host package-manager list. No per-VM NVRAM file. No Secure Boot lock-in.

**When to pick BIOS**:

- Cloud images that ship a GPT BIOS boot partition (vda1, 1 MB) and a potentially-stale ESP. Typical of Arch, Ubuntu Cloud, Debian Cloud, Fedora Cloud.
- Guests that don't need Secure Boot.
- Hosts where OVMF isn't installed and shouldn't be.

**When to pick UEFI**:

- bootc images built explicitly for UEFI (most Fedora CoreOS / bootc variants — their disk layout assumes ESP-only boot).
- Guests that need Secure Boot.
- ARM hosts (aarch64 — BIOS is not a practical choice there).

## Cross-References

- `/charly-internals:vm-spec` — `spec.Firmware` field
- `/charly-internals:libvirt-renderer` — `RenderDomainXML` consumer; `<loader>`/`<nvram>` emission conditions
- `/charly-vm:vm` — command-family; BIOS vs UEFI decision matrix
- `/charly-vm:arch-cloud-vm` — live-test RCA showing why `firmware: bios` is the right default for Arch cloud image
