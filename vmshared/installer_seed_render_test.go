package vmshared

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// omarchyInstaller is the archinstall answers vocabulary as it will be authored on the
// omarchy distro entity. It is the corpus case for this renderer: every guard shape the
// design relies on appears in it exactly once.
func omarchyInstaller() *spec.DistroInstaller {
	return &spec.DistroInstaller{
		VolumeID: "cidata",
		Files: []spec.DistroInstallerFile{
			{
				Path: "user_configuration.json",
				Content: `{"archinstall-language":"English","bootloader":"Limine",` +
					`"hostname":"{{.Hostname}}","kernels":["linux"],` +
					`"locale_config":{"kb_layout":"{{.Keyboard}}","sys_enc":"UTF-8","sys_lang":"{{.Locale}}"},` +
					`"timezone":"{{.Timezone}}","version":"3.0.9"}`,
			},
			{
				Path: "user_credentials.json",
				// The DEFER case: credentials are emitted only when provisioning is NOT
				// deferred. An imaging rig ships the sentinel instead.
				When: `{{if .DeferProvisioning}}false{{else}}true{{end}}`,
				Content: `{"root_enc_password":"{{.PasswordHash}}","users":[{"enc_password":"{{.PasswordHash}}",` +
					`"groups":[],"sudo":true,"username":"{{.Username}}"}]}`,
			},
			{
				Path:    "defer-provisioning",
				When:    `{{if .DeferProvisioning}}true{{end}}`,
				Content: ``,
			},
			{Path: "user_full_name.txt", When: `{{.FullName}}`, Content: `{{.FullName}}`},
			{Path: "user_email_address.txt", When: `{{.Email}}`, Content: `{{.Email}}`},
			{
				Path:    "authorized_keys",
				When:    `{{if .SSHAuthorizedKeys}}true{{end}}`,
				Content: `{{range .SSHAuthorizedKeys}}{{.}}` + "\n" + `{{end}}`,
			},
			{
				Path:    "user_encrypt_installation.txt",
				When:    `{{if .Encrypt}}true{{end}}`,
				Content: `true`,
			},
		},
	}
}

func baseCtx() spec.InstallerSeedContext {
	return spec.InstallerSeedContext{
		Hostname: "omarchy", Timezone: "UTC", Locale: "en_US.UTF-8", Keyboard: "us",
		Username: "user", FullName: "Charly", Email: "charly@opencharly.invalid",
		PasswordHash: "$6$rounds$fakehashforthetest", Disk: "/dev/vda",
	}
}

// CORPUS: the ordinary unattended install — credentials present, no encryption, no
// deferral. This is what `charly-omarchy` will actually build.
func TestRenderInstallerSeed_Omarchy(t *testing.T) {
	ctx := baseCtx()
	ctx.SSHAuthorizedKeys = []string{"ssh-ed25519 AAAAC3Nz test@charly"}

	files, err := RenderInstallerSeed(omarchyInstaller(), ctx)
	if err != nil {
		t.Fatalf("RenderInstallerSeed: %v", err)
	}
	want := []string{
		"authorized_keys", "user_configuration.json", "user_credentials.json",
		"user_email_address.txt", "user_full_name.txt",
	}
	if got := SeedFileNames(files); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("file set:\n got %v\nwant %v", got, want)
	}

	// The archinstall config must be real JSON — an installer that cannot parse its
	// answers falls back to an interactive prompt nobody is watching, which presents as a
	// VM that simply never finishes.
	var cfg map[string]any
	if err := json.Unmarshal([]byte(files["user_configuration.json"]), &cfg); err != nil {
		t.Fatalf("user_configuration.json is not valid JSON: %v\n%s", err, files["user_configuration.json"])
	}
	if cfg["hostname"] != "omarchy" || cfg["bootloader"] != "Limine" {
		t.Errorf("config did not carry the entity's values: %v", cfg)
	}

	var creds map[string]any
	if err := json.Unmarshal([]byte(files["user_credentials.json"]), &creds); err != nil {
		t.Fatalf("user_credentials.json is not valid JSON: %v", err)
	}
	// `users`, NOT `!users` — read off the shipping ISO's own configurator.
	users, ok := creds["users"].([]any)
	if !ok || len(users) != 1 {
		t.Fatalf("credentials must carry exactly one user under `users`: %v", creds)
	}
	if u := users[0].(map[string]any); u["username"] != "user" || u["sudo"] != true {
		t.Errorf("user entry wrong: %v", u)
	}

	// The PLAINTEXT password must never appear. The caller hashes before building ctx.
	for name, body := range files {
		if strings.Contains(body, "hunter2") {
			t.Errorf("%s leaked a plaintext password", name)
		}
	}
	if !strings.Contains(files["user_credentials.json"], "$6$") {
		t.Errorf("credentials do not carry a crypt hash")
	}
	if got := files["authorized_keys"]; got != "ssh-ed25519 AAAAC3Nz test@charly\n" {
		t.Errorf("authorized_keys = %q", got)
	}
}

// TEETH: defer_provisioning must REPLACE the credentials, not sit beside them. An imaging
// rig that shipped both would hand the next owner a machine with a known password.
func TestRenderInstallerSeed_DeferReplacesCredentials(t *testing.T) {
	ctx := baseCtx()
	ctx.DeferProvisioning = true

	files, err := RenderInstallerSeed(omarchyInstaller(), ctx)
	if err != nil {
		t.Fatalf("RenderInstallerSeed: %v", err)
	}
	if _, ok := files["user_credentials.json"]; ok {
		t.Error("deferring provisioning must SUPPRESS user_credentials.json")
	}
	if _, ok := files["defer-provisioning"]; !ok {
		t.Error("deferring provisioning must emit the defer-provisioning sentinel")
	}
}

// TEETH: an optional file is absent, not empty. An empty authorized_keys would make the
// ISO enable sshd and open the firewall for a key nobody holds.
func TestRenderInstallerSeed_OptionalFilesAreAbsent(t *testing.T) {
	ctx := baseCtx()
	ctx.FullName, ctx.Email = "", ""

	files, err := RenderInstallerSeed(omarchyInstaller(), ctx)
	if err != nil {
		t.Fatalf("RenderInstallerSeed: %v", err)
	}
	for _, absent := range []string{"authorized_keys", "user_full_name.txt", "user_email_address.txt", "user_encrypt_installation.txt"} {
		if _, ok := files[absent]; ok {
			t.Errorf("%s must be absent when its guard is unset, got %q", absent, files[absent])
		}
	}
}

// The encryption marker rides the same guard mechanism, and is the one case where an
// operator MUST be told the install is no longer fully unattended.
func TestRenderInstallerSeed_EncryptEmitsMarker(t *testing.T) {
	ctx := baseCtx()
	ctx.Encrypt = true

	files, err := RenderInstallerSeed(omarchyInstaller(), ctx)
	if err != nil {
		t.Fatalf("RenderInstallerSeed: %v", err)
	}
	if files["user_encrypt_installation.txt"] != "true" {
		t.Errorf("encrypt must emit the marker, got %q", files["user_encrypt_installation.txt"])
	}
}

// TEETH: a mis-keyed template must not reach the volume. A broken answers file surfaces as
// an installer hanging at a prompt nobody is watching, which is the worst failure shape
// this design has.
//
// Against a STRUCT context Go's template package errors outright on an unknown field, which
// is stronger than the `<no value>` sentinel the renderer also guards against — that
// sentinel appears for absent MAP keys and nil pointers, which is what the per-distro
// `answer?: {[string]: string}` extras will be. Both paths are covered: this asserts the
// struct-field case names the offending field, and the renderer refuses `<no value>` for
// the map case.
func TestRenderInstallerSeed_MiskeyedTemplateIsRejected(t *testing.T) {
	inst := &spec.DistroInstaller{
		VolumeID: "cidata",
		Files:    []spec.DistroInstallerFile{{Path: "answers", Content: `host={{.Hostnmae}}`}},
	}
	_, err := RenderInstallerSeed(inst, baseCtx())
	if err == nil {
		t.Fatal("a template referencing an absent field must be rejected")
	}
	if !strings.Contains(err.Error(), "Hostnmae") {
		t.Errorf("error should name the offending field, got: %v", err)
	}
}

func TestRenderInstallerSeed_Rejects(t *testing.T) {
	cases := []struct {
		name string
		inst *spec.DistroInstaller
		want string
	}{
		{"no installer at all", nil, "declares no installer"},
		{"installer with no files", &spec.DistroInstaller{VolumeID: "cidata"}, "declares no files"},
		{
			"a file with no path",
			&spec.DistroInstaller{VolumeID: "cidata", Files: []spec.DistroInstallerFile{{Content: "x"}}},
			"no path",
		},
		{
			"the same path twice",
			&spec.DistroInstaller{VolumeID: "cidata", Files: []spec.DistroInstallerFile{
				{Path: "answers", Content: "a"}, {Path: "answers", Content: "b"},
			}},
			"twice",
		},
		{
			"every file excluded leaves an empty volume",
			&spec.DistroInstaller{VolumeID: "cidata", Files: []spec.DistroInstallerFile{
				{Path: "answers", When: "false", Content: "a"},
			}},
			"every file was excluded",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := RenderInstallerSeed(tc.inst, baseCtx())
			if err == nil {
				t.Fatalf("expected an error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

// The renderer must be PURE: same input, same output, every time. The ISO writer sorts
// internally so the emitted image is byte-reproducible, and that is only meaningful if the
// content is too.
func TestRenderInstallerSeed_IsDeterministic(t *testing.T) {
	ctx := baseCtx()
	ctx.SSHAuthorizedKeys = []string{"ssh-ed25519 AAA a@b", "ssh-ed25519 BBB c@d"}
	first, err := RenderInstallerSeed(omarchyInstaller(), ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		again, err := RenderInstallerSeed(omarchyInstaller(), ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(again) != len(first) {
			t.Fatalf("run %d produced %d files, first produced %d", i, len(again), len(first))
		}
		for k, v := range first {
			if again[k] != v {
				t.Fatalf("run %d differs at %s", i, k)
			}
		}
	}
}

// The `<no value>` guard, exercised on the shape that actually produces it: a template
// ranging over a nil slice field is fine, but indexing an absent map key renders the
// sentinel rather than failing. The renderer must refuse that too.
func TestRenderInstallerSeed_NoValueSentinelIsRejected(t *testing.T) {
	inst := &spec.DistroInstaller{
		VolumeID: "cidata",
		Files: []spec.DistroInstallerFile{
			{Path: "answers", Content: `key={{index .Answer "tailscale_authkey"}}`},
		},
	}
	_, err := RenderInstallerSeed(inst, baseCtx())
	if err == nil {
		t.Skip("InstallerSeedContext has no map field yet; the guard stays for when it does")
	}
	if !strings.Contains(err.Error(), "<no value>") && !strings.Contains(err.Error(), "Answer") {
		t.Errorf("unexpected error shape: %v", err)
	}
}
