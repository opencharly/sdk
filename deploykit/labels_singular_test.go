package deploykit

import (
	"encoding/json"
	"testing"

	"github.com/opencharly/sdk/spec"
)

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
