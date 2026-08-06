package deploykit

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/sdk/vmshared"
	"github.com/opencharly/spec/spec"
)

// TestIsEncryptedInitialized / TestHasEncryptedBindMounts / TestEncServiceFilename /
// TestVerifyBindMounts* relocated from charly/enc_test.go (Cutover B unit 2) alongside the
// functions themselves.

func TestIsEncryptedInitialized(t *testing.T) {
	// Non-existent directory
	if IsEncryptedInitialized("/nonexistent/cipher") {
		t.Error("expected false for nonexistent directory")
	}

	// Directory without gocryptfs.conf
	dir := t.TempDir()
	if IsEncryptedInitialized(dir) {
		t.Error("expected false for dir without gocryptfs.conf")
	}
}

func TestHasEncryptedBindMounts(t *testing.T) {
	tests := []struct {
		name   string
		mounts []ResolvedBindMount
		want   bool
	}{
		{"nil", nil, false},
		{"empty", []ResolvedBindMount{}, false},
		{"plain only", []ResolvedBindMount{{Encrypted: false}}, false},
		{"encrypted", []ResolvedBindMount{{Encrypted: true}}, true},
		{"mixed", []ResolvedBindMount{{Encrypted: false}, {Encrypted: true}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasEncryptedBindMounts(tt.mounts)
			if got != tt.want {
				t.Errorf("HasEncryptedBindMounts() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEncServiceFilename(t *testing.T) {
	tests := []struct {
		image string
		want  string
	}{
		{"myapp", "charly-myapp-enc.service"},
		{"openclaw", "charly-openclaw-enc.service"},
	}
	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			got := EncServiceFilename(tt.image)
			if got != tt.want {
				t.Errorf("EncServiceFilename(%q) = %q, want %q", tt.image, got, tt.want)
			}
		})
	}
}

func TestVerifyBindMountsPlainDirMissing(t *testing.T) {
	mounts := []ResolvedBindMount{
		{Name: "data", HostPath: "/nonexistent/path", ContPath: "/home/user/.myapp", Encrypted: false},
	}
	err := VerifyBindMounts(mounts, "myapp")
	if err == nil {
		t.Fatal("expected error for missing host dir")
	}
	if !strings.Contains(err.Error(), "bind mount \"data\"") {
		t.Errorf("error should reference bind mount name, got: %v", err)
	}
}

func TestVerifyBindMountsPlainDirExists(t *testing.T) {
	dir := t.TempDir()
	mounts := []ResolvedBindMount{
		{Name: "data", HostPath: dir, ContPath: "/home/user/.myapp", Encrypted: false},
	}
	err := VerifyBindMounts(mounts, "myapp")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVerifyBindMountsEncryptedNotMounted(t *testing.T) {
	// Mock IsEncryptedMounted to always return false
	orig := IsEncryptedMounted
	IsEncryptedMounted = func(plainDir string) bool { return false }
	defer func() { IsEncryptedMounted = orig }()

	mounts := []ResolvedBindMount{
		{Name: "secrets", HostPath: "/tmp/plain", ContPath: "/home/user/.secrets", Encrypted: true},
	}
	err := VerifyBindMounts(mounts, "myapp")
	if err == nil {
		t.Fatal("expected error for unmounted encrypted volume")
	}
	if !strings.Contains(err.Error(), "not mounted") {
		t.Errorf("error should mention 'not mounted', got: %v", err)
	}
	if !strings.Contains(err.Error(), "charly config mount") {
		t.Errorf("error should suggest 'charly config mount', got: %v", err)
	}
}

func TestVerifyBindMountsEncryptedMounted(t *testing.T) {
	// Mock IsEncryptedMounted to always return true
	orig := IsEncryptedMounted
	IsEncryptedMounted = func(plainDir string) bool { return true }
	defer func() { IsEncryptedMounted = orig }()

	mounts := []ResolvedBindMount{
		{Name: "secrets", HostPath: "/tmp/plain", ContPath: "/home/user/.secrets", Encrypted: true},
	}
	err := VerifyBindMounts(mounts, "myapp")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestFuseAllowOtherEnabled relocated from charly/enc_fuse_test.go (Cutover B unit 2) alongside
// the function itself.
func TestFuseAllowOtherEnabled(t *testing.T) {
	withFuseConf := func(body string) {
		orig := FuseConfPath
		t.Cleanup(func() { FuseConfPath = orig })
		if body == "\x00" {
			FuseConfPath = filepath.Join(t.TempDir(), "absent-fuse.conf")
			return
		}
		p := filepath.Join(t.TempDir(), "fuse.conf")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		FuseConfPath = p
	}

	cases := []struct {
		name string
		body string
		want bool
	}{
		{"active", "# comment\nuser_allow_other\n#mount_max = 1000\n", true},
		{"active-trailing-space", "  user_allow_other  \n", true},
		{"commented", "#user_allow_other\n", false},
		{"commented-spaced", "# user_allow_other - description line\n", false},
		{"absent", "# nothing here\n#mount_max = 1000\n", false},
		{"missing-file", "\x00", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withFuseConf(c.body)
			if got := FuseAllowOtherEnabled(); got != c.want {
				t.Fatalf("FuseAllowOtherEnabled() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestCipherPopulatedPlainEmpty relocated from charly/enc_cipher_test.go (Cutover B unit 2)
// alongside the function itself.
func TestCipherPopulatedPlainEmpty(t *testing.T) {
	mk := func(t *testing.T, cipherFiles, plainFiles []string) (cipher, plain string) {
		t.Helper()
		dir := t.TempDir()
		cipher = filepath.Join(dir, "cipher")
		plain = filepath.Join(dir, "plain")
		if err := os.MkdirAll(cipher, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(plain, 0o700); err != nil {
			t.Fatal(err)
		}
		for _, f := range cipherFiles {
			if err := os.WriteFile(filepath.Join(cipher, f), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		for _, f := range plainFiles {
			if err := os.WriteFile(filepath.Join(plain, f), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return cipher, plain
	}

	t.Run("dangerous: cipher populated, plain empty", func(t *testing.T) {
		cipher, plain := mk(t,
			[]string{"gocryptfs.conf", "gocryptfs.diriv", "AbCdEfGh", "QrStUvWx"},
			nil,
		)
		if !CipherPopulatedPlainEmpty(cipher, plain) {
			t.Error("expected true (cipher has user data, plain empty)")
		}
	})

	t.Run("benign: cipher metadata-only, plain empty (fresh init)", func(t *testing.T) {
		cipher, plain := mk(t,
			[]string{"gocryptfs.conf", "gocryptfs.diriv"},
			nil,
		)
		if CipherPopulatedPlainEmpty(cipher, plain) {
			t.Error("expected false (cipher only has metadata files)")
		}
	})

	t.Run("benign: plain non-empty (FUSE was mounted then containerwrote, OR plain has stale plaintext drift)", func(t *testing.T) {
		cipher, plain := mk(t,
			[]string{"gocryptfs.conf", "AbCdEfGh"},
			[]string{"some-file"},
		)
		if CipherPopulatedPlainEmpty(cipher, plain) {
			t.Error("expected false (plain not empty — different failure class)")
		}
	})

	t.Run("missing cipher dir", func(t *testing.T) {
		dir := t.TempDir()
		plain := filepath.Join(dir, "plain")
		if err := os.MkdirAll(plain, 0o700); err != nil {
			t.Fatal(err)
		}
		if CipherPopulatedPlainEmpty(filepath.Join(dir, "missing-cipher"), plain) {
			t.Error("expected false (cipher dir does not exist)")
		}
	})

	t.Run("missing plain dir", func(t *testing.T) {
		dir := t.TempDir()
		cipher := filepath.Join(dir, "cipher")
		if err := os.MkdirAll(cipher, 0o700); err != nil {
			t.Fatal(err)
		}
		if CipherPopulatedPlainEmpty(cipher, filepath.Join(dir, "missing-plain")) {
			t.Error("expected false (plain dir does not exist)")
		}
	})
}

// --- LoadEncryptedVolumeFromConfig / EncPlanForConfig / EncStatusFromConfig ---
//
// These are the SEAM-ROUTABLE siblings of LoadEncryptedVolume/EncPlanFor/EncStatus: instead
// of reaching the package-level LoadFleetConfig() themselves, they take an ALREADY-LOADED
// *FleetConfig. Every fixture below sets an explicit Host: on its encrypted volume so the
// derived CipherDir/PlainDir/ScopeUnit are fully deterministic — independent of whatever
// engine.encrypted_storage_path a live ~/.config/charly/charly.yml on the test host might
// carry (only kit.ResolveRuntime()'s ENGINE AUTO-DETECT is exercised for real, which needs
// nothing beyond `podman`/`docker` on PATH and never fails the test on its own).

// encFixtureConfig builds a *FleetConfig with one encrypted + one plain volume under
// DeployKey(boxName, instance), each with an explicit Host: so downstream path derivation
// needs no runtime-config lookup.
func encFixtureConfig(instance, encHost, plainHost string) *FleetConfig {
	const boxName = "myapp"
	return &FleetConfig{
		Fleet: map[string]FleetNode{
			DeployKey(boxName, instance): {
				Volume: []spec.DeployVolume{
					{Name: "secrets", Type: "encrypted", Host: encHost},
					{Name: "data", Type: "plain", Host: plainHost},
				},
			},
		},
	}
}

func TestLoadEncryptedVolumeFromConfig(t *testing.T) {
	t.Run("filters to encrypted-type volumes only, for the matching deploy key", func(t *testing.T) {
		dc := encFixtureConfig("", "/srv/enc/secrets", "/srv/plain/data")
		mounts, _, err := LoadEncryptedVolumeFromConfig(dc, "myapp", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mounts) != 1 || mounts[0].Name != "secrets" || mounts[0].Type != "encrypted" {
			t.Errorf("LoadEncryptedVolumeFromConfig() mounts = %+v, want exactly the one encrypted volume", mounts)
		}
	})

	t.Run("no entry for the deploy key returns an empty (not error) result", func(t *testing.T) {
		dc := encFixtureConfig("", "/srv/enc/secrets", "/srv/plain/data")
		mounts, _, err := LoadEncryptedVolumeFromConfig(dc, "other-app", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mounts) != 0 {
			t.Errorf("LoadEncryptedVolumeFromConfig(unmatched key) mounts = %+v, want empty", mounts)
		}
	})

	t.Run("nil dc (no per-host overlay) returns an empty result, no error", func(t *testing.T) {
		mounts, _, err := LoadEncryptedVolumeFromConfig(nil, "myapp", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mounts) != 0 {
			t.Errorf("LoadEncryptedVolumeFromConfig(nil dc) mounts = %+v, want empty", mounts)
		}
	})

	t.Run("instance-qualified deploy key is looked up distinctly from the base key", func(t *testing.T) {
		dc := encFixtureConfig("prod", "/srv/enc/secrets-prod", "/srv/plain/data-prod")
		// The base (no-instance) key must NOT see the instance-qualified entry's volumes.
		baseMounts, _, err := LoadEncryptedVolumeFromConfig(dc, "myapp", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(baseMounts) != 0 {
			t.Errorf("base-key lookup leaked the instance-qualified entry: %+v", baseMounts)
		}
		instMounts, _, err := LoadEncryptedVolumeFromConfig(dc, "myapp", "prod")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(instMounts) != 1 || instMounts[0].Host != "/srv/enc/secrets-prod" {
			t.Errorf("instance-key lookup = %+v, want the prod-instance encrypted volume", instMounts)
		}
	})
}

func TestEncPlanForConfig(t *testing.T) {
	t.Run("builds a plan entry with the deterministic derived paths + fresh-state defaults", func(t *testing.T) {
		dc := encFixtureConfig("", "/srv/enc/secrets", "/srv/plain/data")
		plan, err := EncPlanForConfig(dc, "myapp", "", "", "myapp")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(plan) != 1 {
			t.Fatalf("EncPlanForConfig() plan = %+v, want exactly 1 entry (plain volume excluded)", plan)
		}
		got := plan[0]
		if got.Name != "secrets" {
			t.Errorf("plan[0].Name = %q, want secrets", got.Name)
		}
		if got.CipherDir != filepath.Join("/srv/enc/secrets", "cipher") {
			t.Errorf("plan[0].CipherDir = %q, want %q", got.CipherDir, filepath.Join("/srv/enc/secrets", "cipher"))
		}
		if got.PlainDir != filepath.Join("/srv/enc/secrets", "plain") {
			t.Errorf("plan[0].PlainDir = %q, want %q", got.PlainDir, filepath.Join("/srv/enc/secrets", "plain"))
		}
		if got.ScopeUnit != "charly-enc-myapp-secrets" {
			t.Errorf("plan[0].ScopeUnit = %q, want charly-enc-myapp-secrets", got.ScopeUnit)
		}
		// Neither /srv/enc/secrets/cipher nor its gocryptfs.conf exist on this test host, so
		// Initialized must be false — proving IsEncryptedInitialized was actually consulted
		// (not just defaulted true).
		if got.Initialized {
			t.Error("plan[0].Initialized = true, want false (no gocryptfs.conf on disk)")
		}
	})

	t.Run("Mounted reflects the IsEncryptedMounted probe (package var, mockable)", func(t *testing.T) {
		orig := IsEncryptedMounted
		defer func() { IsEncryptedMounted = orig }()
		var probedPath string
		IsEncryptedMounted = func(plainDir string) bool {
			probedPath = plainDir
			return true
		}
		dc := encFixtureConfig("", "/srv/enc/secrets", "/srv/plain/data")
		plan, err := EncPlanForConfig(dc, "myapp", "", "", "myapp")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(plan) != 1 || !plan[0].Mounted {
			t.Fatalf("plan = %+v, want Mounted true (from the mocked probe)", plan)
		}
		wantPath := filepath.Join("/srv/enc/secrets", "plain")
		if probedPath != wantPath {
			t.Errorf("IsEncryptedMounted was probed with %q, want %q", probedPath, wantPath)
		}
	})

	t.Run("volume filter narrows the plan to the named volume only", func(t *testing.T) {
		dc := &FleetConfig{
			Fleet: map[string]FleetNode{
				DeployKey("myapp", ""): {
					Volume: []spec.DeployVolume{
						{Name: "secrets", Type: "encrypted", Host: "/srv/enc/secrets"},
						{Name: "media", Type: "encrypted", Host: "/srv/enc/media"},
					},
				},
			},
		}
		plan, err := EncPlanForConfig(dc, "myapp", "", "media", "myapp")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(plan) != 1 || plan[0].Name != "media" {
			t.Errorf("EncPlanForConfig(volume filter) = %+v, want exactly the media volume", plan)
		}
	})

	t.Run("volume filter matching nothing returns an empty plan, no error", func(t *testing.T) {
		dc := encFixtureConfig("", "/srv/enc/secrets", "/srv/plain/data")
		plan, err := EncPlanForConfig(dc, "myapp", "", "does-not-exist", "myapp")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(plan) != 0 {
			t.Errorf("EncPlanForConfig(unmatched filter) = %+v, want empty", plan)
		}
	})

	t.Run("no encrypted volumes at all returns an empty plan, no error", func(t *testing.T) {
		dc := &FleetConfig{Fleet: map[string]FleetNode{
			DeployKey("myapp", ""): {Volume: []spec.DeployVolume{{Name: "data", Type: "plain", Host: "/srv/plain/data"}}},
		}}
		plan, err := EncPlanForConfig(dc, "myapp", "", "", "myapp")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(plan) != 0 {
			t.Errorf("EncPlanForConfig(no encrypted volumes) = %+v, want empty", plan)
		}
	})
}

// captureStdout redirects os.Stdout for the duration of fn and returns everything written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe writer: %v", err)
	}
	os.Stdout = orig
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("reading pipe: %v", err)
	}
	return buf.String()
}

func TestEncStatusFromConfig(t *testing.T) {
	t.Run("no encrypted mounts prints the empty-state message", func(t *testing.T) {
		dc := &FleetConfig{Fleet: map[string]FleetNode{
			DeployKey("myapp", ""): {Volume: []spec.DeployVolume{{Name: "data", Type: "plain", Host: "/srv/plain/data"}}},
		}}
		out := captureStdout(t, func() {
			if err := EncStatusFromConfig(dc, "myapp", ""); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
		if !strings.Contains(out, "No encrypted bind mounts configured") {
			t.Errorf("output %q missing the empty-state message", out)
		}
	})

	t.Run("prints one row per encrypted mount with its live initialized/mounted state", func(t *testing.T) {
		orig := IsEncryptedMounted
		defer func() { IsEncryptedMounted = orig }()
		IsEncryptedMounted = func(plainDir string) bool { return true }

		dc := encFixtureConfig("", "/srv/enc/secrets", "/srv/plain/data")
		out := captureStdout(t, func() {
			if err := EncStatusFromConfig(dc, "myapp", ""); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
		if !strings.Contains(out, "secrets") {
			t.Errorf("output %q missing the volume name", out)
		}
		if !strings.Contains(out, fmt.Sprintf("%-20s %-12s %-8s %s", "secrets", "no", "yes", filepath.Join("/srv/enc/secrets", "plain"))) {
			t.Errorf("output %q missing the expected formatted row (initialized=no, mounted=yes)", out)
		}
	})
}

// TestCryptoPasswdRequiresUnmount / TestCryptoPasswdPasswordMismatch relocated from
// charly/enc_test.go (#55 K4 cone3): pure IsEncryptedMounted/AskPassword/EncryptedPlainDir
// mock-driven coverage with zero charly dependency (charly's own encPasswd() needs deploy.yml,
// so these simulate its logic against the deploykit primitives directly).

func TestCryptoPasswdRequiresUnmount(t *testing.T) {
	// Mock IsEncryptedMounted to return true (volume is mounted)
	origMounted := IsEncryptedMounted
	IsEncryptedMounted = func(plainDir string) bool { return true }
	defer func() { IsEncryptedMounted = origMounted }()

	boxName := "myapp"
	// We can't call encPasswd() directly because loadEncryptedVolume needs deploy.yml,
	// so test the logic by simulating what encPasswd() does.
	mounts := []vmshared.DeployVolumeConfig{
		{Name: "secrets", Type: "encrypted"},
	}
	storagePath := "/data/enc"

	for _, m := range mounts {
		plainDir := EncryptedPlainDir(storagePath, boxName, m.Name)
		if IsEncryptedMounted(plainDir) {
			err := fmt.Errorf("encrypted volume %q is still mounted; run 'charly config unmount %s' first", m.Name, boxName)
			if !strings.Contains(err.Error(), "still mounted") {
				t.Errorf("expected 'still mounted' in error, got: %v", err)
			}
			if !strings.Contains(err.Error(), "charly config unmount") {
				t.Errorf("expected 'charly config unmount' hint in error, got: %v", err)
			}
			return
		}
	}
	t.Fatal("expected mounted volume to trigger error")
}

func TestCryptoPasswdPasswordMismatch(t *testing.T) {
	// Mock AskPassword to return controlled values
	origAsk := AskPassword
	callCount := 0
	AskPassword = func(id, prompt string) (string, error) {
		callCount++
		switch callCount {
		case 1:
			return "oldpass", nil // current
		case 2:
			return "newpass", nil // new
		case 3:
			return "different", nil // confirm (mismatch)
		}
		return "", fmt.Errorf("unexpected call")
	}
	defer func() { AskPassword = origAsk }()

	// Mock IsEncryptedMounted to return false (all unmounted)
	origMounted := IsEncryptedMounted
	IsEncryptedMounted = func(plainDir string) bool { return false }
	defer func() { IsEncryptedMounted = origMounted }()

	// Simulate the password check logic from Run()
	oldPass, _ := AskPassword("test-old", "Current passphrase:")
	newPass, _ := AskPassword("test-new", "New passphrase:")
	confirmPass, _ := AskPassword("test-confirm", "Confirm new passphrase:")

	_ = oldPass
	if newPass != confirmPass {
		// This is the expected path
		return
	}
	t.Fatal("expected password mismatch to be detected")
}
