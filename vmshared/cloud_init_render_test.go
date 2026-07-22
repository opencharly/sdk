package vmshared

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Golden/behavior coverage for the cloud-init renderer (cloud_init_render.go).
// These lock the CURRENT behavior of RenderCloudInit + its composable units
// (composeUsers adopt-vs-create, composePackages/composeRunCmd distro-awareness,
// ResolveKeyInjectionChannels D13 defaults) so any drift fails here rather than
// only surfacing when a VM boots. The renderer is a pure function — no bed needed.

const testPubKey = "ssh-ed25519 AAAATESTKEY user@host"

// findUserEntry returns the map entry named `name` from a composeUsers result
// (skipping the leading "default" string), or nil.
func findUserEntry(users []any, name string) map[string]any {
	for _, u := range users {
		if m, ok := u.(map[string]any); ok && m["name"] == name {
			return m
		}
	}
	return nil
}

func keyInject(pubkey string) CloudInitRuntimeParams {
	return CloudInitRuntimeParams{SSHPublicKey: pubkey, InjectKeyViaCloudInit: true}
}

// --- composeUsers: adopt vs create vs declared-merge ---

func TestComposeUsers_AdoptBaseUser(t *testing.T) {
	spec := &VmSpec{Source: VmSource{Kind: "cloud_image", Distro: "arch", BaseUser: "arch"}}
	users := composeUsers(spec, &VmCloudInit{}, keyInject(testPubKey))

	if len(users) == 0 || users[0] != "default" {
		t.Fatalf("users must start with \"default\", got %v", users)
	}
	arch := findUserEntry(users, "arch")
	if arch == nil {
		t.Fatalf("adopted user \"arch\" missing: %v", users)
	}
	// Adopt path: ONLY name + ssh_authorized_keys — never sudo/groups/shell.
	if _, hasSudo := arch["sudo"]; hasSudo {
		t.Errorf("adopt entry must not set sudo: %v", arch)
	}
	if _, hasGroups := arch["groups"]; hasGroups {
		t.Errorf("adopt entry must not set groups: %v", arch)
	}
	keys, _ := arch["ssh_authorized_keys"].([]string)
	if len(keys) != 1 || keys[0] != testPubKey {
		t.Errorf("adopt entry ssh_authorized_keys = %v, want [%q]", arch["ssh_authorized_keys"], testPubKey)
	}
}

func TestComposeUsers_CreateNewUser(t *testing.T) {
	// ssh.user set, no matching base_user → full create entry.
	spec := &VmSpec{Source: VmSource{Kind: "cloud_image", Distro: "arch"}, SSH: &VmSSH{User: "charly"}}
	users := composeUsers(spec, &VmCloudInit{}, keyInject(testPubKey))

	u := findUserEntry(users, "charly")
	if u == nil {
		t.Fatalf("created user \"charly\" missing: %v", users)
	}
	if u["sudo"] != "ALL=(ALL) NOPASSWD:ALL" {
		t.Errorf("create entry sudo = %v", u["sudo"])
	}
	if u["shell"] != "/bin/bash" {
		t.Errorf("create entry shell = %v", u["shell"])
	}
	groups, _ := u["groups"].([]string)
	if len(groups) != 1 || groups[0] != "wheel" {
		t.Errorf("create entry groups = %v, want [wheel]", u["groups"])
	}
	if keys, _ := u["ssh_authorized_keys"].([]string); len(keys) != 1 {
		t.Errorf("create entry missing ssh_authorized_keys: %v", u)
	}
}

func TestComposeUsers_DeclaredUserMergedNotDuplicated(t *testing.T) {
	// A user-declared entry whose name == sshUser gets the pubkey appended
	// (merged) rather than a second entry being created.
	spec := &VmSpec{Source: VmSource{Kind: "cloud_image", BaseUser: "arch"}}
	ci := &VmCloudInit{Users: []VmCloudInitUser{{Name: "arch", Groups: []string{"docker"}}}}
	users := composeUsers(spec, ci, keyInject(testPubKey))

	count := 0
	for _, u := range users {
		if m, ok := u.(map[string]any); ok && m["name"] == "arch" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one \"arch\" entry (merged), got %d: %v", count, users)
	}
	arch := findUserEntry(users, "arch")
	if keys, _ := arch["ssh_authorized_keys"].([]string); len(keys) != 1 {
		t.Errorf("declared user not merged with pubkey: %v", arch)
	}
	if groups, _ := arch["groups"].([]string); len(groups) != 1 || groups[0] != "docker" {
		t.Errorf("declared user's own fields lost: %v", arch)
	}
}

func TestComposeUsers_KeyGatingOff(t *testing.T) {
	spec := &VmSpec{Source: VmSource{Kind: "cloud_image", BaseUser: "arch"}}
	// InjectKeyViaCloudInit defaults false → no keys even with a pubkey present.
	rt := CloudInitRuntimeParams{SSHPublicKey: testPubKey, InjectKeyViaCloudInit: false}
	users := composeUsers(spec, &VmCloudInit{}, rt)
	if arch := findUserEntry(users, "arch"); arch != nil {
		if _, has := arch["ssh_authorized_keys"]; has {
			t.Errorf("key injection OFF must emit no ssh_authorized_keys: %v", arch)
		}
	}
}

// --- composePackages: distro-aware ssh pkg + dedup, user order preserved ---

func TestComposePackages(t *testing.T) {
	cases := []struct {
		name   string
		distro string
		user   []string
		want   []string
	}{
		{"arch minimum", "arch", nil, []string{"openssh", "curl", "tar"}},
		{"fedora minimum", "fedora", nil, []string{"openssh", "curl", "tar"}},
		{"debian server pkg", "debian", nil, []string{"openssh-server", "curl", "tar"}},
		{"ubuntu server pkg", "ubuntu", nil, []string{"openssh-server", "curl", "tar"}},
		{"user extras appended", "arch", []string{"htop", "neovim"}, []string{"openssh", "curl", "tar", "htop", "neovim"}},
		{"user dup deduped", "arch", []string{"curl", "htop"}, []string{"openssh", "curl", "tar", "htop"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := composePackages(c.user, c.distro)
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Errorf("composePackages(%v, %q) = %v, want %v", c.user, c.distro, got, c.want)
			}
		})
	}
}

// --- composeRunCmd: distro-aware sshd unit + user runcmd appended ---

func TestComposeRunCmd(t *testing.T) {
	cases := []struct {
		distro   string
		wantUnit string
	}{
		{"arch", "sshd"},
		{"fedora", "sshd"},
		{"debian", "ssh"},
		{"ubuntu", "ssh"},
	}
	for _, c := range cases {
		t.Run(c.distro, func(t *testing.T) {
			spec := &VmSpec{Source: VmSource{Kind: "cloud_image", Distro: c.distro}}
			got := composeRunCmd(spec, &VmCloudInit{RunCmd: []string{"echo hi"}})
			// D18 (bed-robustness batch item 3): the sshd hardening drop-in write+validate is
			// now step 0, "systemctl unmask ssh.socket || true" is step 1 (the matching unmask
			// for composeBootCmd's mask), the enable step moved to index 2, and the user's own
			// runcmd entries still land last, unchanged relative order.
			wantEnable := "systemctl enable --now " + c.wantUnit
			if len(got) != 4 {
				t.Fatalf("composeRunCmd(%q) = %v, want 4 entries", c.distro, got)
			}
			if got[0] != sshHardeningDropInCmd {
				t.Errorf("composeRunCmd(%q)[0] = %v, want the sshd hardening drop-in command", c.distro, got[0])
			}
			if got[1] != "systemctl unmask ssh.socket || true" {
				t.Errorf("composeRunCmd(%q)[1] = %v, want the ssh.socket unmask", c.distro, got[1])
			}
			if got[2] != wantEnable {
				t.Errorf("composeRunCmd(%q)[2] = %v, want %q", c.distro, got[2], wantEnable)
			}
			if got[3] != "echo hi" {
				t.Errorf("composeRunCmd(%q)[3] = %v, want the user's own runcmd entry", c.distro, got[3])
			}
		})
	}
}

// TestComposeBootCmd_MasksSshSocket is the regression test for the D18 mask half of the fix: the
// mask must be the FIRST bootcmd entry (bootcmd runs earliest, before write_files/packages/
// runcmd), and the user's own bootcmd entries must still be preserved after it.
func TestComposeBootCmd_MasksSshSocket(t *testing.T) {
	got := composeBootCmd(&VmCloudInit{BootCmd: []string{"echo early"}})
	if len(got) != 2 {
		t.Fatalf("composeBootCmd() = %v, want 2 entries", got)
	}
	if got[0] != "systemctl mask ssh.socket || true" {
		t.Errorf("composeBootCmd()[0] = %v, want the ssh.socket mask", got[0])
	}
	if got[1] != "echo early" {
		t.Errorf("composeBootCmd()[1] = %v, want the user's own bootcmd entry", got[1])
	}
}

// --- ResolveKeyInjectionChannels: D13 defaults + explicit overrides ---

func TestResolveKeyInjectionChannels(t *testing.T) {
	enabled, disabled := "enabled", "disabled"
	cases := []struct {
		name              string
		spec              *VmSpec
		wantSM, wantCloud bool
	}{
		{"cloud_image defaults", &VmSpec{Source: VmSource{Kind: "cloud_image"}}, true, true},
		{"bootc defaults", &VmSpec{Source: VmSource{Kind: "bootc"}}, true, false},
		{"cloud_image disable cloud_init", &VmSpec{Source: VmSource{Kind: "cloud_image"}, SSH: &VmSSH{KeyInjection: &VmKeyInjection{CloudInit: disabled}}}, true, false},
		{"bootc enable cloud_init", &VmSpec{Source: VmSource{Kind: "bootc"}, SSH: &VmSSH{KeyInjection: &VmKeyInjection{CloudInit: enabled}}}, true, true},
		{"disable smbios", &VmSpec{Source: VmSource{Kind: "cloud_image"}, SSH: &VmSSH{KeyInjection: &VmKeyInjection{SMBIOS: disabled}}}, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sm, cloud := ResolveKeyInjectionChannels(c.spec)
			if sm != c.wantSM || cloud != c.wantCloud {
				t.Errorf("ResolveKeyInjectionChannels = (%v,%v), want (%v,%v)", sm, cloud, c.wantSM, c.wantCloud)
			}
		})
	}
}

func TestResolveCloudInitSSHUser(t *testing.T) {
	cases := []struct {
		name string
		spec *VmSpec
		want string
	}{
		{"explicit ssh.user wins", &VmSpec{Source: VmSource{Kind: "cloud_image", BaseUser: "arch"}, SSH: &VmSSH{User: "charly"}}, "charly"},
		{"base_user adopt", &VmSpec{Source: VmSource{Kind: "cloud_image", BaseUser: "ubuntu"}}, "ubuntu"},
		{"bootc fallback root", &VmSpec{Source: VmSource{Kind: "bootc"}}, "root"},
		{"cloud_image no base → empty", &VmSpec{Source: VmSource{Kind: "cloud_image"}}, ""},
		// RCA #13 (FINAL/K5 unit 6a): a check command must never SIGSEGV on any
		// input — an unresolved vm entity (e.g. an upstream entity-resolution bug)
		// must return "", not dereference spec.SSH.
		{"nil spec → empty, no panic", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveCloudInitSSHUser(c.spec); got != c.want {
				t.Errorf("resolveCloudInitSSHUser = %q, want %q", got, c.want)
			}
		})
	}
}

// --- RenderCloudInit end-to-end: meta-data, the #cloud-config envelope, Extra ---

// parseUserData strips the #cloud-config prefix + returns the FIRST document.
func parseUserData(t *testing.T, userData string) map[string]any {
	t.Helper()
	body := strings.TrimPrefix(userData, "#cloud-config\n")
	if i := strings.Index(body, "\n---\n"); i >= 0 {
		body = body[:i]
	}
	var m map[string]any
	if err := yaml.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("user-data is not valid yaml: %v\n%s", err, userData)
	}
	return m
}

func TestRenderCloudInit_EnvelopeAndMeta(t *testing.T) {
	// fedora: NON-pacman (rpm), so this exercises the unchanged packages:-key path —
	// see TestRenderCloudInit_PacmanFamily_RunsPacmanNeeded for the arch/pacman shape.
	spec := &VmSpec{Source: VmSource{Kind: "cloud_image", Distro: "fedora", BaseUser: "fedora"}}
	rt := CloudInitRuntimeParams{SSHPublicKey: testPubKey, InjectKeyViaCloudInit: true, InstanceID: "iid-123", Hostname: "testvm"}
	userData, metaData, _, err := RenderCloudInit(spec, rt)
	if err != nil {
		t.Fatalf("RenderCloudInit: %v", err)
	}
	if !strings.HasPrefix(userData, "#cloud-config\n") {
		t.Errorf("user-data must start with #cloud-config:\n%s", userData)
	}
	if !strings.Contains(metaData, "instance-id: iid-123") || !strings.Contains(metaData, "local-hostname: testvm") {
		t.Errorf("meta-data missing instance-id/local-hostname:\n%s", metaData)
	}
	um := parseUserData(t, userData)
	if um["hostname"] != "testvm" {
		t.Errorf("user-data hostname = %v, want testvm", um["hostname"])
	}
	pkgs, _ := um["packages"].([]any)
	if len(pkgs) == 0 || pkgs[0] != "openssh" {
		t.Errorf("user-data packages = %v, want openssh first", um["packages"])
	}
	rc, _ := um["runcmd"].([]any)
	// D18 (bed-robustness batch item 3): "systemctl enable --now sshd" is no longer runcmd[0] —
	// the sshd hardening drop-in write+validate and the ssh.socket unmask now precede it.
	if len(rc) < 3 || rc[2] != "systemctl enable --now sshd" {
		t.Errorf("user-data runcmd = %v, want index 2 to be the sshd enable step", um["runcmd"])
	}
	bc, _ := um["bootcmd"].([]any)
	if len(bc) == 0 || bc[0] != "systemctl mask ssh.socket || true" {
		t.Errorf("user-data bootcmd = %v, want the ssh.socket mask first", um["bootcmd"])
	}
}

// --- R10 bed finding: pacman-family package delivery moves to an idempotent
// runcmd, every other distro's render is untouched. ---
//
// Root cause (check-sidecar-pod-ephvm wedge, S3b): cloud-init's own Arch
// package-install module invokes `pacman -S` WITHOUT `--needed`, so on an
// image that already ships the D15 minimum set (every Arch cloud image does),
// it unconditionally reinstalls them — reinstalling openssh re-triggers its
// post-install host-key-regen hook while the base image's own
// socket-activated sshd is already listening, racing new connections against
// a live key-file rewrite (observed: TCP accepts, resets during
// kex_exchange_identification, guest otherwise idle — a live console capture
// of the wedge is in scratchpad/s3b-vm-wedge-evidence/). apt/dnf installs are
// naturally no-op-idempotent when the package is already present, so only
// pacman-family distros need the rewrite.

func TestRenderCloudInit_PacmanFamily_RunsPacmanNeeded(t *testing.T) {
	for _, distro := range []string{"arch", "cachyos", "manjaro", "endeavouros"} {
		t.Run(distro, func(t *testing.T) {
			spec := &VmSpec{Source: VmSource{Kind: "cloud_image", Distro: distro, BaseUser: "arch"}}
			ci := &VmCloudInit{Package: []string{"htop"}, RunCmd: []string{"echo user-cmd"}}
			spec.CloudInit = ci
			rt := CloudInitRuntimeParams{SSHPublicKey: testPubKey, InjectKeyViaCloudInit: true, InstanceID: "iid", Hostname: "testvm"}
			userData, _, _, err := RenderCloudInit(spec, rt)
			if err != nil {
				t.Fatalf("RenderCloudInit: %v", err)
			}
			um := parseUserData(t, userData)

			if _, has := um["packages"]; has {
				t.Errorf("pacman-family render must OMIT the packages: key entirely, got %v", um["packages"])
			}
			rc, _ := um["runcmd"].([]any)
			// D18 (bed-robustness batch item 3): the sshd hardening drop-in write+validate and the
			// ssh.socket unmask now sit BETWEEN the pacman install and the sshd-enable step —
			// [pacman, hardening-dropin, unmask, sshd-enable, user-cmd].
			if len(rc) != 5 {
				t.Fatalf("runcmd = %v, want 5 entries [pacman, hardening-dropin, unmask, sshd-enable, user-cmd]", rc)
			}
			wantPacman := "pacman -S --needed --noconfirm openssh curl tar htop"
			if rc[0] != wantPacman {
				t.Errorf("runcmd[0] = %q, want %q (must be FIRST — before sshd is ever enabled)", rc[0], wantPacman)
			}
			if rc[1] != sshHardeningDropInCmd {
				t.Errorf("runcmd[1] = %v, want the sshd hardening drop-in command", rc[1])
			}
			if rc[2] != "systemctl unmask ssh.socket || true" {
				t.Errorf("runcmd[2] = %q, want the ssh.socket unmask", rc[2])
			}
			if rc[3] != "systemctl enable --now sshd" {
				t.Errorf("runcmd[3] = %q, want the sshd-enable command", rc[3])
			}
			if rc[4] != "echo user-cmd" {
				t.Errorf("runcmd[4] = %q, want the user's own runcmd entry, order preserved", rc[4])
			}
		})
	}
}

// TestRenderCloudInit_NonPacman_ByteIdenticalToPreFix locks the EXACT rendered
// user-data for every non-pacman distro — this is the containment proof: the
// pacman-family branch must be provably unreachable for rpm/deb, so these
// distros need no new runtime (bed) proof, only this pure-function golden.
func TestRenderCloudInit_NonPacman_ByteIdenticalToPreFix(t *testing.T) {
	cases := []struct {
		distro       string
		wantSSHPkg   string
		wantSSHdUnit string
	}{
		{"fedora", "openssh", "sshd"},
		{"debian", "openssh-server", "ssh"},
		{"ubuntu", "openssh-server", "ssh"},
	}
	for _, c := range cases {
		t.Run(c.distro, func(t *testing.T) {
			spec := &VmSpec{Source: VmSource{Kind: "cloud_image", Distro: c.distro, BaseUser: "op"}}
			ci := &VmCloudInit{Package: []string{"htop"}, RunCmd: []string{"echo user-cmd"}}
			spec.CloudInit = ci
			rt := CloudInitRuntimeParams{SSHPublicKey: testPubKey, InjectKeyViaCloudInit: true, InstanceID: "iid", Hostname: "testvm"}
			userData, _, _, err := RenderCloudInit(spec, rt)
			if err != nil {
				t.Fatalf("RenderCloudInit: %v", err)
			}
			um := parseUserData(t, userData)

			// packages: key present with the EXACT pre-fix union — never folded into runcmd.
			pkgs, _ := um["packages"].([]any)
			wantPkgs := []any{c.wantSSHPkg, "curl", "tar", "htop"}
			if len(pkgs) != len(wantPkgs) {
				t.Fatalf("packages = %v, want %v", pkgs, wantPkgs)
			}
			for i := range wantPkgs {
				if pkgs[i] != wantPkgs[i] {
					t.Errorf("packages[%d] = %v, want %v", i, pkgs[i], wantPkgs[i])
				}
			}
			// runcmd: {hardening-dropin, unmask, sshd-enable, user-cmd} — no pacman line ever
			// prepended. D18 (bed-robustness batch item 3) inserted the first two steps ahead of
			// the sshd-enable this test used to assert was runcmd[0] — the test name predates
			// that fix and is no longer literally "byte-identical to pre-fix" for THIS reason
			// (the pacman-vs-non-pacman CONTAINMENT the name documents still holds: no pacman
			// line here, only the new hardening steps every distro now gets equally).
			rc, _ := um["runcmd"].([]any)
			wantRC := []any{sshHardeningDropInCmd, "systemctl unmask ssh.socket || true", "systemctl enable --now " + c.wantSSHdUnit, "echo user-cmd"}
			if len(rc) != len(wantRC) {
				t.Fatalf("runcmd = %v, want %v", rc, wantRC)
			}
			for i := range wantRC {
				if rc[i] != wantRC[i] {
					t.Errorf("runcmd[%d] = %v, want %v", i, rc[i], wantRC[i])
				}
			}
		})
	}
}

func TestRenderCloudInit_ExtraSecondDocument(t *testing.T) {
	spec := &VmSpec{Source: VmSource{Kind: "cloud_image", BaseUser: "arch"}}
	ci := &VmCloudInit{Extra: "#cloud-config\nfinal_message: done\n"}
	spec.CloudInit = ci
	rt := CloudInitRuntimeParams{InstanceID: "iid", InjectKeyViaCloudInit: true, SSHPublicKey: testPubKey}
	userData, _, _, err := RenderCloudInit(spec, rt)
	if err != nil {
		t.Fatalf("RenderCloudInit: %v", err)
	}
	if !strings.Contains(userData, "\n---\n") {
		t.Errorf("Extra must be appended as a second --- document:\n%s", userData)
	}
	// The Extra's own leading #cloud-config is stripped (only the envelope keeps one).
	if strings.Count(userData, "#cloud-config") != 1 {
		t.Errorf("expected exactly one #cloud-config header, got %d:\n%s", strings.Count(userData, "#cloud-config"), userData)
	}
	if !strings.Contains(userData, "final_message: done") {
		t.Errorf("Extra body missing:\n%s", userData)
	}
}

// --- effectiveDistro inference (second R10 bed finding, same wedge): the
// first cut of the pacman-family fix silently never fired for eval-vm,
// because its charly.yml source: block declares no distro: field —
// Source.Distro was simply "". These lock the inference's three edges. ---

func TestRenderCloudInit_InferredArch_RunsPacmanNeeded(t *testing.T) {
	// No explicit distro: — exactly eval-vm's real charly.yml shape
	// (kind: cloud_image, base_user: arch, no distro:).
	spec := &VmSpec{Source: VmSource{Kind: "cloud_image", BaseUser: "arch"}}
	ci := &VmCloudInit{Package: []string{"htop"}, RunCmd: []string{"echo user-cmd"}}
	spec.CloudInit = ci
	rt := CloudInitRuntimeParams{SSHPublicKey: testPubKey, InjectKeyViaCloudInit: true, InstanceID: "iid", Hostname: "testvm"}
	userData, _, _, err := RenderCloudInit(spec, rt)
	if err != nil {
		t.Fatalf("RenderCloudInit: %v", err)
	}
	um := parseUserData(t, userData)

	if _, has := um["packages"]; has {
		t.Errorf("inferred-arch render must OMIT the packages: key, got %v", um["packages"])
	}
	rc, _ := um["runcmd"].([]any)
	wantPacman := "pacman -S --needed --noconfirm openssh curl tar htop"
	if len(rc) != 5 || rc[0] != wantPacman {
		t.Fatalf("runcmd = %v, want [%q, hardening-dropin, unmask, sshd-enable, echo user-cmd] (empty distro + base_user=arch must infer pacman-family)", rc, wantPacman)
	}
}

func TestRenderCloudInit_EmptyDistroNonArchBaseUser_StaysNonPacman(t *testing.T) {
	// The safe side of the inference: an unrecognized image (no distro:, and
	// base_user isn't "arch") must NEVER get a pacman command — falls through
	// to the untouched, pre-fix packages:-key path exactly as before.
	cases := []string{"", "ubuntu", "someotheruser"}
	for _, baseUser := range cases {
		t.Run("base_user="+baseUser, func(t *testing.T) {
			spec := &VmSpec{Source: VmSource{Kind: "cloud_image", BaseUser: baseUser}}
			ci := &VmCloudInit{Package: []string{"htop"}, RunCmd: []string{"echo user-cmd"}}
			spec.CloudInit = ci
			rt := CloudInitRuntimeParams{SSHPublicKey: testPubKey, InjectKeyViaCloudInit: true, InstanceID: "iid", Hostname: "testvm"}
			userData, _, _, err := RenderCloudInit(spec, rt)
			if err != nil {
				t.Fatalf("RenderCloudInit: %v", err)
			}
			um := parseUserData(t, userData)

			pkgs, _ := um["packages"].([]any)
			wantPkgs := []any{"openssh", "curl", "tar", "htop"}
			if len(pkgs) != len(wantPkgs) {
				t.Fatalf("packages = %v, want %v (must stay on the packages:-key path — no pacman command for an unrecognized image)", pkgs, wantPkgs)
			}
			rc, _ := um["runcmd"].([]any)
			wantRC := []any{sshHardeningDropInCmd, "systemctl unmask ssh.socket || true", "systemctl enable --now sshd", "echo user-cmd"}
			if len(rc) != len(wantRC) || rc[0] != wantRC[0] {
				t.Errorf("runcmd = %v, want %v (no pacman line ever prepended)", rc, wantRC)
			}
		})
	}
}

func TestRenderCloudInit_ExplicitNonPacmanDistro_UnaffectedByInference(t *testing.T) {
	// An EXPLICIT non-pacman distro must never be overridden by the base_user
	// inference, even in the contrived case base_user happens to be "arch".
	spec := &VmSpec{Source: VmSource{Kind: "cloud_image", Distro: "fedora", BaseUser: "arch"}}
	rt := CloudInitRuntimeParams{SSHPublicKey: testPubKey, InjectKeyViaCloudInit: true, InstanceID: "iid"}
	userData, _, _, err := RenderCloudInit(spec, rt)
	if err != nil {
		t.Fatalf("RenderCloudInit: %v", err)
	}
	um := parseUserData(t, userData)
	pkgs, _ := um["packages"].([]any)
	if len(pkgs) == 0 || pkgs[0] != "openssh" {
		t.Errorf("explicit distro=fedora must keep the packages: key (openssh first), got %v", um["packages"])
	}
}

func TestEffectiveDistro(t *testing.T) {
	cases := []struct {
		name string
		spec *VmSpec
		want string
	}{
		{"nil spec", nil, ""},
		{"explicit wins", &VmSpec{Source: VmSource{Kind: "cloud_image", Distro: "debian", BaseUser: "arch"}}, "debian"},
		{"inferred from cloud_image+arch base_user", &VmSpec{Source: VmSource{Kind: "cloud_image", BaseUser: "arch"}}, "arch"},
		{"no inference for non-cloud_image kind", &VmSpec{Source: VmSource{Kind: "bootc", BaseUser: "arch"}}, ""},
		{"no inference for non-arch base_user", &VmSpec{Source: VmSource{Kind: "cloud_image", BaseUser: "ubuntu"}}, ""},
		{"no inference with no base_user", &VmSpec{Source: VmSource{Kind: "cloud_image"}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := effectiveDistro(c.spec); got != c.want {
				t.Errorf("effectiveDistro = %q, want %q", got, c.want)
			}
		})
	}
}
