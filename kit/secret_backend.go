package kit

import (
	"fmt"
	"os"
	"sync"
)

// secret_backend.go — the ONE place that knows the secret_backend vocabulary.
//
// `config` (pin every credential to plaintext ~/.config/charly/config.yml) was removed as a
// selectable backend: the supported set is the system keyring reached through Secret Service,
// plus the env-var and GPG-agent paths that never consult this setting at all.
//
// Removing it from the SETTER alone would not remove it from service — the value also arrives
// from a config file written before the removal and from CHARLY_SECRET_BACKEND. Both readers
// (candy/plugin-secrets, which selects the store, and candy/plugin-pod, which reports the
// backend in enc-mount diagnostics) funnel their raw value through NormalizeSecretBackend, so
// the removed value cannot reach either. That is what lets sdk/deploykit's mount resolver drop
// its non-waiting branch: every value this returns waits.

// SecretBackendAuto probes the system keyring and falls back to the config file when no keyring
// is available. It is the default and the value a removed backend normalizes to.
const SecretBackendAuto = "auto"

// SecretBackendKeyring pins credential resolution to the system keyring (Secret Service).
const SecretBackendKeyring = "keyring"

var removedSecretBackendOnce sync.Once

// NormalizeSecretBackend maps a raw secret_backend value — from CHARLY_SECRET_BACKEND or from
// ~/.config/charly/config.yml — onto the supported set, returning SecretBackendAuto for an
// empty, removed, or unrecognized value.
//
// Coercion rather than rejection is deliberate: a host carrying `secret_backend: config` keeps
// working, and keeps resolving the credentials it already has. `auto` still reads the config
// file when no keyring is present, so nothing becomes unreachable — the operator loses only the
// ability to PIN charly to the plaintext store, which is the point of the removal. The notice
// is printed once per process so a scripted run does not stutter.
func NormalizeSecretBackend(raw string) string {
	switch raw {
	case "":
		return SecretBackendAuto
	case SecretBackendAuto, SecretBackendKeyring:
		return raw
	}
	removedSecretBackendOnce.Do(func() {
		fmt.Fprintf(os.Stderr,
			"charly: secret_backend %q is not a supported backend; using %q instead.\n"+
				"  Supported: %q (keyring, falling back to the config file) or %q (keyring only).\n"+
				"  Silence this by running: charly settings set secret_backend %s\n",
			raw, SecretBackendAuto, SecretBackendAuto, SecretBackendKeyring, SecretBackendAuto)
	})
	return SecretBackendAuto
}
