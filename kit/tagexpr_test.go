package kit

import "testing"

func TestNormalizeTag(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"smoke", "smoke"},
		{"@smoke", "smoke"},
		{"  @smoke  ", "smoke"},
		{"@@smoke", "@smoke"}, // only ONE leading sigil is stripped
		{"", ""},
		{"@", ""},
	} {
		if got := NormalizeTag(tc.in); got != tc.want {
			t.Errorf("NormalizeTag(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEffectiveTags_NormalizesDedupsPreservesOrder(t *testing.T) {
	got := EffectiveTags([]string{"@smoke", "slow", "smoke", "", "  @slow ", "gpu"})
	want := []string{"smoke", "slow", "gpu"}
	if len(got) != len(want) {
		t.Fatalf("EffectiveTags = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("EffectiveTags = %v, want %v", got, want)
		}
	}
	if EffectiveTags(nil) != nil {
		t.Error("EffectiveTags(nil) must be nil")
	}
}

func TestParseTagExpr_EmptyMatchesEverything(t *testing.T) {
	for _, src := range []string{"", "   ", "\t\n"} {
		e, err := ParseTagExpr(src)
		if err != nil {
			t.Fatalf("ParseTagExpr(%q): %v", src, err)
		}
		if e != nil {
			t.Fatalf("ParseTagExpr(%q) must be nil (match-everything), got %v", src, e)
		}
		if !e.Match([]string{"anything"}) || !e.Match(nil) {
			t.Errorf("a nil TagExpr must match every tag set")
		}
	}
}

func TestTagExpr_Match(t *testing.T) {
	cases := []struct {
		expr string
		tags []string
		want bool
	}{
		{"smoke", []string{"smoke"}, true},
		{"@smoke", []string{"smoke"}, true}, // sigil optional on BOTH sides
		{"smoke", []string{"@smoke"}, true}, // tag set is normalized too
		{"smoke", []string{"slow"}, false},
		{"smoke and slow", []string{"smoke", "slow"}, true},
		{"smoke and slow", []string{"smoke"}, false},
		{"smoke or slow", []string{"slow"}, true},
		{"smoke or slow", []string{"gpu"}, false},
		{"not smoke", []string{"slow"}, true},
		{"not smoke", []string{"smoke"}, false},
		{"not not smoke", []string{"smoke"}, true}, // double negation
		// Precedence: `and` binds tighter than `or`.
		{"a and b or c", []string{"c"}, true},
		{"a and b or c", []string{"a"}, false},
		{"a and (b or c)", []string{"a", "c"}, true},
		{"a and (b or c)", []string{"a"}, false},
		// `not` binds to the atom, not the conjunction.
		{"not a and b", []string{"b"}, true},
		{"not a and b", []string{"a", "b"}, false},
		// Identifier punctuation actually used by tags.
		{"fedora:43", []string{"fedora:43"}, true},
		{"ci-smoke", []string{"ci-smoke"}, true},
		{"v1.2", []string{"v1.2"}, true},
		// Keywords are case-insensitive.
		{"a AND b", []string{"a", "b"}, true},
		{"a OR b", []string{"b"}, true},
		{"NOT a", []string{"b"}, true},
	}
	for _, tc := range cases {
		e, err := ParseTagExpr(tc.expr)
		if err != nil {
			t.Fatalf("ParseTagExpr(%q): %v", tc.expr, err)
		}
		if got := e.Match(tc.tags); got != tc.want {
			t.Errorf("ParseTagExpr(%q).Match(%v) = %v, want %v", tc.expr, tc.tags, got, tc.want)
		}
	}
}

func TestParseTagExpr_SyntaxErrors(t *testing.T) {
	for _, src := range []string{
		"and",   // leading binary operator
		"a and", // trailing binary operator
		"(a",    // unclosed paren
		"a)",    // unexpected close — trailing token
		"not",   // dangling not
		"a b",   // juxtaposition without an operator
		"a & b", // unexpected character
	} {
		if _, err := ParseTagExpr(src); err == nil {
			t.Errorf("ParseTagExpr(%q) must fail, got nil error", src)
		}
	}
}

func TestTagExpr_String_ReturnsRawSource(t *testing.T) {
	e, err := ParseTagExpr("  smoke and not slow  ")
	if err != nil {
		t.Fatal(err)
	}
	if got := e.String(); got != "smoke and not slow" {
		t.Errorf("String() = %q, want the trimmed raw source", got)
	}
	var nilExpr *TagExpr
	if nilExpr.String() != "" {
		t.Error("nil TagExpr String() must be empty")
	}
}

func TestCombineTagFilters(t *testing.T) {
	mustParse := func(s string) *TagExpr {
		t.Helper()
		e, err := ParseTagExpr(s)
		if err != nil {
			t.Fatalf("ParseTagExpr(%q): %v", s, err)
		}
		return e
	}

	if CombineTagFilters(nil, nil) != nil {
		t.Error("nil+nil must combine to nil (match everything)")
	}

	// include only
	inc := CombineTagFilters(mustParse("smoke"), nil)
	if !inc.Match([]string{"smoke"}) || inc.Match([]string{"slow"}) {
		t.Error("include-only must match the included tag and nothing else")
	}

	// exclude only
	exc := CombineTagFilters(nil, mustParse("slow"))
	if !exc.Match([]string{"smoke"}) || exc.Match([]string{"slow"}) {
		t.Error("exclude-only must match everything except the excluded tag")
	}

	// include AND NOT exclude
	both := CombineTagFilters(mustParse("smoke"), mustParse("slow"))
	if !both.Match([]string{"smoke"}) {
		t.Error("smoke must match")
	}
	if both.Match([]string{"smoke", "slow"}) {
		t.Error("smoke+slow must be excluded")
	}
	if both.Match([]string{"gpu"}) {
		t.Error("gpu must not match (include not satisfied)")
	}
	if got, want := both.String(), "(smoke) and not (slow)"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// TestLexTagExpr_UnicodeIdentifier is the regression for the byte-vs-rune decoding bug.
//
// The lexer used to do `rune(src[i])`, which converts a single BYTE to a rune. For any
// multi-byte codepoint that yields the lead byte (e.g. 0xC3 for 'é'), which
// unicode.IsLetter rejects — so a non-ASCII tag failed with "unexpected character",
// even though the lexer's own predicates (unicode.IsLetter / IsDigit) are Unicode-aware.
//
// Against the old byte-decoding lexer this test FAILS with:
//
//	tag expression: unexpected character 'Ã' at position 0
func TestLexTagExpr_UnicodeIdentifier(t *testing.T) {
	for _, tag := range []string{"café", "日本語", "naïve-test"} {
		e, err := ParseTagExpr(tag)
		if err != nil {
			t.Fatalf("ParseTagExpr(%q): %v", tag, err)
		}
		if !e.Match([]string{tag}) {
			t.Errorf("ParseTagExpr(%q) must match its own tag", tag)
		}
		if e.Match([]string{"other"}) {
			t.Errorf("ParseTagExpr(%q) must not match an unrelated tag", tag)
		}
	}
}

func TestLexTagExpr_InvalidUTF8(t *testing.T) {
	if _, err := ParseTagExpr("\xff\xfe"); err == nil {
		t.Error("invalid UTF-8 must be a lex error, not a silent match")
	}
}

// TestLexTagExpr_IdentifierMayNotStartWithPunctuation locks the deliberately-narrow
// leading-character predicate.
//
// `-`, `.` and `:` may appear INSIDE a tag (`ci-smoke`, `v1.2`, `fedora:43`) but must not
// START one. Collapsing isTagIdentStart into isTagIdentRune widens the grammar so that
// `-b`, `.x`, `:y` and `a and -b` are SILENTLY ACCEPTED as valid expressions instead of
// erroring — which contradicts ParseTagExpr's own contract ("a syntax error is returned
// rather than silently matching"). This test fails against that widening.
func TestLexTagExpr_IdentifierMayNotStartWithPunctuation(t *testing.T) {
	for _, src := range []string{"-b", ".x", ":y", "a and -b", "a or .b", "not :c"} {
		if _, err := ParseTagExpr(src); err == nil {
			t.Errorf("ParseTagExpr(%q) must be a lex error: a tag may not START with '-', '.' or ':'", src)
		}
	}
	// ...but the same characters INSIDE an identifier remain valid.
	for _, src := range []string{"ci-smoke", "v1.2", "fedora:43", "a and b-c"} {
		if _, err := ParseTagExpr(src); err != nil {
			t.Errorf("ParseTagExpr(%q) must parse: punctuation is legal inside an identifier: %v", src, err)
		}
	}
}
