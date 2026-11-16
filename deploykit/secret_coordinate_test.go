package deploykit

// secret_coordinate_test.go — pins the read/write agreement that opencharly/sdk#526 exposed.
//
// Before this change the two sides of a candy-owned secret disagreed:
//
//	READ  (ResolveSecretValue): an explicit `key:` override is queried exactly at
//	                            (Service, Key) — e.g. charly/api-key/qdrant.
//	WRITE (ProvisionPodmanSecrets): auto-generation, the interactive prompt and the operator
//	                            hint all hardcoded ("charly/secret", podman-secret-name).
//
// so a candy declaring `key: charly/api-key/<name>` could never converge: first config
// auto-generated a value at the DEFAULT coordinate, the next reconcile read the DECLARED one
// (miss), generated another value, overwrote the default — and the operator following the docs
// (`charly secrets get charly/api-key <name>`) found nothing. Measured on layer-qdrant's
// QDRANT__SERVICE__API_KEY, whose description advertises exactly that coordinate.
//
// Pinned here:
//  1. secretCoordinate is the one rule: explicit (Service, Key) wins, otherwise the historical
//     default;
//  2. auto-generation WRITES to the same coordinate the resolver READS from;
//  3. a secret without an override still lands at the historical default (no behaviour change
//     for every existing candy).

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSecretCoordinate pins the rule itself.
func TestSecretCoordinate(t *testing.T) {
	withOverride := CollectedSecret{Name: "charly-app-api-key", SecretName: "API_KEY", Service: "charly/api-key", Key: "qdrant"}
	if svc, key, explicit := secretCoordinate(withOverride); svc != "charly/api-key" || key != "qdrant" || !explicit {
		t.Errorf("secretCoordinate(with override) = (%q, %q, %v), want (charly/api-key, qdrant, true)", svc, key, explicit)
	}
	def := CollectedSecret{Name: "charly-app-api-key", SecretName: "API_KEY"}
	if svc, key, explicit := secretCoordinate(def); svc != "charly/secret" || key != "charly-app-api-key" || explicit {
		t.Errorf("secretCoordinate(default) = (%q, %q, %v), want (charly/secret, charly-app-api-key, false)", svc, key, explicit)
	}
	// A half-set override is not an override: both fields must be present (same rule the
	// resolver applies).
	half := CollectedSecret{Name: "charly-app-api-key", Service: "charly/api-key"}
	if svc, key, explicit := secretCoordinate(half); svc != "charly/secret" || key != "charly-app-api-key" || explicit {
		t.Errorf("secretCoordinate(half override) = (%q, %q, %v), want the default", svc, key, explicit)
	}
}

// TestResolveSecretValue_ReadsTheOverride pins the read side so the two cannot drift again.
func TestResolveSecretValue_ReadsTheOverride(t *testing.T) {
	var asked [][2]string
	resolve := func(_ /*env*/, service, key, _ string) (string, string) {
		asked = append(asked, [2]string{service, key})
		return "v", "store"
	}
	s := CollectedSecret{Name: "charly-app-api-key", Env: "API_KEY", Service: "charly/api-key", Key: "qdrant"}
	if v, _ := ResolveSecretValue(s, "app", "", "", resolve); v != "v" {
		t.Fatalf("ResolveSecretValue = %q, want v", v)
	}
	if len(asked) != 1 || asked[0] != [2]string{"charly/api-key", "qdrant"} {
		t.Errorf("resolver asked %v, want exactly one query at (charly/api-key, qdrant)", asked)
	}
}

// fakeEngine installs a `podman` script that reports "no such secret", accepts `secret rm` and
// `secret create`, and records every invocation — enough to drive the provisioning loop without
// a container engine.
func fakeEngine(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
echo "$@" >> "` + dir + `/calls.log"
case "$1" in
  secret)
    case "$2" in
      inspect) exit 1 ;;            # never exists → no short-circuit
      rm)      exit 0 ;;
      create)  cat > /dev/null; exit 0 ;;
    esac
    exit 0 ;;
esac
exit 0
`
	bin := filepath.Join(dir, "podman")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatalf("writing fake engine: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

// TestProvisionPodmanSecrets_AutoGenWritesToTheOverride is the regression test for sdk#526: with
// an explicit `key:` declared, the auto-generated value must be persisted where the resolver
// reads it — not at the hardcoded default.
func TestProvisionPodmanSecrets_AutoGenWritesToTheOverride(t *testing.T) {
	fakeEngine(t)
	var writes [][3]string
	cred := CredentialAccess{
		Resolve: func(_, _, _, _ string) (string, string) { return "", "" }, // nothing stored yet
		Write: func(service, key, value string) error {
			writes = append(writes, [3]string{service, key, value})
			return nil
		},
	}
	s := CollectedSecret{Name: "charly-app-api-key", SecretName: "API_KEY", Env: "API_KEY",
		Service: "charly/api-key", Key: "qdrant"}

	provisioned, _, err := ProvisionPodmanSecrets("podman", "app", "", []CollectedSecret{s}, true, "", cred)
	if err != nil {
		t.Fatalf("ProvisionPodmanSecrets: %v", err)
	}
	if len(provisioned) != 1 {
		t.Fatalf("provisioned %d secrets, want 1", len(provisioned))
	}
	if len(writes) != 1 {
		t.Fatalf("wrote the credential %d times, want 1: %v", len(writes), writes)
	}
	if writes[0][0] != "charly/api-key" || writes[0][1] != "qdrant" {
		t.Errorf("auto-generated value written to (%q, %q), want (charly/api-key, qdrant) — the coordinate the resolver reads",
			writes[0][0], writes[0][1])
	}
	if writes[0][2] == "" {
		t.Error("auto-generated value is empty")
	}
}

// TestProvisionPodmanSecrets_AutoGenKeepsTheDefaultWithoutAnOverride guards against over-reach:
// a candy that declares no `key:` must still land at the historical default.
func TestProvisionPodmanSecrets_AutoGenKeepsTheDefaultWithoutAnOverride(t *testing.T) {
	fakeEngine(t)
	var writes [][3]string
	cred := CredentialAccess{
		Resolve: func(_, _, _, _ string) (string, string) { return "", "" },
		Write: func(service, key, value string) error {
			writes = append(writes, [3]string{service, key, value})
			return nil
		},
	}
	s := CollectedSecret{Name: "charly-app-token", SecretName: "TOKEN", Env: "TOKEN"}

	if _, _, err := ProvisionPodmanSecrets("podman", "app", "", []CollectedSecret{s}, true, "", cred); err != nil {
		t.Fatalf("ProvisionPodmanSecrets: %v", err)
	}
	if len(writes) != 1 || writes[0][0] != "charly/secret" || writes[0][1] != "charly-app-token" {
		t.Errorf("writes = %v, want one at (charly/secret, charly-app-token)", writes)
	}
}

// TestProvisionPodmanSecrets_InteractiveWritesToTheOverride covers the second changed write site:
// the interactive prompt's persist (the branch a TTY would take) must persist at the coordinate
// the resolver reads, exactly like auto-generation.
func TestProvisionPodmanSecrets_InteractiveWritesToTheOverride(t *testing.T) {
	fakeEngine(t)
	origTerm, origPrompt := stdinIsTerminal, promptPassword
	t.Cleanup(func() { stdinIsTerminal, promptPassword = origTerm, origPrompt })
	stdinIsTerminal = func() bool { return true }
	promptPassword = func(string) (string, error) { return "entered-by-operator", nil }

	var writes [][3]string
	cred := CredentialAccess{
		Resolve: func(_, _, _, _ string) (string, string) { return "", "" },
		Write: func(service, key, value string) error {
			writes = append(writes, [3]string{service, key, value})
			return nil
		},
	}
	s := CollectedSecret{Name: "charly-app-api-key", SecretName: "API_KEY", Env: "API_KEY",
		Service: "charly/api-key", Key: "qdrant"}

	if _, _, err := ProvisionPodmanSecrets("podman", "app", "", []CollectedSecret{s}, false, "", cred); err != nil {
		t.Fatalf("ProvisionPodmanSecrets: %v", err)
	}
	if len(writes) != 1 || writes[0][0] != "charly/api-key" || writes[0][1] != "qdrant" || writes[0][2] != "entered-by-operator" {
		t.Fatalf("interactive persist wrote %v, want one write of the entered value at (charly/api-key, qdrant)", writes)
	}
}

// TestProvisionPodmanSecrets_MissingValueHintNamesTheOverride covers the third changed site: the
// operator-facing hint is part of this fix's promise — it must name the coordinate the resolver
// will read, not the hardcoded default.
func TestProvisionPodmanSecrets_MissingValueHintNamesTheOverride(t *testing.T) {
	fakeEngine(t)
	origTerm, origPrompt := stdinIsTerminal, promptPassword
	t.Cleanup(func() { stdinIsTerminal, promptPassword = origTerm, origPrompt })
	stdinIsTerminal = func() bool { return false } // no TTY, no auto-generate: the hint prints

	cred := CredentialAccess{
		Resolve: func(_, _, _, _ string) (string, string) { return "", "" },
		Write:   func(_, _, _ string) error { return nil },
	}
	s := CollectedSecret{Name: "charly-app-api-key", SecretName: "API_KEY", Env: "API_KEY",
		Service: "charly/api-key", Key: "qdrant"}

	// Capture stderr around the call.
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	_, _, _ = ProvisionPodmanSecrets("podman", "app", "", []CollectedSecret{s}, false, "", cred)
	_ = w.Close()
	os.Stderr = origStderr
	captured, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading captured stderr: %v", err)
	}
	_ = r.Close()
	out := string(captured)
	if !strings.Contains(out, "charly secrets set charly/api-key qdrant") {
		t.Errorf("the hint does not name the declared coordinate; got:\n%s", out)
	}
	if strings.Contains(out, "charly secrets set charly/secret charly-app-api-key") {
		t.Errorf("the hint still names the hardcoded default; got:\n%s", out)
	}
}
