package vmshared

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// CloudInitRuntimeParams carries the runtime-resolved state needed to
// render cloud-init user-data: the SSH public key to inject, the
// instance-id (stable UUIDv4 persisted in VmDeployState), the hostname,
// and whether cloud-init should inject the SSH key at all (computed
// from D13 auto-defaults + explicit VmKeyInjection overrides).
type CloudInitRuntimeParams struct {
	// SSHPublicKey is the OpenSSH authorized_keys-format public key
	// line (e.g. "ssh-ed25519 AAAA..."). Empty when key injection is
	// disabled or when VmSSH.KeySource == "none".
	SSHPublicKey string

	// InstanceID is the stable UUIDv4 cloud-init instance-id.
	// Pinned at first VM create and persisted in VmDeployState.
	InstanceID string

	// Hostname for the guest. Defaults to the VM name when empty.
	Hostname string

	// InjectKeyViaCloudInit is the resolved D13 key_injection.cloud_init
	// channel state. When false the renderer emits no
	// ssh_authorized_keys entries even if SSHPublicKey is populated.
	InjectKeyViaCloudInit bool
}

// RenderCloudInit produces the three NoCloud seed ISO payloads from a
// VmSpec plus runtime parameters. Pure function — no filesystem or
// network calls.
//
// - userData   → written to cidata/user-data (prefixed with #cloud-config)
// - metaData   → written to cidata/meta-data (instance-id + hostname)
// - networkCfg → written to cidata/network-config (optional; empty if unset)
//
// Defaults applied automatically (D15):
//  1. VmSSH.User added to users: with sudo + ssh_authorized_keys
//     (if the key-injection channel is enabled AND SSHPublicKey != "")
//  2. Minimum packages: {openssh, curl, tar} unioned with user's Packages —
//     delivered via the `packages:` cloud-config key on every distro EXCEPT
//     pacman-family (arch/cachyos/manjaro/endeavouros), where it is instead
//     PREPENDED to runcmd as `pacman -S --needed --noconfirm <union>` and the
//     `packages:` key is omitted entirely (R10 bed finding: cloud-init's own
//     package-install module invokes `pacman -S` without `--needed`, so on an
//     image that already ships the minimum set — e.g. every Arch cloud image —
//     it unconditionally REINSTALLS them; reinstalling openssh re-triggers its
//     post-install host-key-regen hook while the base image's own
//     socket-activated sshd is already listening, racing a live key-file
//     rewrite against new connections — the observed "reset during
//     kex_exchange_identification, guest otherwise idle" signature. apt/dnf
//     installs are naturally no-op-idempotent when the package is already
//     present, so only the pacman-family path needs this rewrite; every other
//     distro's render is BYTE-IDENTICAL to before this fix). Source.Distro is
//     an OPTIONAL yaml field — a cloud_image source (e.g. eval-vm) commonly
//     omits it, relying on base_user alone (a second live-bed finding: the
//     first cut of this fix silently never fired for exactly that reason).
//     effectiveDistro fills the ONE narrow gap this codebase already
//     documents as supported-by-convention (resolveCloudInitSSHUser's own
//     "arch" cloud_image fallback, below): empty Distro + kind=="cloud_image"
//     + base_user=="arch" infers "arch". This is STRICTLY NARROWER than (and
//     makes explicit) the pre-existing accidental behavior — composePackages'
//     distro switch already defaulted an empty/unrecognized distro to the
//     Arch/Fedora-shaped {openssh, sshd} output, so no caller this inference
//     newly matches was ever getting anything else. Any other empty-distro
//     image (base_user != "arch") stays on the safe, unchanged non-pacman
//     path — an unknown image never gets a pacman command. The proper
//     long-term fix is schema-level (distro required, or defaulted at
//     entity-resolve time, for every cloud_image source) — tracked separately,
//     not attempted here.
//  3. Minimum runcmd: {systemctl enable --now sshd} prepended (after the
//     pacman install command on pacman-family distros, per #2)
//  4. charly_install: NOT a cloud-init concern — the vm deploy's PrepareVenue delivers charly
//     post-boot (auto/scp stage it; skip verifies). No charly download runcmd.
//  5. VmCloudInit.Extra: raw cloud-config YAML appended as a second
//     document (separated by ---) if non-empty
func RenderCloudInit(spec *VmSpec, rt CloudInitRuntimeParams) (userData, metaData, networkConfig string, err error) {
	ci := spec.CloudInit
	if ci == nil {
		ci = &VmCloudInit{}
	}

	// --- meta-data ---

	hostname := rt.Hostname
	if ci.Hostname != "" {
		hostname = ci.Hostname
	}
	metaMap := map[string]any{}
	if rt.InstanceID != "" {
		metaMap["instance-id"] = rt.InstanceID
	}
	if hostname != "" {
		metaMap["local-hostname"] = hostname
	}
	metaBytes, err := yaml.Marshal(metaMap)
	if err != nil {
		return "", "", "", fmt.Errorf("render meta-data: %w", err)
	}
	metaData = string(metaBytes)
	if err := ValidateEgress("cloud_init_meta", "cloud-init meta-data", metaBytes); err != nil {
		return "", "", "", err
	}

	// --- network-config ---

	if ci.Network != nil && len(ci.Network.Ethernets) > 0 {
		netMap := map[string]any{
			"version": 2,
		}
		if ci.Network.Version > 0 {
			netMap["version"] = ci.Network.Version
		}
		netMap["ethernets"] = ci.Network.Ethernets
		netBytes, err := yaml.Marshal(netMap)
		if err != nil {
			return "", "", "", fmt.Errorf("render network-config: %w", err)
		}
		networkConfig = string(netBytes)
		if err := ValidateEgress("cloud_init_net", "cloud-init network-config", netBytes); err != nil {
			return "", "", "", err
		}
	}

	// --- user-data ---

	userMap := map[string]any{}

	if hostname != "" {
		userMap["hostname"] = hostname
	}
	if ci.Timezone != "" {
		userMap["timezone"] = ci.Timezone
	}
	if ci.Locale != "" {
		userMap["locale"] = ci.Locale
	}

	userMap["users"] = composeUsers(spec, ci, rt)

	distro := effectiveDistro(spec)
	packages := composePackages(ci.Package, distro)
	pacmanFamily := formatForDistroID(distro) == "pac"
	if len(packages) > 0 && !pacmanFamily {
		userMap["packages"] = packages
	}

	bootcmd := composeBootCmd(ci)
	if len(bootcmd) > 0 {
		userMap["bootcmd"] = bootcmd
	}

	runcmd := composeRunCmd(spec, ci)
	if pacmanFamily && len(packages) > 0 {
		pacmanCmd := "pacman -S --needed --noconfirm " + strings.Join(packages, " ")
		runcmd = append([]any{pacmanCmd}, runcmd...)
	}
	if len(runcmd) > 0 {
		userMap["runcmd"] = runcmd
	}

	writeFiles := composeWriteFiles(ci.WriteFiles)
	if len(writeFiles) > 0 {
		userMap["write_files"] = writeFiles
	}

	if ci.Mirrors != nil && len(ci.Mirrors.APT) > 0 {
		// cloud-init apt config (Ubuntu/Debian guests).
		userMap["apt"] = map[string]any{
			"preserve_sources_list": false,
			"primary": []map[string]any{
				{"arches": []string{"default"}, "uri": ci.Mirrors.APT[0]},
			},
		}
	}

	userBytes, err := yaml.Marshal(userMap)
	if err != nil {
		return "", "", "", fmt.Errorf("render user-data: %w", err)
	}
	// Egress gate: the rendered cloud-config (the structured user-data document,
	// before the #cloud-config header + any raw Extra passthrough) must validate
	// against Canonical's vendored schema before it reaches the seed ISO.
	if err := ValidateEgress("cloud_config", "cloud-init user-data", userBytes); err != nil {
		return "", "", "", err
	}

	var b strings.Builder
	b.WriteString("#cloud-config\n")
	b.Write(userBytes)

	if ci.Extra != "" {
		b.WriteString("---\n")
		extra := strings.TrimPrefix(ci.Extra, "#cloud-config\n")
		b.WriteString(extra)
		if !strings.HasSuffix(extra, "\n") {
			b.WriteString("\n")
		}
	}

	userData = b.String()
	return userData, metaData, networkConfig, nil
}

// composeUsers builds the cloud-init users: list. Mirrors the
// container-side user_policy pattern — adopt the upstream's
// pre-existing account when spec.Source.BaseUser is set (emit
// merge-by-name entry with only ssh_authorized_keys), otherwise
// create a new user with sudo+groups+shell.
//
// The rendered list ALWAYS starts with `- default` so cloud-init
// preserves the distro's default-user semantics (including its
// existing sudoers membership). Merge-by-name semantics mean no user
// is recreated: if an entry's Name matches an already-existing
// account, cloud-init just appends ssh_authorized_keys.
func composeUsers(spec *VmSpec, ci *VmCloudInit, rt CloudInitRuntimeParams) []any {
	sshUser := resolveCloudInitSSHUser(spec)
	baseUser := ""
	if spec != nil {
		baseUser = spec.Source.BaseUser
	}

	// Start with the distro default so cloud-init doesn't disable it.
	out := []any{"default"}

	// User-declared custom users are emitted as-is.
	mergedSSH := false
	for _, u := range ci.Users {
		entry := userEntryToMap(u)
		if u.Name == sshUser {
			applySSHDefaults(entry, rt)
			mergedSSH = true
		}
		out = append(out, entry)
	}

	if mergedSSH || sshUser == "" {
		return out
	}

	// Adopt path: the sshUser matches the upstream's pre-existing
	// account (BaseUser). Emit a minimal merge-by-name entry that only
	// appends ssh_authorized_keys. DO NOT set sudo/groups/shell —
	// trust upstream's sudoers config (Arch's arch user is in wheel
	// with NOPASSWD via /etc/sudoers.d/10-default, Ubuntu's ubuntu
	// user has equivalent, etc.).
	if baseUser != "" && sshUser == baseUser {
		entry := map[string]any{"name": sshUser}
		applySSHDefaults(entry, rt)
		out = append(out, entry)
		return out
	}

	// Create path: the sshUser is a NEW user the upstream didn't
	// ship. Emit full create-user entry with sudo, wheel membership,
	// and bash shell.
	entry := map[string]any{
		"name":   sshUser,
		"sudo":   "ALL=(ALL) NOPASSWD:ALL",
		"groups": []string{"wheel"},
		"shell":  "/bin/bash",
	}
	applySSHDefaults(entry, rt)
	out = append(out, entry)
	return out
}

// resolveCloudInitSSHUser picks the ssh user for cloud-init rendering.
// Precedence:
//  1. Explicit spec.ssh.user
//  2. spec.source.base_user (adopt path — cloud_image sources with a
//     declared upstream account)
//  3. Source-kind fallback ("root" for bootc, "arch" for cloud_image —
//     the latter works for Arch cloud images out of the box; for
//     other distros users MUST set base_user or ssh.user explicitly)
//
// RCA #13 (FINAL/K5 unit 6a): a check command must never SIGSEGV on any
// input — nil spec (an unresolved vm entity, e.g. an upstream entity-
// resolution bug) now returns "" instead of dereferencing spec.SSH. Exported
// via the ResolveCloudInitSSHUser var-alias (exports.go) consumed by three
// charly call sites (bundle_add_cmd.go, check_cmd.go, vm_lifecycle_preresolve.go)
// — one guard covers all three (R3).
func resolveCloudInitSSHUser(spec *VmSpec) string {
	if spec == nil {
		return ""
	}
	if spec.SSH != nil && spec.SSH.User != "" {
		return spec.SSH.User
	}
	if spec.Source.BaseUser != "" {
		return spec.Source.BaseUser
	}
	if spec.Source.Kind == "bootc" {
		return "root"
	}
	return ""
}

func userEntryToMap(u VmCloudInitUser) map[string]any {
	m := map[string]any{"name": u.Name}
	if u.Sudo {
		m["sudo"] = "ALL=(ALL) NOPASSWD:ALL"
	}
	if len(u.Groups) > 0 {
		m["groups"] = u.Groups
	}
	if u.Shell != "" {
		m["shell"] = u.Shell
	}
	if u.LockPasswd != nil {
		m["lock_passwd"] = *u.LockPasswd
	}
	return m
}

// applySSHDefaults adds ssh_authorized_keys to a user entry when
// key injection via cloud-init is enabled and a pubkey exists.
func applySSHDefaults(entry map[string]any, rt CloudInitRuntimeParams) {
	if rt.InjectKeyViaCloudInit && rt.SSHPublicKey != "" {
		existing, _ := entry["ssh_authorized_keys"].([]string)
		entry["ssh_authorized_keys"] = append(existing, rt.SSHPublicKey)
	}
}

// effectiveDistro resolves spec.Source.Distro, inferring "arch" for the ONE
// case this codebase already documents as supported-by-convention
// (resolveCloudInitSSHUser's own comment: "arch" for cloud_image works out
// of the box) but that the CUE schema leaves Distro optional for: a
// cloud_image source with no explicit `distro:` and `base_user: arch` (e.g.
// a plain Arch Linux cloud image entity — see the D15 doc comment above for
// why this matters to the pacman-family package-delivery branch). Strictly
// narrower than the pre-existing accidental behavior: composePackages'
// distro switch already defaulted an empty/unrecognized distro to the
// Arch/Fedora {openssh, sshd} shape, so this inference can never change what
// a caller already effectively got — it only makes the SAME assumption
// explicit enough for formatForDistroID to positively match "pac". Any
// other empty-distro image (a different or absent base_user) returns "" and
// stays on the safe, unchanged non-pacman path.
func effectiveDistro(spec *VmSpec) string {
	if spec == nil {
		return ""
	}
	if spec.Source.Distro != "" {
		return spec.Source.Distro
	}
	if spec.Source.Kind == "cloud_image" && spec.Source.BaseUser == "arch" {
		return "arch"
	}
	return ""
}

// composePackages unions charly's minimum SSH+curl+tar package set with
// the user's declared packages, preserving the user's order for extras.
//
// The minimum SSH package name is distro-aware: `openssh` on
// Arch/Fedora, `openssh-server` on Debian/Ubuntu. Without this, cloud-
// init's package-install module hard-fails on Debian (`E: Unable to
// locate package openssh`), which then fails cloud-init-network →
// cloud-init.target → qemu-guest-agent stays stuck waiting forever.
func composePackages(userPkgs []string, distro string) []string {
	sshPkg := "openssh"
	switch distro {
	case "debian", "ubuntu":
		sshPkg = "openssh-server"
	}
	minimum := []string{sshPkg, "curl", "tar"}
	seen := map[string]bool{}
	var out []string
	for _, p := range minimum {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, p := range userPkgs {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// sshUnitForDistro resolves the sshd systemd unit name: `sshd` on Arch/Fedora, `ssh` on
// Debian/Ubuntu (where the systemd unit is named `ssh.service` — `sshd.service` is just a
// symlink that systemd refuses to enable).
func sshUnitForDistro(distro string) string {
	switch distro {
	case "debian", "ubuntu":
		return "ssh"
	default:
		return "sshd"
	}
}

// sshHardeningDropInPath is the sshd_config.d drop-in charly writes on every cloud_image VM (D18,
// bed-robustness batch item 3). Named to sort last among any operator-authored drop-ins.
const sshHardeningDropInPath = "/etc/ssh/sshd_config.d/99-charly-guest-hardening.conf"

// composeBootCmd prepends charly's own EARLY boot step — masking ssh.socket (D18, bed-robustness
// batch item 3) — ahead of the user's declared bootcmd. bootcmd runs on EVERY boot, before
// write_files/packages/runcmd, so this is the earliest point at which a socket-activated sshd
// (ssh.socket — enabled BY DEFAULT on some cloud images, notably Debian/Ubuntu) can be prevented
// from accepting connections before cloud-init has finished configuring the guest (host-key
// regen, user creation, the sshd hardening drop-in below). `|| true`: masking a unit name the
// image doesn't ship (Arch/Fedora typically have no ssh.socket at all) is a harmless no-op, never
// a boot failure. runcmd (composeRunCmd, below) is the matching unmask + enable step, run only
// after cloud-init's OWN work is complete — an orderly key-regen-before-accept sequence instead
// of "sshd (or its socket) is reachable the instant the kernel finishes booting."
func composeBootCmd(ci *VmCloudInit) []any {
	bootcmd := make([]any, 0, 1+len(ci.BootCmd))
	bootcmd = append(bootcmd, "systemctl mask ssh.socket || true")
	for _, cmd := range ci.BootCmd {
		bootcmd = append(bootcmd, cmd)
	}
	return bootcmd
}

// sshHardeningDropInCmd is the self-testing shell snippet that writes sshHardeningDropInPath and
// validates the FULL resulting sshd config before leaving it in place (D18, bed-robustness batch
// item 3). OpenSSH ≥ 9.8 defaults `PerSourcePenalties` ON, which penalizes repeated connection
// attempts from ONE source — exactly what kit.WaitForSSH's readiness poll does, and every VM's
// guest is reached through the SAME single passt gateway source IP, so the poll can trip its own
// guest's rate-limit and appear to "reset forever" against an otherwise-healthy guest (the RCA'd
// kex-reset wedge class this closes). Written as a shell runcmd — NOT a static cloud-init
// write_files entry — because `PerSourcePenalties` does not exist before OpenSSH 9.8: `sshd -t`
// validates the config AFTER the write, and the drop-in is deleted again on failure, so an OLDER
// guest OpenSSH that rejects the unknown directive is NEVER left with a config sshd refuses to
// start against — fail-safe to the pre-fix behavior (the original penalty risk stands on an old
// guest), never a bricked sshd. Runs in runcmd (after packages are installed, so the `sshd`
// binary this depends on exists) and BEFORE the unmask+enable step so sshd's first-ever start
// already reads a validated config.
const sshHardeningDropInCmd = `sh -c 'echo "PerSourcePenalties no" > ` + sshHardeningDropInPath + ` && sshd -t || rm -f ` + sshHardeningDropInPath + `'`

// composeRunCmd prepends charly's minimum boot tasks and appends the user's runcmd. charly is NOT
// installed via cloud-init — the vm deploy's PrepareVenue delivers it post-boot per
// charly_install.strategy (auto/scp stage the host binary; skip verifies presence).
//
// D18 (bed-robustness batch item 3, the RCA'd kex-reset wedge class): the sshd hardening drop-in
// is written+validated FIRST, then sshd (or its socket, masked by composeBootCmd above) is
// unmasked and enabled — the LAST charly runcmd step before the user's own — so it starts only
// after cloud-init's package/user/write_files work AND the drop-in write have completed: an
// orderly key-regen-before-accept sequence, closing the window where a socket-activated sshd
// could accept a connection against a guest cloud-init hasn't finished configuring yet.
func composeRunCmd(spec *VmSpec, ci *VmCloudInit) []any {
	runcmd := make([]any, 0, 3+len(ci.RunCmd))
	sshUnit := sshUnitForDistro(spec.Source.Distro)
	runcmd = append(runcmd,
		sshHardeningDropInCmd,
		"systemctl unmask ssh.socket || true",
		fmt.Sprintf("systemctl enable --now %s", sshUnit),
	)

	for _, cmd := range ci.RunCmd {
		runcmd = append(runcmd, cmd)
	}

	return runcmd
}

// composeWriteFiles turns VmCloudInitFile entries into the
// map-list shape cloud-init expects.
func composeWriteFiles(files []VmCloudInitFile) []map[string]any {
	out := make([]map[string]any, 0, len(files))
	for _, f := range files {
		m := map[string]any{
			"path":    f.Path,
			"content": f.Content,
		}
		if f.Owner != "" {
			m["owner"] = f.Owner
		}
		if f.Perms != "" {
			m["permissions"] = f.Perms
		}
		if f.Encoding != "" {
			m["encoding"] = f.Encoding
		}
		out = append(out, m)
	}
	return out
}

// ResolveKeyInjectionChannels applies the D13 auto-defaults and
// explicit-wins merging to produce the effective (smbios, cloudInit)
// toggle state for a VmSpec. Returns the booleans persisted into
// VmDeployState.KeyInjectionResolved.
func ResolveKeyInjectionChannels(spec *VmSpec) (smbios, cloudInit bool) {
	// Defaults per D13:
	//   cloud_image → {smbios: true, cloud_init: true}  (additive)
	//   bootc       → {smbios: true, cloud_init: false}
	if spec.Source.Kind == "bootc" {
		smbios, cloudInit = true, false
	} else {
		smbios, cloudInit = true, true
	}

	if spec.SSH == nil || spec.SSH.KeyInjection == nil {
		return smbios, cloudInit
	}
	kj := spec.SSH.KeyInjection
	switch kj.SMBIOS {
	case "enabled":
		smbios = true
	case "disabled":
		smbios = false
	}
	switch kj.CloudInit {
	case "enabled":
		cloudInit = true
	case "disabled":
		cloudInit = false
	}
	return smbios, cloudInit
}
