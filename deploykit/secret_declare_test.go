package deploykit

import (
	"reflect"
	"testing"

	"github.com/opencharly/sdk/spec"
)

func TestSecretDeclaredOnBox(t *testing.T) {
	meta := &spec.BoxMetadata{
		SecretRequire: []spec.EnvDependency{{Name: "K3S_CLUSTER_TOKEN"}},
		SecretAccept:  []spec.EnvDependency{{Name: "WEBUI_ADMIN_PASSWORD"}},
	}
	got := SecretDeclaredOnBox(meta)
	want := map[string]bool{"K3S_CLUSTER_TOKEN": true, "WEBUI_ADMIN_PASSWORD": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SecretDeclaredOnBox = %v, want %v", got, want)
	}
	if got := SecretDeclaredOnBox(nil); len(got) != 0 {
		t.Errorf("SecretDeclaredOnBox(nil) = %v, want empty", got)
	}
}

func TestSecretDepNames(t *testing.T) {
	meta := &spec.BoxMetadata{
		SecretRequire: []spec.EnvDependency{{Name: "A"}},
		SecretAccept:  []spec.EnvDependency{{Name: "B"}},
	}
	got := SecretDepNames(meta)
	want := []string{"A", "B"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SecretDepNames = %v, want %v", got, want)
	}
	if got := SecretDepNames(nil); got != nil {
		t.Errorf("SecretDepNames(nil) = %v, want nil", got)
	}
	if got := SecretDepNames(&spec.BoxMetadata{}); got != nil {
		t.Errorf("SecretDepNames(empty) = %v, want nil", got)
	}
}

func TestSecretKeyForDep(t *testing.T) {
	tests := []struct {
		dep         spec.EnvDependency
		wantService string
		wantKey     string
	}{
		{dep: spec.EnvDependency{Name: "WEBUI_ADMIN_PASSWORD"}, wantService: "charly/secret", wantKey: "WEBUI_ADMIN_PASSWORD"},
		{dep: spec.EnvDependency{Name: "OPENROUTER_API_KEY", Key: "charly/api-key/openrouter"}, wantService: "charly/api-key", wantKey: "openrouter"},
	}
	for _, tt := range tests {
		service, key := SecretKeyForDep(tt.dep)
		if service != tt.wantService || key != tt.wantKey {
			t.Errorf("SecretKeyForDep(%+v) = (%q, %q), want (%q, %q)", tt.dep, service, key, tt.wantService, tt.wantKey)
		}
	}
}
