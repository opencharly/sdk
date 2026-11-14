package deploykit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "deploy.yml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

// TestLoadDeployFileReadsTheNameFirstNodeForm pins sdk#517: the per-host file and every project
// charly.yml are NAME-FIRST with an EDGE KIND (`<name>: {pod: …}`), while LoadDeployFile parsed
// only the wrapped `deploy: {<name>: <flat body>}` shape — a shape nothing writes. The result was
// silent data loss: `charly deploy import <file>` reported "Imported 1 file(s)" and imported
// nothing.
func TestLoadDeployFileReadsTheNameFirstNodeForm(t *testing.T) {
	p := writeTemp(t, `
openwebui-lab:
  pod:
    image: "ghcr.io/opencharly/openwebui:2026.317.2254"
    disposable: true
    port: ["18080:8080"]
    env:
      WEBUI_ADMIN_EMAIL: admin@lab.local
`)
	dc, err := LoadDeployFile(p)
	if err != nil {
		t.Fatalf("LoadDeployFile: %v", err)
	}
	entry, ok := dc.Deploy["openwebui-lab"]
	if !ok {
		t.Fatalf("the name-first entry must be read; got keys %v", nodeKeys(dc.Deploy))
	}
	if entry.Image != "ghcr.io/opencharly/openwebui:2026.317.2254" {
		t.Fatalf("image not carried: %+v", entry)
	}
	if len(entry.Port) != 1 || entry.Port[0] != "18080:8080" {
		t.Fatalf("port pin not carried: %+v", entry.Port)
	}
	if entry.Disposable == nil || !*entry.Disposable {
		t.Fatalf("disposable not carried: %+v", entry)
	}
}

// TestLoadDeployFileStillReadsTheWrappedForm keeps the shape DeployConfig marshals to working.
func TestLoadDeployFileStillReadsTheWrappedForm(t *testing.T) {
	p := writeTemp(t, `
deploy:
  legacy:
    image: "ghcr.io/opencharly/redis:2026.1.1"
`)
	dc, err := LoadDeployFile(p)
	if err != nil {
		t.Fatalf("LoadDeployFile: %v", err)
	}
	if entry, ok := dc.Deploy["legacy"]; !ok || entry.Image != "ghcr.io/opencharly/redis:2026.1.1" {
		t.Fatalf("the wrapped form must keep working; got %v", nodeKeys(dc.Deploy))
	}
}

// TestLoadDeployFileFailsLoudlyWhenNothingParses pins the other half: a file that contributes no
// deploy entry is an ERROR, never a silent success — the host-file keys (provides/cache/ledger/
// system/sidecar) are not deploy entries.
func TestLoadDeployFileFailsLoudlyWhenNothingParses(t *testing.T) {
	p := writeTemp(t, `
system:
  hostname: a
cache: {}
`)
	dc, err := LoadDeployFile(p)
	if err == nil {
		t.Fatalf("a file with no deploy entry must fail loudly; got %d entries", len(dc.Deploy))
	}
	for _, want := range []string{"no deploy entry", "deploy:"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error must say %q so the operator can fix the shape; got %v", want, err)
		}
	}
}

func nodeKeys(m map[string]spec.DeployNode) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
