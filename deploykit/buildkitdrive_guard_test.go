package deploykit

// buildkitdrive_guard_test.go — the live precondition for the emit path.

import (
	"os/exec"
	"testing"
)

// requireBuildkitDrive skips VISIBLY when the BuildKit toolchain is absent.
//
// The VM-box emit path builds through BuildKit now, so its live tests need the drive's
// tools and not only an engine plus qemu-img. CI has the latter two and NOT this toolchain
// — measured there as `exec: "rootlesskit": executable file not found in $PATH`, with every
// other assertion in the test having passed. A boundary that crosses a real service SKIPS
// VISIBLY when its requirement is absent: it never fails as though the code under test were
// broken, and it is never mocked.
func requireBuildkitDrive(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"rootlesskit", "buildkitd", "buildctl"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed — skipping the live BuildKit emit path: %v", tool, err)
		}
	}
}
