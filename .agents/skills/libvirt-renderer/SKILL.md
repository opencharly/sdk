---
name: libvirt-renderer
description: |-
  Translation facade from VmSpec + LibvirtDomain to libvirt domain XML and QEMU argv — RenderDomainXML, device emission (passt, portForward, virtio-gpu), firmware plumbing, LibvirtDomain schema. Use before editing libvirt XML emission.
---

# libvirt-renderer

The libvirt renderer converts `VmSpec` + `LibvirtDomain` into a libvirt domain XML (`RenderDomainXML`, which builds a `libvirtxml.Domain` tree via `BuildLibvirtDomainXML` and marshals it) and — for the direct-QEMU backend — into a QEMU argv array (`RenderQemuArgv`). Pure functions: given the same inputs, they produce the same output; no side effects. The opencharly YAML layer is a translation facade over `libvirt.org/go/libvirtxml`, not an independent schema. Consumed by `charly vm create` (libvirt path) and `charly vm create --backend qemu` (direct path).

## Source files

| File | Contents | LOC |
|---|---|---|
| `sdk/vmshared/libvirt_yaml.go` | stub — the per-device structs moved to `#LibvirtDomain` in `spec/schema/vm.cue` (generated into `spec/spec/cue_types_gen.go`); `RenderDomainXML`/`BuildLibvirtDomainXML` moved to `plugin-vm/candy/plugin-vm/libvirt_yaml_bridge.go` | ~3 |
| `plugin-vm/candy/plugin-vm/libvirt_yaml_bridge.go` | `RenderDomainXML`/`BuildLibvirtDomainXML` top-level composition (builds a `libvirtxml.Domain`); `buildDomainDevices` `<devices>` child emitters — channels, graphics, video, rng, memballoon, hostdev, interface (with portForward), filesystem; firmware plumbing (D17); SMBIOS credentials; `XMLPassthrough` merge | ~1800 |
| `sdk/vmshared/libvirt_helpers.go` | helpers shared by the libvirt YAML bridge + `qemu_render` argv emitter (incl. `VmRuntimeParams`) | ~160 |
| `sdk/vmshared/libvirt_yaml_listen.go` | structured `<listen>` support for `LibvirtGraphics` | ~24 |
| `sdk/vmshared/qemu_render.go` | `RenderQemuArgv` for direct-QEMU backend | ~340 |
| `spec/schema/vm.cue` | `#LibvirtDomain` — the closed CUE schema for the libvirt subtree (enums/ranges/PCI-hex + the `uefi-secure ⇒ smm` cross-rule); registered per-kind via the spec CUE registry. Validation lives here, not in Go | ~720 |

## LibvirtDomain top-level

```go
type LibvirtDomain struct {
    Snippets       []string                // raw XML escape hatch (candy-composed), classified by isDeviceElement
    XMLPassthrough string                  // verbatim libvirt XML fragments, merged via libvirtxml.Unmarshal at render time
    Features       *LibvirtFeatures        // ACPI/APIC/PAE/SMM/HAP/VMPort/PMU/HyperV/KVM
    CPU            *LibvirtCPU             // mode, model, features, topology, numa
    Clock          *LibvirtClock           // offset, timers
    MemoryBacking  *LibvirtMemoryBacking   // hugepages, nosharepages, locked, source
    MemTune        *LibvirtMemTune         // hard_limit, soft_limit, swap_hard_limit
    NUMATune       *LibvirtNUMATune        // memory, memnode
    CPUTune        *LibvirtCPUTune         // vcpupin, emulatorpin, iothreadpin, shares, period
    IOThreads      int
    Devices        *LibvirtDevices         // interfaces, disks, channels, graphics, video, rng, …
    SecLabel       *LibvirtSecLabel        // SELinux/AppArmor
    LaunchSecurity *LibvirtLaunchSecurity  // AMD SEV / Intel TDX
    Resource       *LibvirtResource        // partition
    SysInfo        *LibvirtSysInfo         // SMBIOS — type 11 OEM strings used for ssh-key injection
}
```

**Structured first; raw XML is a last resort.** `Snippets` exists for the rare case where libvirt gained a new element before charly's schema caught up. The legacy list-of-strings form `libvirt: ["<xml>", ...]` on `candy:` image entries was deleted in the cutover — raw XML now lives only in `<name>.vm.libvirt.snippets:` (new), on layer-level `libvirt.snippets:` fields, and the `InjectLibvirtXML` post-processor still handles both paths.

## Device-level gotchas learned in live testing

### `<backend type='passt'/>` required for `<portForward>`

libvirt ≥ 9.x `<portForward>` elements on `<interface type='user'>` only activate when the interface declares `<backend type='passt'/>`. Without it, libvirt silently drops the port mapping — guest is reachable internally but host has nothing on the forwarded port. Emitted automatically in `renderDefaultInterface`; if you author a custom interface in `libvirt.devices.interfaces[]`, include the backend line.

### `<portForward>` start/to attribute order

The element's attributes are **start=HOST port, to=GUEST port** (not the other way around). Reversing them maps incoming guest traffic to the host, which is exactly wrong. The renderer enforces this ordering; unit tests in `libvirt_test.go` guard against regression. The live-test bisect that caught this: ssh on port 2224 connected but landed in the wrong port inside the guest.

### virtio-gpu is the default video model for Linux guests

Document rationale for *every* cloud_image VM author:

| Model | Pros | Cons | When to pick |
|---|---|---|---|
| `virtio` (virtio-gpu) | Native `virtio_gpu` kernel DRM driver; Wayland-native; simpler config (just `vram` + `heads`); optional `accel3d: true` for virgl OpenGL passthrough | Only modern Linux kernels (4.16+) | **default for Linux guests** |
| `qxl` | Legacy SPICE-specific (2010 Red Hat); X11-oriented; requires xf86-video-qxl for acceleration | More knobs to tune (ram_size + vram_size + vram64_size_mb + vgamem_mb quartet); simpledrm→qxldrmfb takeover race under UEFI (see `/charly-vm:arch-cloud-vm` Finding B); X11-dominant | Legacy guests only |
| `cirrus` | Most compatible | Low resolution, no acceleration | BIOS-fallback only |
| `none` | No video | — | Headless VMs |

SPICE graphics protocol is video-model-agnostic — it streams pixels from virtio-gpu just as it does from QXL.

### PCI passthrough (`<hostdev>`) + NVIDIA Code-43 features

`mapHostdev` (`libvirt_yaml_bridge.go`) renders `libvirt.devices.hostdevs[]`. For
`type: pci` it emits `<hostdev managed='…' type='pci'><source><address …/></source>`
plus — when set — the optional `<rom bar='off'/file='…'/>` (from `rom:`) and
`<driver name='vfio'/>` (from `driver:`). `managed='yes'` makes libvirt auto-bind
the device to vfio-pci on VM start and rebind the host driver on stop. Every
function in the GPU's IOMMU group must be a separate hostdev entry (`charly vm gpu
list` emits the whole group). Passthrough wants `firmware: uefi-insecure` and the
libvirt backend (the QEMU-argv renderer skips PCI hostdev — see below).

`buildDomainFeatures` renders the two NVIDIA Code-43 workarounds for consumer
GPUs: `libvirt.features.kvm.hidden: on` → `<kvm><hidden state='on'/></kvm>`, and
`libvirt.features.hyperv.vendor_id: {state, value}` → `<hyperv><vendor_id …/>`.
`features.ibs` and the rest of the KVM struct (hint_dedicated / poll_control /
pv_ipi / dirty_ring) render too; other HyperV enlightenments stay available via
`libvirt.snippets`. The `#LibvirtDomain` CUE schema (`schema/vm.cue`) checks
hostdev type/managed enums and hex PCI source fields. Worked example: the CachyOS `check-cachyos-gpu-vm` bed.

### virtiofs filesystem shares + auto-paired shared memory

`mapFilesystem` (`libvirt_yaml_bridge.go`) renders `libvirt.devices.filesystems[]`
— `{driver: virtiofs|9p|path, accessmode, source: <host dir>, target: <mount tag>}`
→ `<filesystem type='mount' accessmode='…'><driver type='virtiofs'/><source dir='…'/><target dir='…'/></filesystem>`,
plus the optional virtiofsd `binary:` knobs (`path`, `xattr`, `cache`, `sandbox`,
`thread_pool` → `<binary …>`), useful for tuning the rootless `qemu:///session`
virtiofsd. **virtiofs requires shared guest memory** — `BuildLibvirtDomainXML`
calls `ensureVirtiofsSharedMemory`, which auto-injects
`<memoryBacking><source type='memfd'/><access mode='shared'/></memoryBacking>`
whenever a virtiofs share is present and no shared backing was declared (an
explicit backing, e.g. hugepages, is honored — only the missing source/access
bits are filled). Without this libvirt refuses to start the domain; auto-pairing
means an author never has to remember the coupling. The `#LibvirtDomain` CUE
schema requires `source`+`target` and checks the driver/accessmode enums — a literal
`/home/...` source is allowed (a share's purpose is to expose a host dir). The
guest mounts the tag with the `workspace-mount` layer (or `mount -t virtiofs`).

### virtiofs guest-user idmap (auto-paired, like shared memory)

`ensureVirtiofsIdmap` (`libvirt_yaml_bridge.go`, called right after
`ensureVirtiofsSharedMemory`) auto-injects a `<filesystem><idmap>` onto every
**passthrough** virtiofs share that doesn't already declare one. libvirt's
DEFAULT rootless idmap maps **guest-root → the host operator**, so a host-home
passthrough share is `root:root` inside the guest and the interactive guest
user (uid 1000) gets `Permission denied` — the "/workspace is mounted but I
can't read it" footgun. The injected idmap instead maps the guest's primary
user (uid/gid 1000, the cloud-init/pacstrap/debootstrap first-user convention)
to the host operator, with all other ids in the operator's `/etc/subuid` +
`/etc/subgid` range:

```
<idmap>
  <uid start='0'    target='100000' count='1000'/>   <!-- guest 0-999  → subuid -->
  <uid start='1000' target='1000'   count='1'/>       <!-- guest user   → host operator -->
  <uid start='1001' target='101000' count='64536'/>   <!-- guest 1001+  → subuid -->
  <gid …/>  <!-- same partition for gids -->
</idmap>
```

Built by `guestOwnerIDMap(guestID, hostID, subStart, subCount)` from
`subIDRange("/etc/subuid", …)`. An author-declared idmap, a non-passthrough
accessmode (`mapped`/`squash`), or a missing subordinate-ID range all leave
libvirt's own default untouched. This makes "mount /home/X into the VM" usable
by the guest's interactive user, which is what the operator means in practice.

### guest-agent unix channel

`mapChannel` renders `libvirt.devices.channels[]`. The qemu-guest-agent idiom is
`{type: unix, name: org.qemu.guest_agent.0}` with **no** explicit path → a
libvirt-managed socket (`<channel type='unix'><source mode='bind'/><target
type='virtio' name='org.qemu.guest_agent.0'/></channel>`); libvirt auto-assigns
the socket path under the per-VM lib dir. `extractChannelSocketPaths` pre-creates
the parent dir. (The same channel is also contributed as a raw snippet by the
`/charly-distros:qemu-guest-agent` layer for image-composed VMs.)

## Firmware plumbing (D17)

`buildDomainOS` (in `libvirt_yaml_bridge.go`, called from `BuildLibvirtDomainXML`) reads `spec.Firmware`. The OVMF paths are resolved UPSTREAM by the CLI layer (`candy/plugin-vm/vm_create_orchestrate.go` calls `ResolveOvmfForSpec` — see `/charly-internals:ovmf`) and passed into the renderer via `VmRuntimeParams.OVMFCodePath` / `.NVRAMPath`; the renderer itself never touches the host filesystem. For `firmware: uefi-insecure` / `uefi-secure` it sets `<os firmware='efi'>` with the matching secure-boot `<feature>` toggles, then emits a `<loader>`/`<nvram>` only when the runtime params are non-empty. Emits:

```xml
<os firmware='efi'>
  <type arch='x86_64' machine='pc-q35-...'>hvm</type>
  <firmware><feature enabled='yes|no' name='secure-boot'/>...</firmware>
  <loader readonly='yes' secure='yes|no' type='pflash'>{rt.OVMFCodePath}</loader>
  <nvram>{rt.NVRAMPath}</nvram>
</os>
```

When `spec.Firmware == "bios"` or empty, `ResolveOvmfForSpec` returns empty paths, `VmRuntimeParams.OVMFCodePath`/`.NVRAMPath` stay empty, and `buildDomainOS` emits neither `<loader>` nor `<nvram>` (the firmware `switch` has no bios case). No OVMF package dependency, no per-VM NVRAM file, no Secure Boot lock-in. This is what makes `/charly-vm:arch-cloud-vm` viable — BIOS boot bypasses the stale BOOTX64.EFI issue by never loading it.

`uefi-secure` does NOT auto-enable SMM: `buildDomainOS` emits the secure-boot
`<feature>` toggles + secure `<loader>`, but `buildDomainFeatures` only emits
`<smm state='on'/>` from an explicit `libvirt.features.smm: true`. Because Secure
Boot's authenticated variables require SMM, the author MUST declare
`libvirt.features.smm: true`, and the `#Vm` CUE schema enforces it — the
`uefi-secure ⇒ libvirt.features.smm:true` required-field rule rejects a
`uefi-secure` VM that omits it (`schema/vm.cue`).

## SMBIOS credentials (type 11 OEM strings)

When `VmSsh.KeyInjection.SMBIOS` is enabled, the renderer emits:

```xml
<sysinfo type='smbios'>
  <oemStrings>
    <entry>io.systemd.credential:ssh.authorized_keys.root=ssh-ed25519 AAAA…</entry>
  </oemStrings>
</sysinfo>
<os>
  <smbios mode='sysinfo'/>
  ...
</os>
```

systemd-ssh-generator (systemd ≥ v250) materializes the pubkey into `~<user>/.ssh/authorized_keys` during early boot — no cloud-init needed. Runs in parallel with cloud-init's own ssh_authorized_keys injection when both channels are on; dedup happens in the guest.

## RenderQemuArgv (direct-QEMU backend)

For `charly vm create --backend qemu`, `RenderQemuArgv` emits a flat array of arguments:

```
qemu-system-x86_64 -machine pc-q35-XX,accel=kvm -cpu host,migratable=off \
  -m 8G -smp cpus=4 -drive file={disk},if=virtio,format=qcow2 \
  -cdrom {seed.iso} -device virtio-net-pci,netdev=n0 \
  -netdev user,id=n0,hostfwd=tcp::2224-:22 -display none \
  -serial mon:stdio ...
```

Intended for environments without libvirt session daemon (some CI runners, air-gapped VMs). Libvirt is the preferred backend; direct QEMU has no portForward mechanism equivalent and no SPICE console wiring.

## Validation

The libvirt subtree is validated by the closed `#LibvirtDomain` CUE schema
(`spec/schema/vm.cue`, registered per-kind in the spec CUE registry) — there is no Go
libvirt validator. It rejects:

- unknown keys (a typo — the schema is closed, no trailing `...`).
- out-of-range enums (`cpu.mode`, `graphics.type`, `filesystem.driver`/`accessmode`,
  `hostdev.type`/`managed`, `clock.offset`, `launch_security.type`).
- missing required fields (`video.model`, `filesystem.source`/`target`, and — for a
  `pci` hostdev — hex `source.domain`/`bus`/`slot`/`function` via `#LibvirtPCIHex`).
- the cross-rules: `cpu.mode:custom ⇒ model`, and (on the `#Vm` parent)
  `firmware:uefi-secure ⇒ libvirt.features.smm:true`.

Raw `libvirt.snippets` are modeled typed-open (`[...string]`) and not
XML-parsed at config time — well-formedness of a snippet is caught only
indirectly, by the egress validation of the RENDERED domain XML below (a
malformed snippet breaks the render or fails `ValidateXMLEgress`), not by a
dedicated ingress-time XML check.

**Egress validation of the RENDERED domain XML** (distinct from the `#LibvirtDomain`
INGRESS validation above): `RenderDomainXML` runs `ValidateXMLEgress` on its output —
the XML is decoded via CUE's experimental `xml+koala` encoding and validated against
the koala-shaped `#LibvirtDomainXML` envelope (non-empty `$type`/`name.$$`/`memory.$$`),
catching a broken render or `XMLPassthrough` snippet before VM create. It is
**best-effort**: if koala cannot decode the XML it returns nil and defers to libvirt's
`DomainDefineXML`, which remains the authoritative gate. Owned by `/charly-internals:egress`.

## Cross-References

- `/charly-internals:vm-spec` — VmSpec shape the renderer reads
- `/charly-internals:ovmf` — `ResolveOvmfForSpec` for UEFI path resolution
- `/charly-internals:cloud-init-renderer` — paired renderer for seed ISO + user-data
- `/charly-internals:vm-deploy-target` — consumer that applies the rendered domain
- `/charly-vm:vm` — command-family skill; video-model decision table
- `/charly-vm:arch-cloud-vm` — BIOS decision RCA; virtio-gpu live-test bisect
- `/charly-distros:qemu-guest-agent` — virtio-serial channel snippet that this renderer emits in `<devices>`
