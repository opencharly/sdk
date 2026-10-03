package workflowkit

import "testing"

// expr_test.go — the lobster expression grammar. The valid/invalid tables are ported
// from upstream lobster's own expression tests (load.ts / expressions), so the grammar
// this package enforces is the grammar lobster accepts, not a plausible invention.

func TestEvalLobsterExpressions(t *testing.T) {
	scope := Scope{
		"fetch":   `{"json":{"name":"ok","n":3,"flag":true}}`,
		"confirm": `{"approved":true}`,
		"deny":    `{"approved":false}`,
	}
	cases := []struct {
		name string
		expr string
		want bool
	}{
		{"literal true", "true", true},
		{"literal false", "false", false},
		{"ref bool field", "$confirm.approved", true},
		{"ref bool field false", "$deny.approved", false},
		{"number compare", "$fetch.json.n > 1", true},
		{"number compare false", "$fetch.json.n < 1", false},
		{"string compare", `$fetch.json.name == "ok"`, true},
		{"bare RHS identifier is a string", "open == open", true},
		{"bare RHS identifier mismatches", "open == closed", false},
		{"missing path is null", "$fetch.json.missing == null", true},
		{"missing path is not null otherwise", "$fetch.json.name != null", true},
		{"logical or", "$deny.approved || $confirm.approved", true},
		{"word and", "$confirm.approved and $fetch.json.flag", true},
		{"word or false", "$deny.approved or false", false},
		{"negation", "!$deny.approved", true},
		{"word negation", "not $deny.approved", true},
		{"parenthesised", "($confirm.approved)", true},
		{"unknown step is null", "$nope.x == null", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Eval(tc.expr, scope)
			if err != nil {
				t.Fatalf("Eval(%q): %v", tc.expr, err)
			}
			if got != tc.want {
				t.Errorf("Eval(%q) = %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

func TestValidateRejectsNonLobsterGrammar(t *testing.T) {
	cases := []struct {
		name string
		expr string
	}{
		{"membership operator", "$a.b in [1, 2]"},
		{"arithmetic", "$a.b + 1 > 2"},
		{"builtin call", "len($a.b) > 1"},
		{"array literal", "$a.b == [1]"},
		{"map literal", "$a.b == {x: 1}"},
		{"conditional", "$a.b ? true : false"},
		{"member access on a bare word", "a.b == 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := Validate(tc.expr); err == nil {
				t.Errorf("Validate(%q) accepted a non-lobster expression", tc.expr)
			}
		})
	}
}

func TestValidateAcceptsLobsterGrammar(t *testing.T) {
	for _, e := range []string{
		"true",
		"$confirm.approved",
		`$fetch.json.name == "ok"`,
		"$a.b || $c.d",
		"!$a.b",
		"$a.b == null",
		"open == open",
	} {
		if err := Validate(e); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", e, err)
		}
	}
}

func TestRewriteRefs(t *testing.T) {
	got := RewriteRefs(`$fetch.json.name == "ok" && $a.b == 1`)
	want := `ref("fetch", "json.name") == "ok" && ref("a", "b") == 1`
	if got != want {
		t.Errorf("RewriteRefs = %q, want %q", got, want)
	}
}

func TestRefNamesAndIDs(t *testing.T) {
	if got := RefNames("$a.b && $c.d || $a.e"); len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Errorf("RefNames = %v, want [a c]", got)
	}
	if got := RefIDs([]string{"$a.b.c", "$d.e", "$a.f"}); len(got) != 2 || got[0] != "a" || got[1] != "d" {
		t.Errorf("RefIDs = %v, want [a d]", got)
	}
	if !HasRef("x $a.b") || HasRef("no refs here") {
		t.Error("HasRef misclassified")
	}
}
