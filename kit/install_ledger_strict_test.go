package kit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The non-strict listers treat an unparseable charly.yml exactly like an absent one,
// which makes a malformed ledger indistinguishable from "nothing deployed yet". These
// tests pin the strict variants' contract: absent is still fine, malformed is loud.

func strictFixture(t *testing.T, body string) *LedgerPaths {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "charly.yml")
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return &LedgerPaths{ConfigFile: cfg, LockFile: cfg + ".lock"}
}

func TestListStrict_MalformedLedgerIsAnError(t *testing.T) {
	paths := strictFixture(t, "ledger: [this is not a mapping\n")

	if _, err := ListDeployIDsStrict(paths); err == nil {
		t.Error("ListDeployIDsStrict: malformed ledger returned nil error")
	} else if !strings.Contains(err.Error(), "parse ledger") {
		t.Errorf("error does not name the cause: %v", err)
	}
	if _, err := ListCandyNamesStrict(paths); err == nil {
		t.Error("ListCandyNamesStrict: malformed ledger returned nil error")
	}

	// The discriminating contrast: the non-strict listers report the SAME file as
	// simply empty, which is precisely why the strict variants exist.
	if got := ListDeployIDs(paths); len(got) != 0 {
		t.Errorf("ListDeployIDs = %v, want empty", got)
	}
}

func TestListStrict_AbsentLedgerIsNotAnError(t *testing.T) {
	paths := &LedgerPaths{ConfigFile: filepath.Join(t.TempDir(), "absent.yml")}

	ids, err := ListDeployIDsStrict(paths)
	if err != nil {
		t.Errorf("absent ledger must not error: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("ids = %v, want empty", ids)
	}
	names, err := ListCandyNamesStrict(paths)
	if err != nil {
		t.Errorf("absent ledger must not error: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("names = %v, want empty", names)
	}
}

func TestListStrict_WellFormedMatchesNonStrict(t *testing.T) {
	paths := strictFixture(t, `
ledger:
  deploys:
    zeta:
      schema_version: "1"
    alpha:
      schema_version: "1"
  candies:
    layer-b:
      schema_version: "1"
    layer-a:
      schema_version: "1"
`)
	ids, err := ListDeployIDsStrict(paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Join(ids, ",") != "alpha,zeta" {
		t.Errorf("ids = %v, want [alpha zeta]", ids)
	}
	names, err := ListCandyNamesStrict(paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Join(names, ",") != "layer-a,layer-b" {
		t.Errorf("names = %v, want [layer-a layer-b]", names)
	}
}
