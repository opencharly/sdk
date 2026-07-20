package deploykit

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
	"golang.org/x/term"
)

// secret_probe.go — the podman-secret + credential-key leaf helpers relocated from
// charly/secrets.go (Cutover B unit 2). These are genuinely portable (no provider-registry or
// credential-store coupling — pure token generation, exec.Command wrappers over `podman secret`,
// and slice/map bookkeeping), so they move here rather than staying behind a HostBuild seam. What
// does NOT move (charly/secrets.go, STILL core): ProvisionPodmanSecrets/resolveSecretValue/
// CollectCandySecretAccepts/resolveHookSecretEnv/generateAndStoreSecret — each calls
// ResolveCredential/DefaultCredentialStore directly or transitively (the core provider registry),
// registered FINAL/K5 inventory alongside enc.go's credential family. Those core functions now
// call this package's leaves (deploykit.PodmanSecretExists, deploykit.PromptPassword, etc.)
// instead of their own former in-file copies.

// GenerateRandomSecretToken returns `byteCount` random bytes encoded as
// url-safe base64 (RFC 4648 §5). For byteCount=32 this produces a 44-char
// string (43 base64 chars + 1 `=` pad).
//
// Url-safe base64 was chosen over hex because it is a strict superset of
// what every secret consumer in the codebase needs:
//   - Postgres / VNC / generic passwords: accept any string. Base64 has
//     more entropy per character (6 bits vs 4) so 32 random bytes pack
//     into 44 chars instead of hex's 64 chars.
//   - Apache Airflow's AIRFLOW__CORE__FERNET_KEY: REQUIRES url-safe
//     base64 of exactly 32 bytes (cryptography.fernet.Fernet's documented
//     format). Hex strings are rejected with `binascii.Error: Invalid
//     base64-encoded string`.
//   - gocryptfs / Podman / KeePassXC: accept any string; format-agnostic.
//
// Url-safe (vs standard base64) avoids `+` and `/` characters that would
// need shell-escaping in `[Service] Environment=...` quadlet lines and
// in `--password` CLI args. The `=` padding is benign in every consumer.
func GenerateRandomSecretToken(byteCount int) string {
	b := make([]byte, byteCount)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return base64.URLEncoding.EncodeToString(b)
}

// PromptPassword reads a password from the terminal without echo — the interactive
// secret-entry path in ProvisionPodmanSecrets (a `charly config`-time operator prompt).
func PromptPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	pw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}
	return string(pw), nil
}

// ListProvisionedSecretNames returns the engine-side podman secrets
// provisioned for a box (the charly-<box>-* names, sidecar secrets
// included), sorted — the charly-native replacement for ad-hoc
// `podman secret ls` verification (surfaced on `charly status <box>` detail).
func ListProvisionedSecretNames(engineBin, boxName string) []string {
	out, err := exec.Command(engineBin, "secret", "ls", "--format", "{{.Name}}").Output()
	if err != nil {
		return nil
	}
	prefix := "charly-" + boxName + "-"
	var names []string
	for n := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if n != "" && strings.HasPrefix(n, prefix) {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// ApplySecretRefresh marks the named secrets (matched by their manifest
// SecretName; the literal "all" matches every secret) RotateOnConfig for
// THIS provisioning run, so ProvisionPodmanSecrets removes and recreates
// their podman secrets — the charly-native replacement for the retired
// ad-hoc `podman secret rm` re-provisioning path. Returns the requested
// names that matched nothing so the caller can surface typos. NOTE: a
// candy-owned auto-generated secret gets a NEW random value on refresh;
// services that persisted the old value (an initialized database) must be
// re-initialized by the operator.
func ApplySecretRefresh(secrets []CollectedSecret, refresh []string) ([]CollectedSecret, []string) {
	if len(refresh) == 0 {
		return secrets, nil
	}
	all := false
	hit := map[string]bool{}
	for _, r := range refresh {
		if r == "all" {
			all = true
			continue
		}
		hit[r] = false
	}
	for i := range secrets {
		name := secrets[i].SecretName
		if _, requested := hit[name]; all || requested {
			secrets[i].RotateOnConfig = true
			if _, requested := hit[name]; requested {
				hit[name] = true
			}
		}
	}
	var unmatched []string
	for name, matched := range hit {
		if !matched {
			unmatched = append(unmatched, name)
		}
	}
	sort.Strings(unmatched)
	return secrets, unmatched
}

// CollectSecretsFromLabels reconstructs secrets from image label metadata.
func CollectSecretsFromLabels(boxName string, labelSecrets []spec.LabelSecretEntry) []CollectedSecret {
	secrets := make([]CollectedSecret, 0, len(labelSecrets))
	for _, ls := range labelSecrets {
		secrets = append(secrets, CollectedSecret{
			Name:       "charly-" + boxName + "-" + ls.Name,
			Target:     ls.Target,
			Env:        ls.Env,
			SecretName: ls.Name,
		})
	}
	return secrets
}

// SecretArgs returns --secret flags for container run (direct mode).
func SecretArgs(secrets []CollectedSecret) []string {
	args := make([]string, 0, 2*len(secrets))
	for _, s := range secrets {
		args = append(args, "--secret", fmt.Sprintf("%s,target=%s", s.Name, s.Target))
	}
	return args
}

// CredServiceForSecret maps well-known env vars to credential services.
func CredServiceForSecret(envVar, credServiceVNC string) string {
	switch envVar {
	case "VNC_PASSWORD":
		return credServiceVNC
	default:
		return "charly/secret"
	}
}

// CredKeyForSecret returns the credential key for an image/instance pair.
func CredKeyForSecret(boxName, instance string) string {
	if instance != "" {
		return boxName + "-" + instance
	}
	return boxName
}

// PodmanSecretExists checks whether a podman secret with the given name already exists.
func PodmanSecretExists(engine, name string) bool {
	binary := kit.EngineBinary(engine)
	cmd := exec.Command(binary, "secret", "inspect", name)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

// EnsurePodmanSecret creates or replaces a podman secret.
func EnsurePodmanSecret(engine, name, value string) error {
	binary := kit.EngineBinary(engine)
	// Remove existing secret (ignore error if doesn't exist)
	rmCmd := exec.Command(binary, "secret", "rm", name)
	rmCmd.Stderr = nil
	_ = rmCmd.Run()

	// Create new secret from stdin
	createCmd := exec.Command(binary, "secret", "create", name, "-")
	createCmd.Stdin = strings.NewReader(value)
	if output, err := createCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("podman secret create %s: %w\n%s", name, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// RemovePodmanSecrets removes podman secrets for an image (best-effort).
func RemovePodmanSecrets(engine string, secrets []CollectedSecret) {
	binary := kit.EngineBinary(engine)
	for _, s := range secrets {
		cmd := exec.Command(binary, "secret", "rm", s.Name)
		cmd.Stderr = nil
		_ = cmd.Run()
	}
}
