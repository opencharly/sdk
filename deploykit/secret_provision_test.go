package deploykit

import "testing"

// TestEnvVarNameToPodmanSecretSlug relocated from charly/validate_test.go (Cutover B unit
// 6b — EnvVarNameToPodmanSecretSlug itself moved here alongside CollectCandySecretAccepts,
// its sole caller).
func TestEnvVarNameToPodmanSecretSlug(t *testing.T) {
	cases := map[string]string{
		"OPENROUTER_API_KEY":   "openrouter-api-key",
		"IMMICH_API_KEY":       "immich-api-key",
		"WEBUI_ADMIN_PASSWORD": "webui-admin-password",
		"TS_AUTHKEY":           "ts-authkey",
		"X":                    "x",
	}
	for in, want := range cases {
		if got := EnvVarNameToPodmanSecretSlug(in); got != want {
			t.Errorf("EnvVarNameToPodmanSecretSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
