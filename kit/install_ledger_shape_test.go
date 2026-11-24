package kit

// install_ledger_shape_test.go — a WRONG-SHAPE `ledger:` section is a malformed ledger, not an
// empty one.
//
// The strict reader exists to tell "nothing deployed yet" apart from "this file is broken". The
// hand-rolled decode it replaced (yaml.Unmarshal into ledgerDoc) errored on valid-YAML-wrong-shape
// sections — `ledger: []`, a scalar `ledger:`, a sequence `ledger.deploys:` — and the shared
// ledgerFromRoot decoder must keep that. These cases are deliberately NOT unparseable YAML: the
// document parses, the shape is wrong.

import (
	"os"
	"strings"
	"testing"
)

func TestStrictReader_RefusesAWrongShapeLedger(t *testing.T) {
	cases := map[string]string{
		"ledger-is-a-sequence":  "ledger: []\n",
		"ledger-is-a-scalar":    "ledger: nope\n",
		"deploys-is-a-sequence": "ledger:\n  deploys: []\n",
		"candies-is-a-scalar":   "ledger:\n  candies: nope\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			paths := strictFixture(t, body)
			if _, err := ListDeployIDsStrict(paths); err == nil {
				t.Fatal("ListDeployIDsStrict: wrong-shape ledger returned nil error")
			} else if !strings.Contains(err.Error(), "parse ledger") {
				t.Errorf("error does not name the cause: %v", err)
			}
			if _, err := ListCandyNamesStrict(paths); err == nil {
				t.Fatal("ListCandyNamesStrict: wrong-shape ledger returned nil error")
			}
			// The best-effort contract is unchanged: the non-strict listers still degrade to empty
			// rather than erroring — which is exactly why the strict variants exist.
			if got := ListDeployIDs(paths); len(got) != 0 {
				t.Errorf("ListDeployIDs = %v, want empty", got)
			}
		})
	}
}

func TestWriter_RefusesAWrongShapeLedgerAndKeepsItsBytes(t *testing.T) {
	const body = "ledger: nope\n"
	paths := strictFixture(t, body)

	if err := DeleteDeployRecord(paths, "anything"); err == nil {
		t.Fatal("DeleteDeployRecord on a wrong-shape ledger returned nil; want a refusal")
	} else if !strings.Contains(err.Error(), "is not a mapping") {
		t.Errorf("error does not name the shape problem: %v", err)
	}
	got, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != body {
		t.Fatalf("the writer rewrote a malformed ledger instead of refusing it:\n%s", got)
	}
}

// An ABSENT ledger section is the empty ledger and must stay a non-error for both readers — the
// shape refusal must not turn a fresh host into a failure.
func TestLedgerReaders_AcceptAnAbsentSection(t *testing.T) {
	paths := strictFixture(t, "provides:\n  env: {}\n")
	if ids, err := ListDeployIDsStrict(paths); err != nil || len(ids) != 0 {
		t.Fatalf("ListDeployIDsStrict(absent ledger) = %v, %v; want empty, nil", ids, err)
	}
	if names, err := ListCandyNamesStrict(paths); err != nil || len(names) != 0 {
		t.Fatalf("ListCandyNamesStrict(absent ledger) = %v, %v; want empty, nil", names, err)
	}
}
