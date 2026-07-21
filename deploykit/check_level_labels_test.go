package deploykit

import (
	"testing"

	"github.com/opencharly/sdk/spec"
)

// The check_level capability label must round-trip: emitted from BoxConfig at build
// (normalized via ResolveCheckLevel, kit-sourced), parsed back into BoxMetadata at deploy.
// Exercises ExtractMetadata + the OCI label surface, not the ladder logic (which has its
// own tests in sdk/kit).
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
