package deploykit

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// TestLoadDeployConfig_CtxRunEnvSelectsOverlay is the in-tree test for the ctx threading: an
// invocation's ctx RunEnv (spec.RunEnv) names its OWN overlay, and LoadDeployConfig resolves THAT
// path. Without the ctx param the read falls through to the process env / the operator's real
// overlay — the cross-bed bleed the roster cutover closes (plan §4.2 / RCA issue 1). It observes
// the resolved path through the DeployStateHost seam (which receives the configDir derived).
func TestLoadDeployConfig_CtxRunEnvSelectsOverlay(t *testing.T) {
	operatorDir := t.TempDir()
	t.Setenv(kit.DeployConfigEnv, filepath.Join(operatorDir, "charly.yml"))
	bedDir := t.TempDir()
	bedPath := filepath.Join(bedDir, "charly.yml")

	var seenDir string
	RegisterDeployStateHost(&StateHostMechanisms{
		LoadUnifiedDeployConfig: func(configDir string) (*DeployConfig, error) {
			seenDir = configDir
			return &DeployConfig{Deploy: map[string]DeployNode{}}, nil
		},
	})
	t.Cleanup(func() { DeployStateHost = nil })

	ctx := spec.WithRunEnv(context.Background(), spec.RunEnv{kit.DeployConfigEnv: bedPath})
	if _, err := LoadDeployConfig(ctx); err != nil {
		t.Fatalf("LoadDeployConfig(ctx): %v", err)
	}
	if seenDir != bedDir {
		t.Fatalf("LoadDeployConfig(ctx) resolved dir %q, want %q (ctx RunEnv must win)", seenDir, bedDir)
	}
	if _, err := LoadDeployConfig(context.Background()); err != nil {
		t.Fatalf("LoadDeployConfig(bg): %v", err)
	}
	if seenDir != operatorDir {
		t.Fatalf("LoadDeployConfig(bg) resolved dir %q, want %q (process env fallback)", seenDir, operatorDir)
	}
}
