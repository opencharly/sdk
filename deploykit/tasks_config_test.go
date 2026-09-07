package deploykit

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// --- SubstituteContent (the config: verb's generate-time substitution) ---

func TestSubstituteContent_ResolvesVarsAndAutoExports(t *testing.T) {
	img := testResolvedBox()
	vars := map[string]string{"CBX_CFG_PROVIDER": "local-container"}
	out, err := SubstituteContent("provider: ${CBX_CFG_PROVIDER}\nhome: ${HOME}\nuser: ${USER}\n", vars, img)
	if err != nil {
		t.Fatalf("SubstituteContent: %v", err)
	}
	if !strings.Contains(out, "provider: local-container") {
		t.Errorf("candy var not substituted:\n%s", out)
	}
	if !strings.Contains(out, "home: /home/user") {
		t.Errorf("HOME auto-export not substituted:\n%s", out)
	}
	if !strings.Contains(out, "user: user") {
		t.Errorf("USER auto-export not substituted:\n%s", out)
	}
}

func TestSubstituteContent_UnresolvedRefIsHardError(t *testing.T) {
	_, err := SubstituteContent("a: ${MISSING_VAR}\n", map[string]string{"A": "b"}, testResolvedBox())
	if err == nil {
		t.Fatal("unresolved reference must be a hard error")
	}
	if !strings.Contains(err.Error(), "MISSING_VAR") {
		t.Errorf("error should name the unresolved ref, got: %v", err)
	}
}

func TestSubstituteContent_LowercaseDoesNotMatch(t *testing.T) {
	out, err := SubstituteContent("esc: ${lower}\n", nil, testResolvedBox())
	if err != nil {
		t.Fatalf("SubstituteContent: %v", err)
	}
	if !strings.Contains(out, "${lower}") {
		t.Errorf("lowercase ref must pass through verbatim, got:\n%s", out)
	}
}

// --- EmitTasks config: case ---

func TestEmitTasks_ConfigVerbRendersSubstitutedContent(t *testing.T) {
	dir := t.TempDir()
	g := NewRenderGenerator()
	g.BuildDir = dir
	ops := []spec.Op{
		{Config: "/etc/crabbox.yaml", Content: "provider: ${CBX_CFG_PROVIDER}\nnoHostname: true\n", Mode: "0600", RunAs: "root"},
	}
	layer := testCandy("lyr", spec.CandyModel{Vars: map[string]string{"CBX_CFG_PROVIDER": "local-container"}}, spec.CandyView{})
	var b strings.Builder
	if _, err := g.EmitTasks(&b, layer, testResolvedBox(), ops, dir, ".build/test-img"); err != nil {
		t.Fatalf("EmitTasks: %v", err)
	}
	out := b.String()
	// The staged file carries the SUBSTITUTED content (content-addressed path).
	subst := "provider: local-container\nnoHostname: true\n"
	sum := sha256.Sum256([]byte(subst))
	hexSum := hex.EncodeToString(sum[:])
	src := ".build/test-img/_inline/lyr/" + hexSum
	want := "COPY --chmod=0600 " + src + " /etc/crabbox.yaml"
	if !strings.Contains(out, want) {
		t.Errorf("config should COPY the substituted staged file:\n%s", out)
	}
	// No heredoc / RUN must render the file body (COPY only — the write safety contract).
	if strings.Contains(out, "provider: local-container\nnoHostname") && strings.Contains(out, "RUN cat") {
		t.Errorf("config must never emit a heredoc RUN: got %s", out)
	}
}

func TestEmitTasks_ConfigVerbValidateGatesBytes(t *testing.T) {
	dir := t.TempDir()
	g := NewRenderGenerator()
	g.BuildDir = dir
	g.ValidateEgress = func(kind, label string, data []byte) error {
		if kind == "crabbox-yaml" {
			return errors.New("bad yaml: boom")
		}
		return nil
	}
	ops := []spec.Op{
		{Config: "/etc/crabbox.yaml", Content: "x: y\n", Validate: "crabbox-yaml", RunAs: "user"},
	}
	layer := testCandy("lyr", spec.CandyModel{}, spec.CandyView{})
	var b strings.Builder
	_, err := g.EmitTasks(&b, layer, testResolvedBox(), ops, dir, ".build/test-img")
	if err == nil {
		t.Fatal("validate: rejection must fail EmitTasks")
	}
	if !strings.Contains(err.Error(), "crabbox-yaml") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("error should carry the schema name + egress reason, got: %v", err)
	}
}

func TestEmitTasks_ConfigVerbNoValidateSkipsEgress(t *testing.T) {
	dir := t.TempDir()
	g := NewRenderGenerator()
	g.BuildDir = dir
	called := false
	g.ValidateEgress = func(_, _ string, _ []byte) error {
		called = true
		return nil
	}
	ops := []spec.Op{
		{Config: "/etc/x.yml", Content: "a: b\n", RunAs: "user"},
	}
	layer := testCandy("lyr", spec.CandyModel{}, spec.CandyView{})
	var b strings.Builder
	if _, err := g.EmitTasks(&b, layer, testResolvedBox(), ops, dir, ".build/test-img"); err != nil {
		t.Fatalf("EmitTasks: %v", err)
	}
	if called {
		t.Fatal("validate: unset must not invoke the egress seam")
	}
}
