package deploykit

import (
	"reflect"
	"regexp"
	"testing"

	"github.com/opencharly/sdk/spec"
)

// TestCollectSecretsFromLabels / TestSecretArgs / TestCredServiceForSecret /
// TestCredKeyForSecret / TestApplySecretRefresh relocated from charly/secrets_test.go
// (Cutover B-1 fix round — a pr-validator FAIL caught the functions themselves left
// duplicated in-file on the first pass; this round wires every charly/secrets.go call
// site to this package and deletes the in-file originals + their tests, moving
// coverage here alongside the functions per R3/R10).

func TestGenerateRandomSecretToken(t *testing.T) {
	tok := GenerateRandomSecretToken(32)
	if len(tok) != 44 {
		t.Errorf("GenerateRandomSecretToken(32) length = %d, want 44 (43 base64 chars + 1 pad)", len(tok))
	}
	// Url-safe base64: no '+' or '/' characters.
	if matched, _ := regexp.MatchString(`^[A-Za-z0-9_=-]+$`, tok); !matched {
		t.Errorf("GenerateRandomSecretToken(32) = %q, contains non-url-safe-base64 characters", tok)
	}
	// Two calls must not collide (crypto/rand, astronomically unlikely to repeat).
	tok2 := GenerateRandomSecretToken(32)
	if tok == tok2 {
		t.Error("two GenerateRandomSecretToken(32) calls produced identical output")
	}
	// A different byteCount changes the encoded length predictably.
	if got := len(GenerateRandomSecretToken(16)); got == 0 {
		t.Error("GenerateRandomSecretToken(16) returned empty string")
	}
}

// TestPromptPassword proves the function never panics and surfaces a wrapped
// error when stdin is not a terminal (the case in any `go test` run — no live
// TTY is ever attached, so this is deterministic across environments without
// requiring interactive input or a fake terminal harness).
func TestPromptPassword(t *testing.T) {
	_, err := PromptPassword("test prompt: ")
	if err == nil {
		t.Skip("stdin is a real terminal in this environment — PromptPassword succeeded, nothing to assert")
	}
	if want := "reading password:"; !regexp.MustCompile(regexp.QuoteMeta(want)).MatchString(err.Error()) {
		t.Errorf("PromptPassword error = %q, want it to contain %q", err.Error(), want)
	}
}

func TestCollectSecretsFromLabels(t *testing.T) {
	labelSecrets := []spec.LabelSecretEntry{
		{Name: "api-key", Target: "/run/secrets/api_key", Env: "API_KEY"},
		{Name: "vnc-password", Target: "/run/secrets/vnc_password"},
	}

	secrets := CollectSecretsFromLabels("my-image", labelSecrets)
	if len(secrets) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(secrets))
	}

	if secrets[0].Name != "charly-my-image-api-key" {
		t.Errorf("secret[0].Name = %q, want %q", secrets[0].Name, "charly-my-image-api-key")
	}
	if secrets[0].Target != "/run/secrets/api_key" {
		t.Errorf("secret[0].Target = %q", secrets[0].Target)
	}
	if secrets[0].Env != "API_KEY" {
		t.Errorf("secret[0].Env = %q", secrets[0].Env)
	}
	if secrets[0].SecretName != "api-key" {
		t.Errorf("secret[0].SecretName = %q", secrets[0].SecretName)
	}

	if secrets[1].Name != "charly-my-image-vnc-password" {
		t.Errorf("secret[1].Name = %q", secrets[1].Name)
	}
}

func TestSecretArgs(t *testing.T) {
	secrets := []CollectedSecret{
		{Name: "charly-img-pass", Target: "/run/secrets/pass"},
		{Name: "charly-img-user", Target: "/run/secrets/user"},
	}
	args := SecretArgs(secrets)
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(args), args)
	}
	if args[0] != "--secret" || args[1] != "charly-img-pass,target=/run/secrets/pass" {
		t.Errorf("args[0:2] = %v", args[0:2])
	}
	if args[2] != "--secret" || args[3] != "charly-img-user,target=/run/secrets/user" {
		t.Errorf("args[2:4] = %v", args[2:4])
	}
}

func TestCredServiceForSecret(t *testing.T) {
	const credServiceVNC = "charly/vnc"
	tests := []struct {
		envVar string
		want   string
	}{
		{"VNC_PASSWORD", credServiceVNC},
		{"CUSTOM_SECRET", "charly/secret"},
	}
	for _, tt := range tests {
		got := CredServiceForSecret(tt.envVar, credServiceVNC)
		if got != tt.want {
			t.Errorf("CredServiceForSecret(%q, ...) = %q, want %q", tt.envVar, got, tt.want)
		}
	}
}

func TestCredKeyForSecret(t *testing.T) {
	if got := CredKeyForSecret("my-image", ""); got != "my-image" {
		t.Errorf("CredKeyForSecret(my-image, '') = %q", got)
	}
	if got := CredKeyForSecret("my-image", "work"); got != "my-image-work" {
		t.Errorf("CredKeyForSecret(my-image, work) = %q", got)
	}
}

func TestApplySecretRefresh_NamedAllAndUnmatched(t *testing.T) {
	base := []CollectedSecret{
		{Name: "charly-app-db-password", SecretName: "db-password"},
		{Name: "charly-app-api-key", SecretName: "api-key"},
	}

	out, unmatched := ApplySecretRefresh(append([]CollectedSecret(nil), base...), nil)
	if len(unmatched) != 0 || out[0].RotateOnConfig || out[1].RotateOnConfig {
		t.Fatal("no-op refresh must not rotate or report unmatched")
	}

	out, unmatched = ApplySecretRefresh(append([]CollectedSecret(nil), base...), []string{"db-password", "nope"})
	if !out[0].RotateOnConfig || out[1].RotateOnConfig {
		t.Errorf("named refresh rotated wrong set: %+v", out)
	}
	if !reflect.DeepEqual(unmatched, []string{"nope"}) {
		t.Errorf("unmatched = %v, want [nope]", unmatched)
	}

	out, unmatched = ApplySecretRefresh(append([]CollectedSecret(nil), base...), []string{"all"})
	if !out[0].RotateOnConfig || !out[1].RotateOnConfig || len(unmatched) != 0 {
		t.Errorf("'all' refresh must rotate everything: %+v unmatched=%v", out, unmatched)
	}
}

// The four remaining functions (ListProvisionedSecretNames, PodmanSecretExists,
// EnsurePodmanSecret, RemovePodmanSecrets) shell out to the real `podman`/`docker`
// binary. Every test below empties $PATH for its own process (t.Setenv, restored
// automatically) so exec.Command's LookPath deterministically fails REGARDLESS of
// whether this host has a live podman/docker daemon — proving the error-handling
// contract without ever touching a real secret store (host-state safety: a live
// EnsurePodmanSecret call would otherwise write a REAL podman secret on whatever
// host runs `go test`, exactly the class of accidental host-config mutation this
// project's disposable-only-autonomy discipline forbids for anything outside a
// `disposable: true` bed).

func withEmptyPATH(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

func TestListProvisionedSecretNames_NoEngineBinary(t *testing.T) {
	withEmptyPATH(t)
	names := ListProvisionedSecretNames("podman", "my-box")
	if names != nil {
		t.Errorf("ListProvisionedSecretNames with no engine binary on PATH = %v, want nil", names)
	}
}

func TestPodmanSecretExists_NoEngineBinary(t *testing.T) {
	withEmptyPATH(t)
	if PodmanSecretExists("podman", "charly-test-nonexistent-secret") {
		t.Error("PodmanSecretExists with no engine binary on PATH = true, want false")
	}
}

func TestEnsurePodmanSecret_NoEngineBinary(t *testing.T) {
	withEmptyPATH(t)
	if err := EnsurePodmanSecret("podman", "charly-test-nonexistent-secret", "value"); err == nil {
		t.Error("EnsurePodmanSecret with no engine binary on PATH returned nil error, want a wrapped exec failure")
	}
}

// TestRemovePodmanSecrets_NoEngineBinary proves the best-effort contract: it
// never panics or blocks even when every underlying `podman secret rm` fails.
func TestRemovePodmanSecrets_NoEngineBinary(t *testing.T) {
	withEmptyPATH(t)
	RemovePodmanSecrets("podman", []CollectedSecret{
		{Name: "charly-test-a"},
		{Name: "charly-test-b"},
	})
}
