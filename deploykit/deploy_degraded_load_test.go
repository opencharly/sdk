package deploykit

// deploy_degraded_load_test.go — a DEGRADED load must never be persisted.
//
// The per-host config's loader can fail OPEN: an entry it cannot unify comes back as an empty or
// short DeployConfig with NO error, and a writer that then saves its own dc erases every entry the
// loader dropped. Measured on a host, 2026-11-24: `~/.config/charly/charly.yml` lost BOTH of its
// deploy entries (and `system:`) in one run, while the containers kept running.
//
// The guard compares the TYPED re-read with the file's own deploy-key count — equal for every
// legitimate write — so a deliberate removal still passes.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/sdk/kit"
)

func seedTwoEntries(t *testing.T) string {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "charly.yml")
	t.Setenv(kit.DeployConfigEnv, dest)
	seed := "web:\n  pod:\n    image: web\napi:\n  pod:\n    image: api\nsystem:\n  hostname: probe\n"
	if err := os.WriteFile(dest, []byte(seed), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return dest
}

func TestSaveDeployConfigRefusesADegradedLoad(t *testing.T) {
	dest := seedTwoEntries(t)
	before, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// The degraded loader: no error, but it accounts for none of the file's entries.
	RegisterDeployStateHost(&StateHostMechanisms{
		LoadUnifiedDeployConfig: func(string) (*DeployConfig, error) { return &DeployConfig{}, nil },
	})
	t.Cleanup(func() { DeployStateHost = nil })

	dc := &DeployConfig{Deploy: map[string]DeployNode{"new": {Image: "new"}}}
	err = SaveDeployConfig(dc, concMarshalNode, nil, context.Background())
	if err == nil {
		t.Fatal("SaveDeployConfig persisted a degraded load — the file's entries were erased")
	}
	if !strings.Contains(err.Error(), "degraded load") {
		t.Errorf("want the degraded-load refusal, got: %v", err)
	}
	after, rerr := os.ReadFile(dest)
	if rerr != nil {
		t.Fatalf("read after: %v", rerr)
	}
	if string(after) != string(before) {
		t.Fatalf("the refusal must leave the bytes untouched, got:\n%s", after)
	}
}

func TestSaveDeployConfigAllowsADeliberateRemoval(t *testing.T) {
	dest := seedTwoEntries(t)
	// A HEALTHY loader: it sees both entries, exactly as the file has them.
	RegisterDeployStateHost(&StateHostMechanisms{
		LoadUnifiedDeployConfig: func(string) (*DeployConfig, error) {
			return &DeployConfig{Deploy: map[string]DeployNode{
				"web": {Image: "web"},
				"api": {Image: "api"},
			}}, nil
		},
	})
	t.Cleanup(func() { DeployStateHost = nil })

	// The caller deliberately removed `api`: dc carries the remaining set.
	dc := &DeployConfig{Deploy: map[string]DeployNode{"web": {Image: "web"}}}
	if err := SaveDeployConfig(dc, concMarshalNode, nil, context.Background()); err != nil {
		t.Fatalf("a deliberate removal must still write: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(got), "api:") {
		t.Fatalf("the removal did not land:\n%s", got)
	}
	if !strings.Contains(string(got), "web:") || !strings.Contains(string(got), "system:") {
		t.Fatalf("the write must keep the remaining entry and the local-state sections:\n%s", got)
	}
}

func TestSaveDeployConfigWritesOnAFreshHost(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "charly.yml")
	t.Setenv(kit.DeployConfigEnv, dest)
	RegisterDeployStateHost(&StateHostMechanisms{
		LoadUnifiedDeployConfig: func(string) (*DeployConfig, error) { return nil, nil },
	})
	t.Cleanup(func() { DeployStateHost = nil })

	dc := &DeployConfig{Deploy: map[string]DeployNode{"first": {Image: "first"}}}
	if err := SaveDeployConfig(dc, concMarshalNode, nil, context.Background()); err != nil {
		t.Fatalf("a fresh host (no file) must write: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(got), "first:") {
		t.Fatalf("the write did not land:\n%s", got)
	}
}
