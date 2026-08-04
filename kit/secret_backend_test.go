package kit

import "testing"

// TestNormalizeSecretBackend pins the vocabulary the whole credential layer resolves against.
// The `config` row is the load-bearing one: sdk/deploykit's mount resolver dropped its
// non-waiting branch on the strength of this function never returning a non-waiting value, so a
// regression here silently restores a backend that path can no longer serve.
func TestNormalizeSecretBackend(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"unset falls back to auto", "", SecretBackendAuto},
		{"auto is supported", SecretBackendAuto, SecretBackendAuto},
		{"keyring is supported", SecretBackendKeyring, SecretBackendKeyring},
		{"the removed config backend coerces to auto", "config", SecretBackendAuto},
		{"an unrecognized value coerces to auto", "kdbx", SecretBackendAuto},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeSecretBackend(tc.raw); got != tc.want {
				t.Errorf("NormalizeSecretBackend(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestNormalizeSecretBackendNeverReturnsANonWaitingBackend states the invariant deploykit
// depends on directly, so the coupling is visible at the point it is relied upon rather than
// only in a comment. Every value this function can return must be one the enc-mount resolver
// treats as a waiting backend.
func TestNormalizeSecretBackendNeverReturnsANonWaitingBackend(t *testing.T) {
	for _, raw := range []string{"", "auto", "keyring", "config", "kdbx", "KEYRING", "file"} {
		got := NormalizeSecretBackend(raw)
		if got != SecretBackendAuto && got != SecretBackendKeyring {
			t.Errorf("NormalizeSecretBackend(%q) = %q, which is outside the supported set", raw, got)
		}
	}
}
