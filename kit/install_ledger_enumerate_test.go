package kit

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The ledger relocation removed the directory layout consumers enumerated by listing
// *.json stems under LedgerPaths.Deploys/.Candies. These tests pin the replacement:
// without ListDeployIDs/ListCandyNames a consumer can only read an id it already knows,
// so it cannot discover what is deployed -- which is how a stale consumer ends up
// reporting "nothing deployed" as though it were the correct answer.

func writeLedgerFixture(t *testing.T, body string) *LedgerPaths {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "charly.yml")
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return &LedgerPaths{ConfigFile: cfg, LockFile: cfg + ".lock"}
}

func TestListDeployIDs_EnumeratesAndSorts(t *testing.T) {
	paths := writeLedgerFixture(t, `
version: "1"
ledger:
  deploys:
    zeta-pod:
      schema_version: "1"
    alpha-pod:
      schema_version: "1"
  candies:
    layer-socat:
      schema_version: "1"
`)
	got := ListDeployIDs(paths)
	want := []string{"alpha-pod", "zeta-pod"}
	if !slices.Equal(got, want) {
		t.Errorf("ListDeployIDs = %v, want %v", got, want)
	}
}

func TestListCandyNames_EnumeratesAndSorts(t *testing.T) {
	paths := writeLedgerFixture(t, `
ledger:
  candies:
    layer-zsh:
      schema_version: "1"
    layer-abc:
      schema_version: "1"
`)
	got := ListCandyNames(paths)
	want := []string{"layer-abc", "layer-zsh"}
	if !slices.Equal(got, want) {
		t.Errorf("ListCandyNames = %v, want %v", got, want)
	}
}

// An absent file is "nothing deployed", not an error -- matching readLedger's
// best-effort contract and the old empty-directory case.
func TestListers_AbsentFileIsEmptyNotPanic(t *testing.T) {
	paths := &LedgerPaths{
		ConfigFile: filepath.Join(t.TempDir(), "does-not-exist.yml"),
	}
	if got := ListDeployIDs(paths); len(got) != 0 {
		t.Errorf("ListDeployIDs on absent file = %v, want empty", got)
	}
	if got := ListCandyNames(paths); len(got) != 0 {
		t.Errorf("ListCandyNames on absent file = %v, want empty", got)
	}
}

// A ledger with entries must NOT read as empty -- this is the discriminating case
// against the silent-empty failure the relocation otherwise invites.
func TestListDeployIDs_PopulatedLedgerIsNotEmpty(t *testing.T) {
	paths := writeLedgerFixture(t, `
ledger:
  deploys:
    only-pod:
      schema_version: "1"
`)
	if got := ListDeployIDs(paths); len(got) == 0 {
		t.Fatal("populated ledger enumerated as empty -- the silent-wrong-answer shape")
	}
}
