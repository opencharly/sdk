package deploykit

import (
	"sync"
	"testing"

	"github.com/opencharly/spec/spec"
)

// secret_candy_resolve_test.go — ported from charly/layer_secrets_test.go (#118,
// coneB-p8bremainder): EnsureCandySecret/ResolveCandySecret/ResolveSecretForCandy moved here, so
// their coverage moves with them. Uses a simple in-memory fake CredentialAccess (a map-backed
// Resolve/Write pair) instead of charly-core's DefaultCredentialStore-swapping test harness — the
// injected-dependency shape means no package-level var to override at all. This is also the
// FIRST direct test coverage of GenerateAndStoreSecret's CredentialAccess injection actually
// working end-to-end (secret_provision_test.go only covered the pure EnvVarNameToPodmanSecretSlug
// helper before this move).

// fakeCredStore is a minimal in-memory backing store for a fake CredentialAccess: Resolve
// returns the stored value (source "config") or ("", "default") on a miss (envVar/defaultVal are
// ignored — the env-var-override chain is charly-core's ResolveCredential's own concern, tested
// there; this fake only needs to prove EnsureCandySecret/ResolveCandySecret's OWN routing +
// auto-gen + idempotency logic).
type fakeCredStore struct {
	mu sync.Mutex
	m  map[string]string // "service/key" -> value
}

func newFakeCredAccess() (CredentialAccess, *fakeCredStore) {
	store := &fakeCredStore{m: map[string]string{}}
	return CredentialAccess{
		Resolve: func(envVar, service, key, defaultVal string) (string, string) {
			store.mu.Lock()
			defer store.mu.Unlock()
			if v, ok := store.m[service+"/"+key]; ok {
				return v, "config"
			}
			return defaultVal, "default"
		},
		Write: func(service, key, value string) error {
			store.mu.Lock()
			defer store.mu.Unlock()
			store.m[service+"/"+key] = value
			return nil
		},
	}, store
}

func (f *fakeCredStore) get(service, key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.m[service+"/"+key]
}

func (f *fakeCredStore) set(service, key, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.m[service+"/"+key] = value
}

// fakeCandyReader is the minimal spec.CandyReader stub EnsureCandySecret/ResolveCandySecret
// need — only SecretRequire/SecretAccept are read.
type fakeCandyReader struct {
	spec.CandyReader
	require []spec.EnvDependency
	accept  []spec.EnvDependency
}

func (f fakeCandyReader) SecretRequire() []spec.EnvDependency { return f.require }
func (f fakeCandyReader) SecretAccept() []spec.EnvDependency  { return f.accept }

// TestEnsureCandySecret_PresentInStore verifies that a value already stored at (service, key)
// is returned as-is — no auto-generation, no rewrite.
func TestEnsureCandySecret_PresentInStore(t *testing.T) {
	cred, store := newFakeCredAccess()
	store.set("charly/secret", "EXISTING_TOKEN", "preset-value")

	dep := spec.EnvDependency{Name: "EXISTING_TOKEN"}
	val, source := EnsureCandySecret(dep, true, cred)

	if val != "preset-value" {
		t.Errorf("expected preset-value, got %q", val)
	}
	if source == "auto-generated" {
		t.Errorf("expected source != auto-generated; got %q (regression: pre-existing values must NOT regenerate)", source)
	}
}

// TestEnsureCandySecret_RequiredMissingAutoGenerates is the primary requested behavior: missing +
// required → 32-byte hex, persisted to the active store.
func TestEnsureCandySecret_RequiredMissingAutoGenerates(t *testing.T) {
	cred, store := newFakeCredAccess()

	dep := spec.EnvDependency{Name: "K3S_CLUSTER_TOKEN"}
	val, source := EnsureCandySecret(dep, true, cred)

	if source != "auto-generated" {
		t.Errorf("expected source=auto-generated, got %q", source)
	}
	// 32 bytes url-safe base64 = 44 chars (Fernet-key compatible). See
	// GenerateRandomSecretToken for rationale.
	if len(val) != 44 {
		t.Errorf("expected 44-char url-safe base64 token, got %d chars: %q", len(val), val)
	}
	for _, c := range val {
		isURLSafeB64 := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') || c == '-' || c == '_' || c == '='
		if !isURLSafeB64 {
			t.Errorf("expected url-safe base64, found invalid char %q in %q", c, val)
			break
		}
	}

	// Persistence: the value must be retrievable via the same store.
	if stored := store.get("charly/secret", "K3S_CLUSTER_TOKEN"); stored != val {
		t.Errorf("persistence mismatch: returned %q, store has %q", val, stored)
	}
}

// TestEnsureCandySecret_IdempotentAcrossCalls verifies the race-free invariant: a second
// resolver call (e.g., k3s-agent reading the token after k3s-server's first-call auto-gen)
// returns the SAME value, not a fresh regeneration.
func TestEnsureCandySecret_IdempotentAcrossCalls(t *testing.T) {
	cred, _ := newFakeCredAccess()

	dep := spec.EnvDependency{Name: "SHARED_TOKEN"}

	// First call — auto-generates + persists.
	val1, source1 := EnsureCandySecret(dep, true, cred)
	if source1 != "auto-generated" {
		t.Fatalf("first call expected auto-generated, got %q", source1)
	}

	// Second call — must read persisted value.
	val2, source2 := EnsureCandySecret(dep, true, cred)
	if val1 != val2 {
		t.Errorf("idempotency broken: first=%q, second=%q (regression: server+agent would mismatch)", val1, val2)
	}
	if source2 == "auto-generated" {
		t.Errorf("second call regenerated instead of reading persisted (source=%q)", source2)
	}
}

// TestEnsureCandySecret_OptionalMissingReturnsEmpty verifies that non-required deps
// (secret_accepts) do NOT auto-generate when missing. Caller is responsible for falling back to
// dep.Default.
func TestEnsureCandySecret_OptionalMissingReturnsEmpty(t *testing.T) {
	cred, store := newFakeCredAccess()

	dep := spec.EnvDependency{Name: "OPTIONAL_KEY"}
	val, source := EnsureCandySecret(dep, false, cred)

	if val != "" {
		t.Errorf("expected empty value for optional+missing, got %q", val)
	}
	if source == "auto-generated" {
		t.Errorf("optional missing must NOT auto-generate; got source=%q", source)
	}

	// Confirm nothing was written to the store either.
	if stored := store.get("charly/secret", "OPTIONAL_KEY"); stored != "" {
		t.Errorf("optional missing leaked %q to store", stored)
	}
}

// TestEnsureCandySecret_CustomKeyRoutesToOverride verifies that the `key:` override on
// EnvDependency (e.g., `key: charly/api-key/openrouter`) routes the lookup AND the auto-gen
// persistence to the override service/key pair, not the default charly/secret/<name>.
func TestEnsureCandySecret_CustomKeyRoutesToOverride(t *testing.T) {
	cred, store := newFakeCredAccess()

	dep := spec.EnvDependency{
		Name: "MY_VAR_NAME",
		Key:  "charly/api-key/openrouter",
	}

	val, source := EnsureCandySecret(dep, true, cred)
	if source != "auto-generated" {
		t.Fatalf("expected auto-generated, got %q", source)
	}

	// The auto-gen MUST persist at the override location, not at the default.
	if atOverride := store.get("charly/api-key", "openrouter"); atOverride != val {
		t.Errorf("expected persistence at override (charly/api-key, openrouter), got %q (val=%q)", atOverride, val)
	}
	if atDefault := store.get("charly/secret", "MY_VAR_NAME"); atDefault != "" {
		t.Errorf("default location should be empty, got %q (key override leaked)", atDefault)
	}
}

// TestResolveCandySecrets_RequiredAutoGen exercises the wrapper that the deploy-add path
// actually calls: a candy with secret_requires must always resolve (auto-gen guarantees
// non-empty values).
func TestResolveCandySecrets_RequiredAutoGen(t *testing.T) {
	cred, _ := newFakeCredAccess()

	layer := fakeCandyReader{require: []spec.EnvDependency{{Name: "K3S_CLUSTER_TOKEN"}}}
	env := ResolveCandySecret(layer, cred)
	val, ok := env["K3S_CLUSTER_TOKEN"]
	if !ok || val == "" {
		t.Fatalf("expected K3S_CLUSTER_TOKEN to be resolved (auto-gen), got env=%v", env)
	}
	if len(val) != 44 {
		t.Errorf("expected 44-char url-safe base64 token (Fernet-compatible), got %d chars", len(val))
	}
}

// TestResolveCandySecrets_OptionalDefaultFallback exercises the secret_accepts path with a
// Default value: missing + optional → dep.Default goes into env (not auto-gen).
func TestResolveCandySecrets_OptionalDefaultFallback(t *testing.T) {
	cred, _ := newFakeCredAccess()

	layer := fakeCandyReader{accept: []spec.EnvDependency{{Name: "OPTIONAL_VAR", Default: "fallback-value"}}}
	env := ResolveCandySecret(layer, cred)
	if env["OPTIONAL_VAR"] != "fallback-value" {
		t.Errorf("expected fallback-value, got %q", env["OPTIONAL_VAR"])
	}
}

// TestResolveSecretsForCandies_TwoCandiesSameSecret verifies the race-free invariant at the
// wrapper level: two candies (think k3s-server + k3s-agent) declaring the same secret_requires
// resolve to the SAME value.
func TestResolveSecretsForCandies_TwoCandiesSameSecret(t *testing.T) {
	cred, store := newFakeCredAccess()

	server := fakeCandyReader{require: []spec.EnvDependency{{Name: "K3S_CLUSTER_TOKEN"}}}
	agent := fakeCandyReader{require: []spec.EnvDependency{{Name: "K3S_CLUSTER_TOKEN"}}}
	env := ResolveSecretForCandy([]spec.CandyReader{server, agent}, cred)

	val := env["K3S_CLUSTER_TOKEN"]
	if val == "" || len(val) != 44 {
		t.Fatalf("expected 44-char url-safe base64 token (Fernet-compatible), got %q", val)
	}
	// And the persisted store must have exactly that value.
	if stored := store.get("charly/secret", "K3S_CLUSTER_TOKEN"); stored != val {
		t.Errorf("server+agent token mismatch: env=%q stored=%q", val, stored)
	}
}
