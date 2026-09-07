package loaderkit

// test_main_test.go — isolate the CONFIG STACK for every loaderkit test: the
// real per-host ~/.config/charly/charly.yml (and any /etc/charly/charly.yml)
// must NOT leak into LoadUnified-based tests as the user/system layer (a real
// deploy config would add its deploy nodes to every merged doc). Tests that
// exercise the stack override the env vars themselves (config_stack_test.go).
//
// CHARLY_SYSTEM_CONFIG + CHARLY_DEPLOY_CONFIG point at absent files in the
// system temp dir, so readConfigStack skips both layers and the stack reduces
// to the in-dir project — the pre-stack behavior.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/opencharly/spec/spec"
)

func TestMain(m *testing.M) {
	tmp := os.TempDir()
	os.Setenv(SystemConfigEnv, filepath.Join(tmp, "charly-test-absent-system.yml"))
	os.Setenv(spec.DeployConfigEnv, filepath.Join(tmp, "charly-test-absent-user.yml"))
	code := m.Run()
	os.Exit(code)
}
