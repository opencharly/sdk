package deploykit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
