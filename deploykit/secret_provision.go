package deploykit

import (
	"fmt"
	"os"
	"strings"

	"github.com/opencharly/sdk/spec"
	"golang.org/x/term"
)

// secret_provision.go — the secret-provisioning ORCHESTRATION relocated from
// charly/secrets.go (Cutover B unit 6b, the InvokeProvider-generalization family). These
// functions were the LAST secrets.go residents blocked on a credential-store dependency
// (ResolveCredential/DefaultCredentialStore, the core provider registry) — they now take
// that dependency as an INJECTED CredentialAccess instead of calling a package-level
// function by name, so the SAME orchestration serves BOTH placements with zero
// duplication (R3): charly-core supplies a CredentialAccess backed by its
// connectPluginByWordRef-based ResolveCredential/DefaultCredentialStore adapter
// (credential_plugin.go); an out-of-process caller (e.g. a deploy-time plugin) supplies one
// backed by sdk.Executor.InvokeProvider(ClassVerb, "credential", …) peer-to-peer. Neither
// caller needs a provider-registry import here — this package stays sdk-only.

// CredentialResolver abstracts a single credential-store lookup — the shape of
// charly-core's ResolveCredential(envVar, service, key, defaultVal) (value, source).
type CredentialResolver func(envVar, service, key, defaultVal string) (value, source string)

// CredentialWriter abstracts a single credential-store persist — the shape of
// charly-core's CredentialStore.Set(service, key, value) error.
type CredentialWriter func(service, key, value string) error

// CredentialAccess bundles the two credential-store operations this file's orchestration
// needs. Both fields are required by ProvisionPodmanSecrets/GenerateAndStoreSecret; only
// Resolve is used by ResolveSecretValue/CollectCandySecretAccepts/ResolveHookSecretEnv.
type CredentialAccess struct {
	Resolve CredentialResolver
	Write   CredentialWriter
}

// EnvVarNameToPodmanSecretSlug converts an env var name to the slug used in the podman
// secret name (lowercase + underscores → hyphens). Relocated from charly/validate.go
// (Cutover B unit 6b) — its sole remaining caller was CollectCandySecretAccepts below.
func EnvVarNameToPodmanSecretSlug(envVarName string) string {
	return strings.ReplaceAll(strings.ToLower(envVarName), "_", "-")
}

// GenerateAndStoreSecret generates a 32-byte url-safe base64 token (44 chars;
// Fernet-key-compatible — see GenerateRandomSecretToken), persists it to the active
// credential store at (service, key) via cred.Write, and returns the value with the
// "auto-generated" source classification. Persistence failures are logged to stderr but
// not returned as errors — the in-memory value is still usable for the current
// invocation.
//
// Used by:
//   - ProvisionPodmanSecrets — config-time CollectedSecret provisioning when
//     --password=auto is in effect.
//   - charly-core's ensureCandySecret (layer_secrets.go) — deploy-time secret_requires
//     resolution on host/VM/SSH targets when the value is missing (calls this directly,
//     supplying its own CredentialAccess).
func GenerateAndStoreSecret(service, key string, cred CredentialAccess) (val, source string) {
	val = GenerateRandomSecretToken(32)
	if err := cred.Write(service, key, val); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not persist auto-generated secret %s/%s: %v\n",
			service, key, err)
	}
	return val, "auto-generated"
}

// ProvisionPodmanSecrets creates podman secrets from the credential store.
// Returns the secrets that were successfully provisioned and any that fell back to env vars.
func ProvisionPodmanSecrets(engine, boxName, instance string, secrets []CollectedSecret, autoGenerate bool, credServiceVNC string, cred CredentialAccess) (provisioned []CollectedSecret, fallbackEnv []string, err error) { //nolint:unparam // error return kept for interface/API stability
	if engine == "docker" {
		fmt.Fprintln(os.Stderr, "NOTE: Docker secrets require Swarm mode (not available).")
		fmt.Fprintln(os.Stderr, "Falling back to environment variable injection for secrets.")
		fmt.Fprintln(os.Stderr, "This is less secure — secret values will be visible in 'docker inspect'.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Consider using Podman for better secrets support:")
		fmt.Fprintln(os.Stderr, "  charly config set engine.run podman")
		// Fall back to env vars for all secrets
		for _, s := range secrets {
			if s.Env != "" {
				val, _ := ResolveSecretValue(s, boxName, instance, credServiceVNC, cred.Resolve)
				if val != "" {
					fallbackEnv = append(fallbackEnv, s.Env+"="+val)
				}
			}
		}
		return nil, fallbackEnv, nil
	}

	if len(secrets) > 0 {
		fmt.Fprintln(os.Stderr, "Provisioning container secrets:")
	}
	// promptedValues caches values entered interactively for a given podman secret name.
	// Two CollectedSecrets sharing the same Name (but different Env vars) only prompt once.
	promptedValues := make(map[string]string)
	interactive := term.IsTerminal(int(os.Stdin.Fd()))

	for _, s := range secrets {
		// Short-circuit: if a podman secret already exists, keep it
		// unconditionally — unless RotateOnConfig is true, in which case we
		// always re-resolve and re-create so credential rotation via
		// `charly secrets set <name> <new>` takes effect on the next charly config.
		//
		// The default (RotateOnConfig=false) is correct for candy-owned
		// secrets like immich's db-password: overwriting would break a live
		// postgres cluster. RotateOnConfig=true is set by
		// CollectCandySecretAccepts for secret_accepts/secret_requires
		// entries, whose whole point is to reflect the current credential
		// store value on every reconcile. See plan §2.3.
		if !s.RotateOnConfig && PodmanSecretExists(engine, s.Name) {
			fmt.Fprintf(os.Stderr, "  %-40s → kept (already provisioned)\n", s.Name)
			provisioned = append(provisioned, s)
			continue
		}

		val, source := ResolveSecretValue(s, boxName, instance, credServiceVNC, cred.Resolve)
		if val == "" {
			switch {
			case autoGenerate:
				// Auto-generate: reuse if same podman secret name already generated
				if cached, ok := promptedValues[s.Name]; ok {
					val = cached
					source = "auto-generated"
				} else {
					val, source = GenerateAndStoreSecret("charly/secret", s.Name, cred)
					promptedValues[s.Name] = val
				}
			case interactive:
				if cached, ok := promptedValues[s.Name]; ok {
					val = cached
					source = "user input"
				} else {
					prompt := fmt.Sprintf("Enter value for secret '%s'", s.SecretName)
					if s.Env != "" {
						prompt += fmt.Sprintf(" (%s)", s.Env)
					}
					prompt += ": "
					entered, promptErr := PromptPassword(prompt)
					if promptErr != nil {
						fmt.Fprintf(os.Stderr, "  %-40s → prompt failed: %v\n", s.Name, promptErr)
						continue
					}
					if entered == "" {
						fmt.Fprintf(os.Stderr, "  %-40s → skipped (no value entered)\n", s.Name)
						continue
					}
					if storeErr := cred.Write("charly/secret", s.Name, entered); storeErr != nil {
						fmt.Fprintf(os.Stderr, "  Warning: could not persist secret '%s': %v\n", s.Name, storeErr)
					}
					promptedValues[s.Name] = entered
					val = entered
					source = "user input"
				}
			default:
				fmt.Fprintf(os.Stderr, "  %-40s → no value configured\n", s.Name)
				fmt.Fprintf(os.Stderr, "\nWARNING: Secret '%s' has no value configured.\n", s.SecretName)
				fmt.Fprintf(os.Stderr, "The container may fail to start properly.\n\n")
				fmt.Fprintf(os.Stderr, "To set it:\n")
				if s.Env != "" {
					fmt.Fprintf(os.Stderr, "  %s=xxx charly config %s  (env var override)\n", s.Env, boxName)
				}
				fmt.Fprintf(os.Stderr, "  charly secrets set charly/secret %s\n\n", s.Name)
				continue
			}
		}

		if err := EnsurePodmanSecret(engine, s.Name, val); err != nil {
			fmt.Fprintf(os.Stderr, "  %-40s → FAILED: %v\n", s.Name, err)
			// Fall back to env var if available
			if s.Env != "" {
				fallbackEnv = append(fallbackEnv, s.Env+"="+val)
			}
			continue
		}
		fmt.Fprintf(os.Stderr, "  %-40s → created (from %s)\n", s.Name, source)
		provisioned = append(provisioned, s)
	}

	if len(provisioned) > 0 {
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Note: Secrets are mounted at /run/secrets/<name> inside the container.")
		fmt.Fprintf(os.Stderr, "To update a secret after changing it: charly update %s\n", boxName)
	}

	return provisioned, fallbackEnv, nil
}

// ResolveSecretValue looks up the value for a secret from the credential store.
//
// When CollectedSecret.Service and CollectedSecret.Key are both non-empty, they take
// precedence over the default lookup chain: the credential store is queried exactly at
// (Service, Key) with the Env var as the env override. This is the path used by
// secret_accepts / secret_requires entries synthesized by CollectCandySecretAccepts,
// where the candy author may have set `key: charly/api-key/openrouter` to point at a
// shared credential namespace.
//
// When Service/Key are unset, the default chain (used by candy-owned secrets) applies:
// env var → charly/secret/<podman-name> → charly/secret/<bare-secret-name>.
func ResolveSecretValue(s CollectedSecret, boxName, instance, credServiceVNC string, resolve CredentialResolver) (value, source string) {
	// Explicit override from CollectCandySecretAccepts: query exactly once at
	// (Service, Key), allowing the Env var to win via the resolver's env-first chain.
	if s.Service != "" && s.Key != "" {
		val, src := resolve(s.Env, s.Service, s.Key, "")
		return val, src
	}

	// Default chain for candy-owned secrets (pre-existing behavior).
	// If the secret has an associated env var, check it first.
	//
	// Multi-tailnet path: when the sidecar resolution set HostEnv to a templated
	// host-side env var name (e.g. TS_AUTHKEY_ARMADILLO_QUAIL_TS_NET), the env-var lookup
	// uses THAT name — the container-side Env (TS_AUTHKEY) is only the QUADLET TARGET, not
	// the host-side source. Without this split, multi-tailnet operators couldn't store
	// per-tailnet keys in `.secrets` (a single TS_AUTHKEY var means a single tailnet).
	envLookup := s.Env
	if s.HostEnv != "" {
		envLookup = s.HostEnv
	}
	if envLookup != "" {
		val, src := resolve(envLookup, CredServiceForSecret(s.Env, credServiceVNC), CredKeyForSecret(boxName, instance), "")
		if val != "" {
			return val, src
		}
	}
	// Try by full podman secret name (e.g. "charly-immich-db-password") — matches
	// `charly secrets set charly/secret charly-immich-db-password`
	if val, src := resolve("", "charly/secret", s.Name, ""); val != "" {
		return val, src
	}
	// Fallback: try by bare secret name (e.g. "db-password")
	val, src := resolve("", "charly/secret", s.SecretName, "")
	return val, src
}

// SecretResolution records the result of resolving a single secret_accepts or
// secret_requires entry against the credential store. Returned alongside the
// []CollectedSecret list from CollectCandySecretAccepts so downstream callers (charly's
// checkMissingSecretRequires) can distinguish "required but missing" from "optional and
// absent" with actionable remediation.
type SecretResolution struct {
	Name     string // env var name (e.g., "OPENROUTER_API_KEY")
	Source   string // resolver source classification (env/keyring/config/locked/unavailable/default)
	Resolved bool   // true iff a non-empty value was obtained
	Required bool   // true iff the entry came from secret_requires (not secret_accepts)
}

// CollectCandySecretAccepts synthesizes CollectedSecret entries from an image's
// secret_accepts and secret_requires label metadata, resolving each against the
// credential store and returning:
//
//   - []CollectedSecret: one entry per secret whose value was successfully resolved
//     (non-empty). Entries carry Service/Key overrides from the candy manifest `key:`
//     field (default: charly/secret/<env-var-name>) and RotateOnConfig=true so every
//     charly config reconciles them with the latest credential store value (see plan
//     §2.3).
//   - []SecretResolution: one entry per input spec, reporting the source classification
//     and whether the resolution succeeded. Required entries with Resolved=false are
//     later caught by charly's checkMissingSecretRequires as a hard-fail condition.
//
// This function does NOT touch the podman secret store — that's the job of
// ProvisionPodmanSecrets. It only reads from the credential store. No network calls, no
// filesystem mutations, safe to run speculatively.
func CollectCandySecretAccepts(boxName, instance string, meta *spec.BoxMetadata, credServiceVNC string, cred CredentialAccess) (collected []CollectedSecret, resolutions []SecretResolution) {
	if meta == nil {
		return nil, nil
	}

	resolveOne := func(dep spec.EnvDependency, required bool) {
		// Parse the optional Key override (<service>/<key> form, validated at build time
		// by validateSecretDeps). Default is charly/secret/<name>.
		service := "charly/secret"
		key := dep.Name
		if dep.Key != "" {
			// Key format is already validated (must match ^charly/.../...$).
			// Service is everything before the final '/', key is the last
			// segment (LastIndex avoids depending on the literal prefix length).
			if idx := strings.LastIndex(dep.Key, "/"); idx >= 0 {
				service = dep.Key[:idx]
				key = dep.Key[idx+1:]
			}
		}

		cs := CollectedSecret{
			Name:           "charly-" + boxName + "-" + EnvVarNameToPodmanSecretSlug(dep.Name),
			Target:         "", // type=env directive doesn't use Target
			Env:            dep.Name,
			SecretName:     dep.Name,
			Service:        service,
			Key:            key,
			RotateOnConfig: true,
		}

		val, src := ResolveSecretValue(cs, boxName, instance, credServiceVNC, cred.Resolve)

		res := SecretResolution{
			Name:     dep.Name,
			Source:   src,
			Resolved: val != "",
			Required: required,
		}
		resolutions = append(resolutions, res)

		if val != "" {
			collected = append(collected, cs)
		}
	}

	for _, dep := range meta.SecretRequire {
		resolveOne(dep, true)
	}
	for _, dep := range meta.SecretAccept {
		resolveOne(dep, false)
	}

	return collected, resolutions
}

// ResolveHookSecretEnv returns `NAME=value` entries for every secret_accept /
// secret_require value that resolves from the credential store, so lifecycle hooks
// (post_enable / pre_remove) receive credential-backed secrets EXPLICITLY via `podman
// exec -e`. This is load-bearing: the CLI `-e` form of these secrets is scrubbed from
// c.Env by charly-core's scrubSecretCLIEnv (never plaintext in charly.yml), and a podman
// `type=env` secret is not reliably inherited by `podman exec`, so a hook that consumes a
// secret (e.g. github-runner's registration token) would otherwise never see it. Generic
// across every hook+secret candy (R3); inert (returns nil) when the image declares no
// secrets or none resolve.
func ResolveHookSecretEnv(boxName, instance string, meta *spec.BoxMetadata, credServiceVNC string, cred CredentialAccess) []string {
	collected, _ := CollectCandySecretAccepts(boxName, instance, meta, credServiceVNC, cred)
	var env []string
	for _, s := range collected {
		if s.Env == "" {
			continue
		}
		if val, _ := ResolveSecretValue(s, boxName, instance, credServiceVNC, cred.Resolve); val != "" {
			env = append(env, s.Env+"="+val)
		}
	}
	return env
}
