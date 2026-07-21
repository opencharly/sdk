package deploykit

// secret_declare.go — the credential-backed secret-declaration lookups (K4:
// relocated from charly/config_secret_migration.go, the genuinely pure third
// of that file with no credential-store or project-loader dependency). The
// remaining functions in that file (MigratePlaintextEnvSecret,
// scrubSecretCLIEnv, writeDeployBackup) stay in charly-core: they call
// DefaultCredentialStore() (a provider-registry-coupled host singleton) and
// saveBundleConfigNodeForm (loader-seam-coupled) — inseparable from
// charly-core today, pending the config_image.go OpConfig seam.
//
// InjectSecretsIntoPlans (P13-KERNEL fold-in) is relocated from
// charly/layer_secrets.go — the ONE genuinely pure function in that file (the
// rest — ensureCandySecret/ResolveCandySecret/ResolveSecretForCandy/
// CandyForPlan — route through DefaultCredentialStore/ResolveCredential
// (provider-registry-coupled) and ScanAllCandyWithConfig (loader-coupled), so
// they stay charly-core, registered FINAL/K5 credential-family inventory).

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

// InjectSecretsIntoPlans merges the resolved secret env map into every
// OpStep's task.Env across the supplied plans. Existing task.Env keys
// are preserved (candy-declared env takes precedence over a credential-
// store collision — a deliberate choice so an author can explicitly pin
// a value they control). Called from the deploy-add path after
// ResolveCandySecret and before target.Emit so the heredoc renderer
// sees the values as regular env exports.
func InjectSecretsIntoPlans(plans []*InstallPlan, env map[string]string) {
	if len(env) == 0 {
		return
	}
	for _, p := range plans {
		for _, step := range p.Steps {
			ts, ok := step.(*OpStep)
			if !ok || ts.Op == nil {
				continue
			}
			if ts.Op.Env == nil {
				ts.Op.Env = map[string]string{}
			}
			for k, v := range env {
				if _, alreadySet := ts.Op.Env[k]; alreadySet {
					continue
				}
				ts.Op.Env[k] = v
			}
		}
	}
}
