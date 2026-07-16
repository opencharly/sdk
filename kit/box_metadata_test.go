package kit

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/opencharly/sdk/spec"
)

// TestExtractMetadata_CheckLevel proves the check_level capability label round-trips:
// emitted from BoxConfig at build (normalized via ResolveCheckLevel), parsed back into
// spec.BoxMetadata at deploy.
func TestExtractMetadata_CheckLevel(t *testing.T) {
	orig := InspectLabels
	defer func() { InspectLabels = orig }()

	InspectLabels = func(engine, imageRef string) (map[string]string, error) {
		return map[string]string{
			spec.LabelVersion:    "1",
			spec.LabelBox:        "x",
			spec.LabelCheckLevel: "agent",
		}, nil
	}
	meta, err := ExtractMetadata("podman", "x")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if meta.CheckLevel != "agent" {
		t.Errorf("meta.CheckLevel = %q, want agent", meta.CheckLevel)
	}
}

// TestExtractMetadata_EnvObjectLabel proves the ai.opencharly.env label — baked
// as a JSON OBJECT from the image's spec.Box.Env map — parses back into
// meta.Env's []string KEY=VALUE form (sorted, via EnvMapToPairs). Decoding the
// object straight into []string was the writer/reader mismatch that failed every
// image with a box-level env: map (check box android-emulator: "cannot
// unmarshal object into []string").
func TestExtractMetadata_EnvObjectLabel(t *testing.T) {
	orig := InspectLabels
	defer func() { InspectLabels = orig }()
	InspectLabels = func(engine, imageRef string) (map[string]string, error) {
		return map[string]string{
			spec.LabelVersion: "2026.001.0000",
			spec.LabelBox:     "env-image",
			spec.LabelEnv:     `{"EMULATOR_NAME":"charly_avd_36","EMULATOR_API_LEVEL":"36","EMULATOR_DEVICE":"pixel_9a"}`,
		}, nil
	}
	meta, err := ExtractMetadata("podman", "env-image")
	if err != nil {
		t.Fatalf("ExtractMetadata must decode the env OBJECT label: %v", err)
	}
	want := []string{"EMULATOR_API_LEVEL=36", "EMULATOR_DEVICE=pixel_9a", "EMULATOR_NAME=charly_avd_36"} // sorted
	if !reflect.DeepEqual(meta.Env, want) {
		t.Fatalf("meta.Env: got %v want %v", meta.Env, want)
	}
}

// TestExtractMetadata_EnvAbsent: no env label → nil, no error.
func TestExtractMetadata_EnvAbsent(t *testing.T) {
	orig := InspectLabels
	defer func() { InspectLabels = orig }()
	InspectLabels = func(engine, imageRef string) (map[string]string, error) {
		return map[string]string{spec.LabelVersion: "2026.001.0000", spec.LabelBox: "no-env"}, nil
	}
	meta, err := ExtractMetadata("podman", "no-env")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Env != nil {
		t.Fatalf("expected nil env, got %v", meta.Env)
	}
}

// TestExtractMetadata_SingularLabels proves the 2026-06 singular-label
// contract end-to-end on the read path: ExtractMetadata reads the SINGULAR
// `ai.opencharly.*` keys into the renamed BoxMetadata fields. The keys are
// written as LITERAL strings (not via the consts), so if any label const ever
// regresses to plural, ExtractMetadata — which reads via the const — won't find
// the literal key and the field stays empty, failing this test.
func TestExtractMetadata_SingularLabels(t *testing.T) {
	orig := InspectLabels
	defer func() { InspectLabels = orig }()

	svcBlob, _ := json.Marshal([]spec.CapabilityService{{Name: "web", Init: "supervisord"}})
	envBlob, _ := json.Marshal(map[string]string{"OLLAMA_HOST": "http://{{.ContainerName}}:11434"})

	InspectLabels = func(engine, imageRef string) (map[string]string, error) {
		return map[string]string{
			"ai.opencharly.version":     "2026.155.1801",
			"ai.opencharly.image":       "demo",
			"ai.opencharly.port":        `["8080:8080"]`,
			"ai.opencharly.service":     string(svcBlob),
			"ai.opencharly.env_provide": string(envBlob),
		}, nil
	}

	meta, err := ExtractMetadata("podman", "ghcr.io/opencharly/demo:test")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(meta.Port) != 1 || meta.Port[0] != "8080:8080" {
		t.Errorf("singular ai.opencharly.port → meta.Port: got %+v", meta.Port)
	}
	if len(meta.Service) != 1 || meta.Service[0].Name != "web" {
		t.Errorf("singular ai.opencharly.service → meta.Service: got %+v", meta.Service)
	}
	if meta.EnvProvide["OLLAMA_HOST"] == "" {
		t.Errorf("singular ai.opencharly.env_provide → meta.EnvProvide: got %+v", meta.EnvProvide)
	}
}

// TestLabelConstantsAreSingular pins every renamed label const to its singular
// wire string. A regression to plural fails the suite — the contract guard.
func TestLabelConstantsAreSingular(t *testing.T) {
	pairs := []struct{ got, want string }{
		{spec.LabelPort, "ai.opencharly.port"},
		{spec.LabelVolume, "ai.opencharly.volume"},
		{spec.LabelAlias, "ai.opencharly.alias"},
		{spec.LabelHook, "ai.opencharly.hook"},
		{spec.LabelRoute, "ai.opencharly.route"},
		{spec.LabelSecret, "ai.opencharly.secret"},
		{spec.LabelService, "ai.opencharly.service"},
		{spec.LabelSkill, "ai.opencharly.skill"},
		{spec.LabelEnvCandy, "ai.opencharly.env_candy"},
		{spec.LabelPortProto, "ai.opencharly.port_proto"},
		{spec.LabelCandyVersion, "ai.opencharly.candy_version"},
		{spec.LabelPlatformFormat, "ai.opencharly.platform.format"},
		{spec.LabelBuilderUse, "ai.opencharly.builder.use"},
		{spec.LabelBuilderProvide, "ai.opencharly.builder.provide"},
		{spec.LabelEnvProvide, "ai.opencharly.env_provide"},
		{spec.LabelEnvRequire, "ai.opencharly.env_require"},
		{spec.LabelEnvAccept, "ai.opencharly.env_accept"},
		{spec.LabelSecretAccept, "ai.opencharly.secret_accept"},
		{spec.LabelSecretRequire, "ai.opencharly.secret_require"},
		{spec.LabelMCPProvide, "ai.opencharly.mcp_provide"},
		{spec.LabelMCPRequire, "ai.opencharly.mcp_require"},
		{spec.LabelMCPAccept, "ai.opencharly.mcp_accept"},
	}
	for _, p := range pairs {
		if p.got != p.want {
			t.Errorf("label const = %q, want singular %q", p.got, p.want)
		}
	}
}
