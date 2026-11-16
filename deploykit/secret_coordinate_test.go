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
	"os"
	"path/filepath"
	"testing"
)

// TestSecretCoordinate pins the rule itself.
func TestSecretCoordinate(t *testing.T) {
	withOverride := CollectedSecret{Name: "charly-app-api-key", SecretName: "API_KEY", Service: "charly/api-key", Key: "qdrant"}
	if svc, key := secretCoordinate(withOverride); svc != "charly/api-key" || key != "qdrant" {
		t.Errorf("secretCoordinate(with override) = (%q, %q), want (charly/api-key, qdrant)", svc, key)
	}
	def := CollectedSecret{Name: "charly-app-api-key", SecretName: "API_KEY"}
	if svc, key := secretCoordinate(def); svc != "charly/secret" || key != "charly-app-api-key" {
		t.Errorf("secretCoordinate(default) = (%q, %q), want (charly/secret, charly-app-api-key)", svc, key)
	}
	// A half-set override is not an override: both fields must be present (same rule the
	// resolver applies).
	half := CollectedSecret{Name: "charly-app-api-key", Service: "charly/api-key"}
	if svc, key := secretCoordinate(half); svc != "charly/secret" || key != "charly-app-api-key" {
		t.Errorf("secretCoordinate(half override) = (%q, %q), want the default", svc, key)
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
