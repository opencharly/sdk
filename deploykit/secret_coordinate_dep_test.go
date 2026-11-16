package deploykit

// secret_coordinate_dep_test.go — ONE rule for a candy-declared secret's coordinate.
//
// opencharly/sdk#526 and opencharly/pod-openwebui#19 both live in this seam: a secret's coordinate
// is computed in more than one place, and the copies drift. sdk carried TWO — the exported
// SecretKeyForDep and an inline copy inside EnsureCandySecret — with a subtle difference between
// them (`idx >= 0` vs `idx > 0` on the override split). The inline copy is gone; these tests pin
// the one rule AND the property that made the duplication dangerous: the RESOLVER must query the
// same coordinate the rule names.

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

func TestSecretKeyForDep_OneRule(t *testing.T) {
	cases := []struct {
		name        string
		dep         spec.EnvDependency
		wantService string
		wantKey     string
	}{
		{
			name:        "no override falls back to the historical default",
			dep:         spec.EnvDependency{Name: "WEBUI_ADMIN_PASSWORD"},
			wantService: "charly/secret", wantKey: "WEBUI_ADMIN_PASSWORD",
		},
		{
			name:        "a single-slash override splits at that slash",
			dep:         spec.EnvDependency{Name: "OPENROUTER_API_KEY", Key: "charly/api-key"},
			wantService: "charly", wantKey: "api-key",
		},
		{
			name:        "a multi-slash override splits at the LAST slash (the service is everything before it)",
			dep:         spec.EnvDependency{Name: "QDRANT__SERVICE__API_KEY", Key: "charly/api-key/qdrant"},
			wantService: "charly/api-key", wantKey: "qdrant",
		},
		{
			name:        "a deeper service keeps all but the last segment",
			dep:         spec.EnvDependency{Name: "TOKEN", Key: "charly/secret/team/prod/token"},
			wantService: "charly/secret/team/prod", wantKey: "token",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, key := SecretKeyForDep(c.dep)
			if svc != c.wantService || key != c.wantKey {
				t.Errorf("SecretKeyForDep(%+v) = (%q, %q), want (%q, %q)", c.dep, svc, key, c.wantService, c.wantKey)
			}
		})
	}
}

// TestEnsureCandySecret_QueriesTheSameCoordinate is the property the duplication endangered: the
// resolver must ask the credential store for exactly the coordinate SecretKeyForDep names. A future
// change that re-inlines the split (or edits only one side) fails here.
func TestEnsureCandySecret_QueriesTheSameCoordinate(t *testing.T) {
	deps := []spec.EnvDependency{
		{Name: "WEBUI_ADMIN_PASSWORD"},
		{Name: "OPENROUTER_API_KEY", Key: "charly/api-key"},
		{Name: "QDRANT__SERVICE__API_KEY", Key: "charly/api-key/qdrant"},
	}
	for _, dep := range deps {
		var askedService, askedKey string
		cred := CredentialAccess{
			Resolve: func(_, service, key, _ string) (string, string) {
				askedService, askedKey = service, key
				return "", ""
			},
			Write: func(_, _, _ string) error { return nil },
		}
		_, _ = EnsureCandySecret(dep, true, cred)
		wantService, wantKey := SecretKeyForDep(dep)
		if askedService != wantService || askedKey != wantKey {
			t.Errorf("EnsureCandySecret(%s) queried (%q, %q), but the rule names (%q, %q)",
				dep.Name, askedService, askedKey, wantService, wantKey)
		}
	}
}

// TestEnsureCandySecret_EnvVarOverrideKeepsTheCoordinate: the env var is the FIRST lookup key but it
// must not replace the coordinate — the resolver is asked with dep.Name as the env var and the
// declared coordinate as the store lookup.
func TestEnsureCandySecret_EnvVarOverrideKeepsTheCoordinate(t *testing.T) {
	var askedEnv, askedService, askedKey string
	cred := CredentialAccess{
		Resolve: func(envVar, service, key, _ string) (string, string) {
			askedEnv, askedService, askedKey = envVar, service, key
			return "value-from-env", "env"
		},
		Write: func(_, _, _ string) error { return nil },
	}
	dep := spec.EnvDependency{Name: "K3S_CLUSTER_TOKEN", Key: "charly/secret/K3S_CLUSTER_TOKEN"}
	val, source := EnsureCandySecret(dep, true, cred)
	if val != "value-from-env" || source != "env" {
		t.Fatalf("EnsureCandySecret = (%q, %q), want the env value", val, source)
	}
	if askedEnv != "K3S_CLUSTER_TOKEN" {
		t.Errorf("resolver env var = %q, want the dependency name", askedEnv)
	}
	if askedService != "charly/secret" || askedKey != "K3S_CLUSTER_TOKEN" {
		t.Errorf("resolver coordinate = (%q, %q), want (charly/secret, K3S_CLUSTER_TOKEN)", askedService, askedKey)
	}
}
