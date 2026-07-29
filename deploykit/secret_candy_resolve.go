package deploykit

import (
	"maps"
	"strings"

	"github.com/opencharly/spec/spec"
)

// secret_candy_resolve.go — the candy `secret_requires:`/`secret_accepts:` resolver for
// host/vm/ssh install-plan deploy targets (relocated from charly/layer_secrets.go, #118
// coneB-p8bremainder): the ONE genuinely core-coupled part of that file, CandyForPlan (needs
// ScanAllCandyWithConfig + *Config, both core-only), stays behind — layer_secrets.go shrinks to
// just that. EnsureCandySecret/ResolveCandySecret/ResolveSecretForCandy had NO core-only
// dependency beyond the credential-store access GenerateAndStoreSecret (above, in
// secret_provision.go) already takes as an INJECTED CredentialAccess — so this move is the SAME
// R3 pattern that file's own doc comment names ensureCandySecret as anticipating ("calls this
// directly, supplying its own CredentialAccess").
//
// Container targets have their own path (ProvisionPodmanSecrets) that mounts secrets as podman
// secrets / env at container-run time, running AFTER build with no candy-task env injection.
// For install-plan-based targets, the candy's tasks run directly on the deploy target, so the
// credential-store value must be resolved on the operator side and passed through as env on the
// step — that's what this file's functions do.
//
// Resolution policy: secret_requires: entries auto-generate a 32-byte hex token via
// GenerateAndStoreSecret when missing everywhere (env + store). secret_accepts: entries fall
// back to dep.Default when missing, never auto-generate. The auto-generation is race-free across
// multiple candies declaring the same secret because the injected CredentialAccess's backing
// store is process-shared and the first caller's Write is visible to the second caller's Resolve.

// EnsureCandySecret resolves a secret_requires/secret_accepts EnvDependency against the
// credential store (via cred). For required deps that miss everywhere (env, store), generates a
// 32-byte hex token, persists via cred.Write, and returns the new value. For optional deps that
// miss, returns "" with the source classification from cred.Resolve so the caller can fall back
// to dep.Default if set.
//
// The Key field on an EnvDependency follows the format "<service>/<key>" and must start with
// "charly/" (enforced at load time by charly's validate.go). When Key is empty, the default
// lookup is service="charly/secret", key=Name.
//
// Race-free across multiple candies declaring the same secret: the first caller's cred.Write
// lands in the active backend; the second caller's cred.Resolve reads the persisted value.
func EnsureCandySecret(dep spec.EnvDependency, required bool, cred CredentialAccess) (val, source string) {
	service, key := "charly/secret", dep.Name
	if dep.Key != "" {
		if idx := strings.LastIndex(dep.Key, "/"); idx > 0 {
			service = dep.Key[:idx]
			key = dep.Key[idx+1:]
		}
	}
	// Pass dep.Name as envVar so an operator can override the persisted value via
	// `export K3S_CLUSTER_TOKEN=…` before invoking deploy (matches the cred.Resolve pattern
	// used elsewhere in this package).
	val, source = cred.Resolve(dep.Name, service, key, "")
	if val != "" {
		return val, source
	}
	if !required {
		return "", source
	}
	return GenerateAndStoreSecret(service, key, cred)
}

// ResolveCandySecret walks the candy's secret_requires + secret_accepts and resolves each
// against the credential store (via cred). Required entries that miss everywhere auto-generate a
// 32-byte hex token (see EnsureCandySecret). Optional secret_accepts: entries that miss fall back
// to dep.Default.
//
// Returns the env map; never returns an error. The auto-generate policy guarantees every
// secret_requires: resolves to a non-empty value. Takes spec.CandyReader (the read-only
// interface every scanned candy is wrapped into) rather than a concrete type — this function
// needs only the SecretRequire/SecretAccept accessors.
func ResolveCandySecret(layer spec.CandyReader, cred CredentialAccess) map[string]string {
	env := map[string]string{}
	if layer == nil {
		return env
	}

	for _, dep := range layer.SecretRequire() {
		val, _ := EnsureCandySecret(dep, true, cred)
		env[dep.Name] = val
	}

	for _, dep := range layer.SecretAccept() {
		val, _ := EnsureCandySecret(dep, false, cred)
		if val == "" && dep.Default != "" {
			env[dep.Name] = dep.Default
			continue
		}
		if val != "" {
			env[dep.Name] = val
		}
	}

	return env
}

// ResolveSecretForCandy is the batch variant used when multiple candies in a single deploy
// share secret_requires — their resolution results merge into one env map, with candy-order
// precedence (later candies win on duplicate names, matching generate.go's own
// secretRequiresMap semantics in the label-emission path).
func ResolveSecretForCandy(layers []spec.CandyReader, cred CredentialAccess) map[string]string {
	env := map[string]string{}
	for _, l := range layers {
		maps.Copy(env, ResolveCandySecret(l, cred))
	}
	return env
}
