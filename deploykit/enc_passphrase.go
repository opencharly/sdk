package deploykit

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// enc_passphrase.go — the gocryptfs PASSPHRASE-RESOLUTION orchestration relocated from
// charly/enc.go (Cutover B unit 6b, the InvokeProvider-generalization family, alongside
// secret_provision.go). This was the last enc.go family blocked on a credential-store
// dependency — it now takes that dependency as an injected CredentialAccess (the SAME type
// secret_provision.go defines) instead of calling ResolveCredential/DefaultCredentialStore
// by name, so charly-core and a future out-of-process caller share ONE implementation (R3).
//
// The verb:enc OpExecute dispatch is now fully plugin-owned (candy/plugin-deploy-pod +
// candy/plugin-pod drive verb:enc via their own InvokeProvider; the former core encExecViaPlugin
// shim + the pod-config-enc-mounts seam are RETIRED). awaitKeyringUnlockViaPlugin needs the
// CONCRETE core CredentialStore's awaitUnlock/credentialAwaiter — it is threaded into
// ResolveEncPassphraseForMount below as an injected `waiter` closure, exactly like the
// pre-existing resolver/reset closures, so this package needs no knowledge of its shape
// beyond the func signature it already declared).

// ResolveEncPassphrase resolves the gocryptfs passphrase for an image. Resolution order:
// GOCRYPTFS_PASSWORD env var → credential store (keyring/config) → auto-generate or
// interactive prompt.
func ResolveEncPassphrase(boxName string, autoGenerate bool, cred CredentialAccess) (string, error) {
	// 1. Test/CI override
	if pw := os.Getenv("GOCRYPTFS_PASSWORD"); pw != "" {
		return pw, nil
	}
	// 2. Credential store (keyring / config)
	if val, _ := cred.Resolve("", "charly/enc", boxName, ""); val != "" {
		return val, nil
	}
	// 3. Auto-generate if requested
	if autoGenerate {
		generated := GenerateRandomSecretToken(32)
		if err := cred.Write("charly/enc", boxName, generated); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not persist enc passphrase for %s: %v\n", boxName, err)
		}
		fmt.Fprintf(os.Stderr, "Generated encryption passphrase for %s\n", boxName)
		return generated, nil
	}
	// 4. Interactive prompt
	return AskPassword("charly-"+boxName, "Passphrase for charly-"+boxName+":")
}

// EncPassphraseUnattended reports whether a box's gocryptfs passphrase can be obtained with
// no human running a command — the single capability the quadlet emitters consume to decide
// whether a unit may be enabled at boot. It asks a property, never a backend name: a backend
// added tomorrow qualifies on the same merit, and this package never learns its name.
//
// It asks the resolver EXACTLY the question the boot path will ask. ResolveEncPassphraseForMount
// resolves `charly/enc/<box>` with an empty envVar under systemd, so the env override is not
// consulted there and must not be consulted here either — a mismatch would promise autostart on
// a value the unit cannot see.
//
//   - a non-empty value            → the passphrase is in hand right now; nothing to wait for.
//   - source "locked"              → a Secret Service is present and holds the collection. It
//     unlocks when the operator logs in, with no charly command
//     involved, and ResolveEncPassphraseForMount waits for exactly
//     that. This is the canonical Secret Service autostart path.
//   - source "default"             → the passphrase is stored nowhere. Enabling the unit would
//     fail at every boot, forever.
//   - source "unavailable"         → the backend could not be probed, so the capability cannot
//     be established. Refuse rather than enable a unit that may
//     never start.
func EncPassphraseUnattended(boxName string, cred CredentialAccess) bool {
	value, source := cred.Resolve("", "charly/enc", boxName, "")
	return value != "" || source == "locked"
}

// EncMountDeadline bounds how long ResolveEncPassphraseForMount will retry transient
// failures (source="unavailable") before giving up. source="locked" does NOT use this — it
// uses event-driven DBus signal waiting with no deadline (see the caller-supplied waiter).
var EncMountDeadline = 2 * time.Minute

// EncMountPollPeriod is the interval between retry attempts for source="unavailable" only.
var EncMountPollPeriod = 5 * time.Second

// ResolveEncPassphraseForMount resolves the gocryptfs passphrase with backend-aware and
// failure-aware retry behavior.
//
// Under systemd (INVOCATION_ID set) with a keyring-capable backend:
//   - If the store is temporarily locked ("locked") or unreachable ("unavailable"), retry
//     every EncMountPollPeriod until EncMountDeadline elapses, then fail with a clear
//     diagnostic.
//   - If the store answered and the credential is NOT stored ("default"), fail immediately
//     with an actionable error — no amount of polling will conjure a credential that was
//     never stored.
//
// Every supported backend waits: `config` was removed as a selectable secret_backend, so the
// resolved backend is always "", "auto" or "keyring", and there is no fail-fast-without-polling
// second path to fork on.
//
// Interactive callers fall back to ResolveEncPassphrase which can prompt.
//
// backend/reset/waiter are supplied by the caller: charly-core passes its
// resolveSecretBackend()/resetDefaultCredentialStore/awaitKeyringUnlockViaPlugin (the
// keyring-unlock wait RPCs verb:credential `await-unlock` out-of-process in
// candy/plugin-secrets); a future plugin caller supplies its own InvokeProvider-backed
// equivalents. waiter may be nil (falls through to the bounded retry).
func ResolveEncPassphraseForMount(boxName, backend string, cred CredentialAccess, reset func(), waiter func(ctx context.Context, boxName string, resolver func() (string, string), reset func()) (string, string, error)) (string, error) {
	if os.Getenv("INVOCATION_ID") == "" {
		return ResolveEncPassphrase(boxName, false, cred)
	}
	resolver := func() (string, string) {
		return cred.Resolve("", "charly/enc", boxName, "")
	}
	return ResolveEncPassphraseForMountWithResolver(boxName, backend, resolver, reset, waiter)
}

// ResolveEncPassphraseForMountWithResolver is the testable core of
// ResolveEncPassphraseForMount. It accepts a resolver closure, a reset closure, and a waiter
// closure so tests can supply mock implementations without touching global state,
// environment variables, or DBus.
func ResolveEncPassphraseForMountWithResolver(
	boxName, backend string,
	resolver func() (value, source string),
	reset func(),
	waiter func(ctx context.Context, boxName string, resolver func() (string, string), reset func()) (string, string, error),
) (string, error) {
	// Initial probe.
	val, src := resolver()
	if val != "" {
		return val, nil
	}

	// source="default" is terminal — credential is not stored anywhere.
	if src == "default" {
		return "", EncNotStoredError(boxName, backend, src)
	}

	// source="locked" — keyring present but locked. Wait indefinitely via DBus signal
	// subscription (zero CPU cost between events).
	if src == "locked" && waiter != nil {
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
		v, src2, err := waiter(ctx, boxName, resolver, reset)
		if err != nil {
			return "", fmt.Errorf("waiting for keyring unlock interrupted: %w", err)
		}
		if v != "" {
			return v, nil
		}
		return "", EncNotStoredError(boxName, backend, src2)
	}

	// source="unavailable" — transient backend probe failure. Bounded poll.
	return RetryUnavailable(boxName, backend, resolver, reset)
}

// EncNotStoredError formats the terminal "credential not stored" error with actionable
// remediation hints.
func EncNotStoredError(boxName, backend, src string) error {
	return fmt.Errorf(
		"encryption passphrase not available for charly/enc/%s "+
			"(backend=%s, source=%s). "+
			"Remediation: run `charly doctor` to check keyring health, "+
			"or store it with `charly secrets set charly/enc %s`",
		boxName, backend, src, boxName)
}

// RetryUnavailable polls the resolver with a bounded deadline for transient backend-probe
// failures (source="unavailable").
func RetryUnavailable(
	boxName, backend string,
	resolver func() (string, string),
	reset func(),
) (string, error) {
	deadline := time.Now().Add(EncMountDeadline)
	attempt := 0
	maxAttempts := max(int(EncMountDeadline/EncMountPollPeriod), 1)
	for {
		attempt++
		val, src := resolver()
		if val != "" {
			return val, nil
		}
		retryable := src == "locked" || src == "unavailable"
		if !retryable || !time.Now().Before(deadline) {
			return "", fmt.Errorf(
				"encryption passphrase not available for charly/enc/%s after %d attempt(s) "+
					"(backend=%s, source=%s, waited up to %v). "+
					"Remediation: run `charly doctor` to check keyring health, "+
					"or store it with `charly secrets set charly/enc %s`",
				boxName, attempt, backend, src, EncMountDeadline, boxName)
		}
		fmt.Fprintf(os.Stderr,
			"charly: waiting for credential store (charly-enc/%s, source=%s, attempt %d/%d)...\n",
			boxName, src, attempt, maxAttempts)
		time.Sleep(EncMountPollPeriod)
		if reset != nil {
			reset()
		}
	}
}
