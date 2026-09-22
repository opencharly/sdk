package loaderkit

// test_main_test.go — isolate the CONFIG STACK for every loaderkit test: the
// real /etc/charly/charly.yml must NOT leak into LoadUnified-based tests as the
// system layer (a real system project would add its nodes to every merged doc).
// Tests that exercise the stack override the env var themselves
// (config_stack_test.go).
//
// CHARLY_SYSTEM_CONFIG points at an absent file in the system temp dir, so
// readConfigStack skips the system layer and the stack reduces to the in-dir
// project. CHARLY_DEPLOY_CONFIG is ALSO pointed at an absent file, even though
// the per-host deploy overlay is no longer a stack layer (config_stack.go): a
// loaderkit test that reaches the overlay through its designed per-field merge
// (deploykit.MergedDeployTree / ResolveMergedTreeViaExecutor) must still not pick
// up the operator's real ~/.config/charly/charly.yml.

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
