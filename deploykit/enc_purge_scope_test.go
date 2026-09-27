package deploykit

import (
	"os"
	"path/filepath"
	"testing"
)

// mkEncVol creates a real encrypted-volume dir (`<volDir>/cipher` + `/plain`)
// under base, so a purge has real content to remove.
func mkEncVol(t *testing.T, base, name string) string {
	t.Helper()
	d := filepath.Join(base, name)
	for _, sub := range []string{"cipher", "plain"} {
		if err := os.MkdirAll(filepath.Join(d, sub), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, sub, "data"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

// TestRemoveEncryptedVolumesUnder_SiblingSafe drives the ACTUAL changed runtime
// path (removeEncryptedVolumesUnder, the core RemoveEncryptedVolumes delegates
// to) against a real filesystem, and fails without the sibling filter: with the
// pre-fix prefix-only match, purging the base deploy deleted an instance's
// cipher/plain dirs too. The config is SUPPLIED (as the out-of-process caller
// must, since the bare LoadDeployConfig is a silent no-op there).
func TestRemoveEncryptedVolumesUnder_SiblingSafe(t *testing.T) {
	base := t.TempDir()
	baseSecrets := mkEncVol(t, base, "charly-githubrunner-secrets")
	inst1 := mkEncVol(t, base, "charly-githubrunner-no-1-secrets")
	inst2 := mkEncVol(t, base, "charly-githubrunner-no-2-secrets")
	unrelated := mkEncVol(t, base, "charly-other-secrets")

	dc := &DeployConfig{Deploy: map[string]DeployNode{
		"githubrunner":      {},
		"githubrunner/no-1": {},
		"githubrunner/no-2": {},
		"other":             {},
	}}

	removeEncryptedVolumesUnder(base, "githubrunner", "", dc)

	mustExist := func(d string, want bool) {
		t.Helper()
		_, err := os.Stat(d)
		if got := err == nil; got != want {
			t.Errorf("%s exists=%v, want %v", filepath.Base(d), got, want)
		}
	}
	mustExist(baseSecrets, false) // base's own dir is purged
	mustExist(inst1, true)        // sibling instance SURVIVES — the regression
	mustExist(inst2, true)
	mustExist(unrelated, true)
}

// TestRemoveEncryptedVolumesUnder_OrphanFallsBackToPrefix pins the
// orphaned-deploy contract: a nil config (unreadable/absent) means no siblings
// are known, so the deploy's own prefix is the only signal and its dirs are
// still purged.
func TestRemoveEncryptedVolumesUnder_OrphanFallsBackToPrefix(t *testing.T) {
	base := t.TempDir()
	own := filepath.Join(base, "charly-app-secrets")
	other := filepath.Join(base, "charly-other-secrets")
	for _, d := range []string{own, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	removeEncryptedVolumesUnder(base, "app", "", nil)

	if _, err := os.Stat(own); err == nil {
		t.Error("orphan path did not purge the deploy's own dir")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("orphan path removed an unrelated deploy's dir")
	}
}
