package deploykit

// secret_declare.go — the credential-backed secret-declaration lookups (K4:
// relocated from charly/config_secret_migration.go, the genuinely pure third
// of that file with no credential-store or project-loader dependency). The
// remaining functions in that file (MigratePlaintextEnvSecret,
// scrubSecretCLIEnv, writeDeployBackup) stay in charly-core: they call
// DefaultCredentialStore() (a provider-registry-coupled host singleton) and
// saveBundleConfigNodeForm (loader-seam-coupled) — inseparable from
// charly-core today, pending the config_image.go OpConfig seam.

import (
	"strings"

	"github.com/opencharly/sdk/spec"
)

// SecretDeclaredOnBox returns the set of env var names an image declares
// as credential-backed (secret_accepts or secret_requires). Returns a
// non-nil empty set when meta is nil or has no secret declarations.
func SecretDeclaredOnBox(meta *spec.BoxMetadata) map[string]bool {
	names := map[string]bool{}
	if meta == nil {
		return names
	}
	for _, dep := range meta.SecretRequire {
		names[dep.Name] = true
	}
	for _, dep := range meta.SecretAccept {
		names[dep.Name] = true
	}
	return names
}

// SecretDepNames returns the flat list of env var names declared as
// credential-backed on an image. Used to populate
// deploykit.SaveDeployStateInput.SecretNames for the defense-in-depth scrub
// in saveDeployState. Returns nil (not an empty slice) when meta has no
// secret declarations — matches the rest of the omitempty-style API.
func SecretDepNames(meta *spec.BoxMetadata) []string {
	if meta == nil || (len(meta.SecretRequire) == 0 && len(meta.SecretAccept) == 0) {
		return nil
	}
	names := make([]string, 0, len(meta.SecretRequire)+len(meta.SecretAccept))
	for _, dep := range meta.SecretRequire {
		names = append(names, dep.Name)
	}
	for _, dep := range meta.SecretAccept {
		names = append(names, dep.Name)
	}
	return names
}

// SecretKeyForDep returns the (service, key) tuple used to look up a secret
// in the credential store. When the candy author set an explicit `key:
// charly/api-key/openrouter` override, that's parsed into its two segments;
// otherwise the default (charly/secret, dep.Name) is returned. The format is
// enforced by validateSecretDeps at build time, so this is purely a
// structural split — no validation is re-run here.
func SecretKeyForDep(dep spec.EnvDependency) (service, key string) {
	if dep.Key != "" {
		// Key format validated: ^charly/<service>/<key>$ — the service is
		// everything before the final "/", the key is the last segment.
		if idx := strings.LastIndex(dep.Key, "/"); idx >= 0 {
			return dep.Key[:idx], dep.Key[idx+1:]
		}
	}
	return "charly/secret", dep.Name
}
