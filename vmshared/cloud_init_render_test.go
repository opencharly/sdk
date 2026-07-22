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
	spec := &VmSpec{Source: VmSource{Kind: "cloud_image", Distro: "arch", BaseUser: "arch"}}
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
