---
name: cloud-init-renderer
description: |-
  Pure renderer from VmSpec + VmCloudInit to NoCloud seed ISO (user-data + meta-data + network-config). Covers composeUsers adopt-merge, SMBIOS vs cloud_init channels, xorriso ISO emission, charly_install.strategy. Use before editing cloud-init emission paths.
---

# cloud-init-renderer

Host-side renderer producing NoCloud seed ISOs for cloud_image VMs (and bootc VMs that include the `cloud-init` layer). Pure transformation — given `VmSpec` + `VmCloudInit`, produces three files (user-data, meta-data, network-config) and packages them into an ISO 9660 image labeled `CIDATA` via xorriso.

Lives **host-side**, in the `charly` binary. The **guest-side** `/charly-distros:cloud-init` layer is complementary: it puts the cloud-init package into the bootc guest OS so that guest reads the seed ISO. The two sides cooperate across the host/guest boundary.

## Source files

| File | Contents |
|---|---|
| `sdk/vmshared/cloud_init_render.go` | `RenderCloudInit`, `ResolveKeyInjectionChannels`, `composeUsers`, `composePackages`, `composeBootCmd`, `composeRunCmd` |
| `sdk/vmshared/cloud_init_iso.go` | `WriteSeedISO` via xorriso; `genisoimage` + `mkisofs` fallbacks |
| `spec/exec/charly_install.go` | `kit.EnsureCharlyInGuest` state machine (auto/scp/skip strategies) |
| `spec/spec/cue_types_gen.go` (generated) | `VmCloudInit`, `VmCloudInitUser`, `VmCloudInitFile`, `VmCloudInitNetwork`, `VmCloudInitMirrors`, `VmCharlyInstall` |

## RenderCloudInit top-level

```go
func RenderCloudInit(spec *VmSpec, rt CloudInitRuntimeParams) (userData, metaData, networkConfig string, err error)
```

Returns three strings — the three files NoCloud expects on the seed ISO. The caller (`BuildCloudImage` or the vm deploy preflight) then calls `WriteSeedISO` to pack them.

**Egress validation gate.** Before returning, each rendered document is validated
against a CUE schema — user-data against the vendored Canonical cloud-config
schema (`#CloudConfig`), meta-data against `#CloudInitMeta`, network-config against
`#NetworkConfigV2` (via `ValidateEgress`). A malformed render fails here, so a
cloud-init that its own schema would reject never reaches the seed ISO. The gate
is owned by `/charly-internals:egress`.

## composeUsers (adopt-merge pattern)

Single most important function in this renderer. Produces the `users:` list in user-data.

**When `spec.Source.BaseUser` is non-empty** (cloud_image adopt pattern):

```yaml
users:
  - default                  # cloud-init sentinel: preserve distro's default account
  - name: <base_user>
    ssh_authorized_keys:
      - <pubkey>
```

No `useradd`, no sudoers write, no shell change — cloud-init interprets "name: X" on an existing account as "append ssh_authorized_keys". The parity is exact with the container-side `base_user:` + `user_policy: adopt` pattern (`/charly-image:image` "user_policy").

**When `BaseUser` is empty and `VmSsh.User` is non-empty** (create pattern):

```yaml
users:
  - default
  - name: <ssh_user>
    sudo: ALL=(ALL) NOPASSWD:ALL
    groups: [wheel, sudo]
    shell: /bin/bash
    lock_passwd: true
    ssh_authorized_keys:
      - <pubkey>
```

Full account provisioning. Used by bootc VMs where no `base_user:` applies.

**When a user already appears in `spec.CloudInit.Users`**: the renderer appends the ssh pubkey to that existing entry instead of emitting a new one. Lets authors declare a user with specific sudo/groups/shell fields and still get the pubkey injected.

## ResolveKeyInjectionChannels

Applies the per-source-kind auto-defaults documented in `/charly-internals:vm-spec`:

```go
func ResolveKeyInjectionChannels(spec *VmSpec) (smbios bool, cloudInit bool) {
    if spec.SSH != nil && spec.SSH.KeyInjection != nil {
        // explicit overrides
        return spec.SSH.KeyInjection.SMBIOS == "enabled" (with "auto" = source-kind default),
               spec.SSH.KeyInjection.CloudInit == "enabled" (with "auto" = source-kind default)
    }
    // per-source-kind auto-defaults
    switch spec.Source.Kind {
    case "cloud_image":
        return true, true       // belt + suspenders
    case "bootc":
        return true, false      // cloud_init channel only activates with cloud-init layer
    }
}
```

**Both channels are additive** — when both are on, systemd-ssh-generator (SMBIOS path) and cloud-init (user-data path) both inject the key. Dedup happens in the guest's `authorized_keys`. There's no correctness issue with duplicate keys; the dual-injection pattern is the safe default.

## composePackages + composeBootCmd + composeRunCmd renderer defaults

The renderer prepends defaults to user-declared lists:

- `composePackages`: prepends `{openssh (or `openssh-server` on debian/ubuntu), curl, tar}` (deduplicated against user's `Packages`). Guarantees the guest has SSH server + download tools + tar for later layer application.
- `composeBootCmd`: prepends `systemctl mask ssh.socket || true` — the EARLIEST cloud-init phase (`bootcmd` runs before `write_files`/packages/`runcmd`), so a socket-activated sshd (enabled by default on some cloud images, notably Debian/Ubuntu) can never accept a connection before cloud-init has finished configuring the guest. `|| true` makes this a harmless no-op on a distro that ships no `ssh.socket` unit at all (Arch/Fedora typically don't).
- `composeRunCmd`: prepends the hardening drop-in, then a sshd START whose form depends on the guest's init system (systemd vs OpenRC) — three steps on systemd, two on OpenRC. Step (1) is unbranched and identical everywhere: a self-testing shell snippet that writes a `PerSourcePenalties no` sshd_config.d drop-in and validates the FULL resulting config with `sshd -t`, deleting the drop-in again on failure. It is hoisted above the branch in the source so "always first, distro-agnostic" is structural rather than duplicated per arm. The start then branches on `distroInit(spec.Source.Distro) == "openrc"` — a lookup into the generated `spec.DistroInits` table, never a compare against the literal `"alpine"`: on **systemd** (everything else) (2) `systemctl unmask ssh.socket || true` (the matching unmask for `composeBootCmd`'s mask) and (3) `systemctl enable --now sshd` (or `ssh` on debian/ubuntu); on **OpenRC** (Alpine) (2) `rc-update add sshd default && rc-service sshd start` — there is no ssh.socket to unmask and no systemctl to enable with, and the OpenSSH package ships `/etc/init.d/sshd`. Either way, distro-specific user `runcmd:` entries can assume sshd is running and hardened. `composeBootCmd`'s `systemctl mask ssh.socket || true` stays UNBRANCHED: on Alpine the `|| true` swallows the missing binary and the mask is vacuous where nothing is socket-activated.

User-supplied fields **extend** defaults; they don't replace them. Prevents the common footgun where an author puts `packages: [nginx]` and accidentally breaks SSH because they overrode the default list.

### Guest SSH hardening (D18) — the RCA'd kex-reset wedge class

The bootcmd-mask + runcmd hardening-drop-in + sshd-start sequence above (the start being unmask+enable on systemd, `rc-update`+`rc-service` on OpenRC) closes a
confirmed wedge: OpenSSH ≥ 9.8 defaults `PerSourcePenalties` ON, penalizing
repeated connection attempts from ONE source. Every VM guest is reached through
the SAME single passt gateway source IP, so `kit.WaitForSSH`'s own readiness
poll (`/charly-internals:vm-deploy-target`) can trip its own guest's rate limit
and appear to "reset forever" against an otherwise-healthy guest.

**Why a shell `runcmd` snippet, not a static `write_files` entry, for the sshd
drop-in.** `PerSourcePenalties` does not exist before OpenSSH 9.8. Writing it
as a static cloud-config `write_files` entry would hand an OLDER guest's sshd
a config with an unrecognized directive, which `sshd` refuses to start
against. The shell snippet writes the drop-in, then runs `sshd -t` (validating
the FULL resulting config) and deletes the drop-in again on failure — fail-safe
to the pre-fix behavior (the original penalty risk stands on an old guest),
never a bricked sshd.

**Design trade-off — deliberate, not overlooked.** Masking `ssh.socket` until
`runcmd` makes the guest **deterministically unreachable via SSH for the
entire package-install phase**, trading the old "sometimes-flaky-but-reachable"
window (sshd up early, racing a possible host-key rewrite) for
"safe-but-fully-blocked-if-package-install-stalls." **On a guest that ships
no `ssh.socket` — Arch cloud images among them — the mask is a `|| true`
no-op, so neither the blocked window NOR the guarantee that motivates it
applies there at all.** Sshd stays live on Arch throughout the package
install — which is not an oversight but the precondition for the
host-key race documented under D15 below (that race "needs only a live
sshd, not a socket-activated one"), and the reason the `--needed`
rewrite and the sshd try-restart guard exist. The trade-off in this
paragraph is scoped to socket-activated guests; on Arch there is no
window to block and nothing for the mask to trade. This
is verified live against real Arch cloud_image VM beds (`check-charly-vm`, and
`check-sidecar-pod`'s nested ephemeral VM member) — including a full
fresh-rebuild pass (destroy+recreate) for each — with zero wedge, now that the
companion redundant-package-reinstall fix (below) keeps the package-install
phase itself fast. If field evidence ever shows a package-install stall under
this ordering, the revisit path is either (a) moving the unmask earlier (e.g.
a `write_files`-stage cloud-init module instead of `runcmd`), or (b) dropping
the mask/unmask pair entirely and relying on the `PerSourcePenalties` drop-in
alone.

**Delivery is distro-branched (D15): `packages:` for most distros, a `runcmd:`-prepended `pacman -Sy --needed` pair for pacman-family.** On every distro EXCEPT the pacman family (arch/archarm/cachyos/manjaro/endeavouros), the composed package union rides the `packages:` cloud-config key as documented above. On a pacman-family distro, `packages:` is OMITTED entirely and the union is instead PREPENDED to `runcmd:` as TWO entries — `pacman -Sy --needed --noconfirm <union>`, then an sshd try-restart guard (`systemctl try-restart sshd.service ssh.socket sshd.socket`, silenced and `|| true`) that resyncs a live sshd if openssh rode along in the dep closure, since a running sshd would otherwise exec a NEWER sshd-session than the parent that spawned it ("internal error: hostkeys confused"). Both sit AHEAD of `composeRunCmd`'s own D18 steps (above), of which there are three on the pacman family since it is systemd throughout — distinct from the two entries this paragraph prepends. This is an R10 bed finding: cloud-init's own package-install module invokes `pacman -S` WITHOUT `--needed`, so on an image that already ships the minimum set (e.g. every Arch cloud image) it unconditionally REINSTALLS them — reinstalling openssh re-triggers its post-install host-key-regen hook while the base image's own sshd is already listening (on a socket-activated image; Arch cloud images typically ship no `ssh.socket`, which is why `composeBootCmd`'s mask is a `|| true` no-op there — the race needs only a live sshd, not a socket-activated one), racing a live key-file rewrite against new SSH connections (the observed "reset during `kex_exchange_identification`, guest otherwise idle" signature). apt/dnf installs are naturally no-op-idempotent when the package is already present, and apk's `add` is likewise a no-op for an already-present package, so only the pacman-family path needs the `--needed` rewrite. The `y` in `-Sy` is deliberate and its partial-upgrade hazard is ACCEPTED by design: a bare `-S` 404s against a rotated mirror, and the guest is a disposable, minutes-lived provisioning target where nothing long-lived survives on the pre-upgrade library set. The pacman-family check is `spec.DistroFormats[spec.Source.Distro] == "pac"` — a lookup into the generated format table keyed on the EXPLICIT field. Nothing is inferred from `base_user` or the image URL, and there is no fallback: an unset or out-of-vocabulary distro yields an empty format rather than the Arch/Fedora shape, which is why the vm kind's `OpValidate` requires the field at author time. See `sdk/vmshared/cloud_init_render.go` and `spec/schema/distro_vocab.cue`. **Note (D18):** the pacman-vs-non-pacman split governs ONLY the `packages:` key vs the `pacman -Sy` + sshd-resync runcmd prepend — every distro's `runcmd:` now ALSO carries the D18 hardening drop-in plus a per-init sshd start (unmask+enable on systemd, `rc-update`+`rc-service` on OpenRC), so this paragraph's scope is the packages-key handling alone, not the full runcmd list.

## WriteSeedISO

```go
func WriteSeedISO(userData, metaData, networkConfig string, outputPath string) error
```

Writes an ISO 9660 image whose volume identifier is `CIDATA` (the shared
`vmshared.cloudInitVolumeID`). It MUST be uppercase: ISO 9660 / ECMA 119
d-characters are `A-Z 0-9 _` only, so a lowercase label makes xorriso warn on
every VM boot. cloud-init still finds it — its NoCloud datasource searches both
`LABEL=<fs_label>.upper()` and `.lower()`, with `fs_label` defaulting to
`cidata`. Tool preference order:

1. `xorriso` (preferred — modern, scriptable).
2. `genisoimage` (legacy but widely available).
3. `mkisofs` (oldest fallback).

Clean error when none are present, with distro-appropriate install recipe (`dnf install xorriso`, `pacman -S libisoburn`, `apt-get install xorriso`).

The ISO is mounted by QEMU as a CD-ROM; cloud-init's NoCloud datasource reads `/dev/sr0` at first boot.

## EnsureCharlyInGuest (charly_install.strategy state machine)

Runs post-boot inside the vm deploy preflight (the `candy/plugin-deploy-vm` plugin's `OpPrepareVenue`, via `kit.EnsureCharlyInGuest` in `spec/exec/charly_install.go`) after cloud-init completes, BEFORE the plugin walks the plans. Dispatches on `spec.CloudInit.CharlyInstall.Strategy`:

| Strategy | Action |
|---|---|
| `auto` / `scp` | `scp $(os.Executable()) guest:/usr/local/bin/charly; chmod +x` |
| `skip` | `ssh 'which charly'` — fails if missing, returns early if present |

Idempotent. If `charly` is already present at the target version, the function returns without re-scp'ing. See `/charly-internals:vm-deploy-target` for how this plugs into the overall deploy flow.

## Network config

`VmCloudInitNetwork.Ethernets` passes through to cloud-init's network-config v2 as-is. When unset, the renderer emits an empty network-config (cloud-init defaults to DHCP on every virtio-net interface). Good default; override only for static-IP deployments.

## SMBIOS vs cloud_init can coexist

Explicitly supported — not either/or. `VmKeyInjection.SMBIOS: enabled` + `VmKeyInjection.CloudInit: enabled` simultaneously is the default for cloud_image VMs. Rationale: belt-and-suspenders (SMBIOS via systemd-ssh-generator v250+, cloud-init via user-data — both paths exist on modern Linux). No duplication cost.

## Cross-References

- `/charly-internals:vm-spec` — `VmCloudInit`, `VmSsh.KeyInjection`, `VmCharlyInstall` types
- `/charly-internals:libvirt-renderer` — SMBIOS-channel emission (domain XML side)
- `/charly-internals:vm-deploy-target` — `EnsureCharlyInGuest` caller; SSH/cloud-init readiness waits
- `/charly-vm:vm` — command-family; cloud-init flow
- `/charly-vm:vms-catalog` — YAML-authoring reference
- `/charly-vm:arch-cloud-vm` — `charly_install.strategy: auto` worked example; adopt-user pattern
- `/charly-distros:cloud-init` — **guest-side pairing**: the cloud-init package installed inside bootc images reads the seed ISO this renderer produces
