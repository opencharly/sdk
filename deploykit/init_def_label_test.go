package deploykit

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/opencharly/spec/container"
	"github.com/opencharly/spec/spec"
)

// TestInitDefLabel_RoundTrip proves the bake (WriteLabels) → parse
// (container.ExtractMetadata) round-trip for the ai.opencharly.init_def label
// (relocated from charly/init_def_label_test.go, K3 cone2 test closure): a
// literal *spec.CapabilityInitDef fixture (in place of the ORIGINAL test's
// charly-embedded supervisord vocab, which is charly-core-only data) is baked
// via WriteLabels, then parsed back via container.ExtractMetadata, and must
// come back byte-for-byte. The charly-side sibling test (still charly/,
// white-box on charly's own embedded vocabulary) asserts the embedded
// supervisord def is non-sparse, using spec.ProjectInitConfig directly — no
// sdk mechanism-kit import needed there any more.
func TestInitDefLabel_RoundTrip(t *testing.T) {
	capDef := spec.CapabilityInitDef{
		Entrypoint:         []string{"/usr/bin/supervisord", "-c", "/etc/supervisord.conf"},
		FallbackEntrypoint: []string{"/bin/sh"},
		ManagementTool:     "supervisorctl",
		ManagementCommands: map[string]string{"start": "start", "stop": "stop", "restart": "restart"},
	}

	payload, err := json.Marshal(capDef)
	if err != nil {
		t.Fatalf("marshal CapabilityInitDef: %v", err)
	}

	// Exercise the actual bake seam: WriteLabels must emit the
	// ai.opencharly.init_def label carrying exactly this JSON payload (podman's
	// Containerfile parser consumes the shell-quoting, so the stored OCI label
	// value is the raw JSON).
	bakedMeta := &spec.BakedLabelSet{
		Version: "2026.001.0000",
		Box:     "round-trip",
		Init:    "supervisord",
		InitDef: &capDef,
	}
	var b strings.Builder
	NewRenderGenerator().WriteLabels(&b, bakedMeta, "round-trip")
	emitted := b.String()
	if !strings.Contains(emitted, spec.LabelInitDef) || !strings.Contains(emitted, string(payload)) {
		t.Fatalf("bake seam did not emit %s with payload %s; got: %q", spec.LabelInitDef, payload, emitted)
	}

	// Parse path: container.ExtractMetadata reads the label value podman returns
	// (raw JSON). Override container.InspectLabels directly — the deploykit-level
	// re-export var is a value-copy that no longer affects the relocated body
	// (see sdk/deploykit/read_labels.go).
	orig := container.InspectLabels
	defer func() { container.InspectLabels = orig }()
	container.InspectLabels = func(engine, imageRef string) (map[string]string, error) {
		return map[string]string{
			spec.LabelVersion: "2026.001.0000",
			spec.LabelBox:     "round-trip",
			spec.LabelInit:    "supervisord",
			spec.LabelInitDef: string(payload),
		}, nil
	}
	meta, err := container.ExtractMetadata("podman", "round-trip")
	if err != nil {
		t.Fatalf("ExtractMetadata: %v", err)
	}
	if meta.InitDef == nil {
		t.Fatal("meta.InitDef nil after parse; expected the baked init_def")
	}
	if !reflect.DeepEqual(*meta.InitDef, capDef) {
		t.Errorf("parsed init_def = %+v, want %+v", *meta.InitDef, capDef)
	}
}
