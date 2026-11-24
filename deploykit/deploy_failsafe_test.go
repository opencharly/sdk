package deploykit

// deploy_failsafe_test.go — the fail-safe re-read GATES the write.
//
// SaveDeployConfig must refuse to overwrite a per-host config that is present but does not load:
// a writer holding a DEGRADED (empty) DeployConfig would otherwise truncate the user's recoverable
// deploy state. The re-check used to run before the document was read; it now runs inside
// perhost.Apply's mutation, so the check and the atomic replace are one critical section. This
// file proves the gate is live — with an unloadable config the write is refused and the bytes stay
// exactly as they were, while the same call with a loadable one writes (so the refusal cannot come
// from some other failure).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/sdk/kit"
)

func failSafeFixture(t *testing.T) string {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "charly.yml")
	t.Setenv(kit.DeployConfigEnv, dest)
	t.Cleanup(func() { DeployStateHost = nil })
	return dest
}

func TestSaveDeployConfigRefusesAnUnloadableConfig(t *testing.T) {
	dest := failSafeFixture(t)
	const before = "web:\n  pod:\n    image: keep-me\n"
	if err := os.WriteFile(dest, []byte(before), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	RegisterDeployStateHost(&StateHostMechanisms{
		LoadUnifiedDeployConfig: func(string) (*DeployConfig, error) {
			return nil, errors.New("schema gate: field not allowed")
		},
	})

	dc := &DeployConfig{Deploy: map[string]DeployNode{"new": {Image: "new"}}}
	err := SaveDeployConfig(dc, concMarshalNode, nil, context.Background())
	if err == nil {
		t.Fatal("SaveDeployConfig wrote over a present-but-unloadable config")
	}
	if !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Errorf("want the fail-safe refusal, got: %v", err)
	}
	got, rerr := os.ReadFile(dest)
	if rerr != nil {
		t.Fatalf("read: %v", rerr)
	}
	if string(got) != before {
		t.Fatalf("the fail-safe must leave the bytes untouched, got:\n%s", got)
	}
}

// The same call with a LOADABLE config writes: the refusal above is the fail-safe, not a broken
// save path.
func TestSaveDeployConfigWritesWhenTheConfigLoads(t *testing.T) {
	dest := failSafeFixture(t)
	RegisterDeployStateHost(&StateHostMechanisms{
		LoadUnifiedDeployConfig: func(string) (*DeployConfig, error) { return &DeployConfig{}, nil },
	})

	dc := &DeployConfig{Deploy: map[string]DeployNode{"new": {Image: "new"}}}
	if err := SaveDeployConfig(dc, concMarshalNode, nil, context.Background()); err != nil {
		t.Fatalf("SaveDeployConfig: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(got), "new:") {
		t.Fatalf("the write did not land:\n%s", got)
	}
}
