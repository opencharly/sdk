package deploykit

import (
	"reflect"
	"testing"
)

func TestFilterOwnProvidesEnv(t *testing.T) {
	entries := []EnvProvideEntry{
		{Name: "OLLAMA_HOST", Value: "http://charly-ollama:11434", Source: "ollama"},
		{Name: "PGHOST", Value: "charly-postgresql", Source: "postgresql"},
		{Name: "CUSTOM", Value: "val", Source: "myimage"},
	}

	got := FilterOwnProvides(entries, "ollama")
	want := []EnvProvideEntry{
		{Name: "PGHOST", Value: "charly-postgresql", Source: "postgresql"},
		{Name: "CUSTOM", Value: "val", Source: "myimage"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FilterOwnProvides(env, ollama) = %v, want %v", got, want)
	}
}

func TestFilterOwnProvidesMCP(t *testing.T) {
	entries := []MCPProvideEntry{
		{Name: "jupyter", URL: "http://charly-jupyter:8888/mcp", Transport: "http", Source: "jupyter"},
		{Name: "code-search", URL: "http://charly-search:3100/mcp", Transport: "http", Source: "search"},
	}

	got := FilterOwnProvides(entries, "jupyter")
	if len(got) != 1 || got[0].Name != "code-search" {
		t.Errorf("FilterOwnProvides(mcp, jupyter) = %v, want only code-search", got)
	}
}

func TestFilterOwnProvidesEmpty(t *testing.T) {
	entries := []MCPProvideEntry{
		{Name: "test", URL: "http://localhost", Source: "img"},
	}
	got := FilterOwnProvides(entries, "")
	if len(got) != 1 {
		t.Errorf("FilterOwnProvides with empty boxName should return all entries")
	}
}

func TestResolveTemplate(t *testing.T) {
	tests := []struct {
		tmpl, ctr, want string
		portMap         map[int]int
	}{
		{tmpl: "http://{{.ContainerName}}:8888/mcp", ctr: "charly-jupyter", want: "http://charly-jupyter:8888/mcp"},
		{tmpl: "no-template", ctr: "charly-test", want: "no-template"},
		{tmpl: "{{.ContainerName}}:{{.ContainerName}}", ctr: "charly-x", want: "charly-x:charly-x"},
		// New: {{.HostPort N}} resolves against portMap.
		{tmpl: "http://127.0.0.1:{{.HostPort 3000}}", ctr: "charly-versa",
			portMap: map[int]int{3000: 23000}, want: "http://127.0.0.1:23000"},
		// New: unmapped container port falls back to literal N.
		{tmpl: "http://127.0.0.1:{{.HostPort 9999}}", ctr: "charly-versa",
			portMap: map[int]int{3000: 23000}, want: "http://127.0.0.1:9999"},
		// New: nil portMap → fallback to literal N.
		{tmpl: "http://127.0.0.1:{{.HostPort 8080}}", ctr: "charly-x",
			portMap: nil, want: "http://127.0.0.1:8080"},
		// New: {{.ContainerPort N}} always resolves to N (symmetry/readability).
		{tmpl: "http://{{.ContainerName}}:{{.ContainerPort 8080}}", ctr: "charly-airflow",
			portMap: map[int]int{8080: 28080}, want: "http://charly-airflow:8080"},
		// Combined: both placeholders + container name.
		{tmpl: "internal=http://{{.ContainerName}}:{{.ContainerPort 8080}} public=http://127.0.0.1:{{.HostPort 8080}}",
			ctr: "charly-airflow", portMap: map[int]int{8080: 28080},
			want: "internal=http://charly-airflow:8080 public=http://127.0.0.1:28080"},
	}
	for _, tt := range tests {
		got := ResolveTemplate(tt.tmpl, tt.ctr, tt.portMap)
		if got != tt.want {
			t.Errorf("ResolveTemplate(%q, %q, %v) = %q, want %q", tt.tmpl, tt.ctr, tt.portMap, got, tt.want)
		}
	}
}

func TestValidateProvidesTemplate(t *testing.T) {
	tests := []struct {
		tmpl string
		want bool
	}{
		{"http://{{.ContainerName}}:8888/mcp", true},
		{"no-template", true},
		{"{{.ContainerName}}", true},
		{"{{.BadVar}}", false},
		{"{{.ContainerName}}{{.Other}}", false},
		{"{{broken", false},
		// New placeholders — must be allowed when N is numeric.
		{"http://127.0.0.1:{{.HostPort 3000}}", true},
		{"{{.ContainerPort 8080}}", true},
		{"both {{.HostPort 1}} and {{.ContainerPort 2}}", true},
		// Numeric requirement: non-numeric argument is rejected.
		{"{{.HostPort foo}}", false},
		{"{{.ContainerPort bar}}", false},
		// Unterminated placeholders still rejected.
		{"{{.HostPort 3000", false},
	}
	for _, tt := range tests {
		got := ValidateProvidesTemplate(tt.tmpl)
		if got != tt.want {
			t.Errorf("ValidateProvidesTemplate(%q) = %v, want %v", tt.tmpl, got, tt.want)
		}
	}
}

func TestPortMapFromMappings(t *testing.T) {
	mappings := []string{"22718:2718", "28080:8080", "127.0.0.1:23000:3000"}
	m := PortMapFromMappings(mappings)
	if m[2718] != 22718 {
		t.Errorf("portMap[2718] = %d, want 22718", m[2718])
	}
	if m[8080] != 28080 {
		t.Errorf("portMap[8080] = %d, want 28080", m[8080])
	}
	if m[3000] != 23000 {
		t.Errorf("portMap[3000] = %d, want 23000 (IP:H:C form)", m[3000])
	}
}
