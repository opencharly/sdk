package sdk

import (
	"testing"
)

// TestMatchers exercises the exported matcher surface shared by the core check
// runner and out-of-tree verb plugins: MatchAll dispatch across the operator
// classes, plus the MatchValueString / MatchValueStrings coercion helpers.
func TestMatchers(t *testing.T) {
	t.Run("MatchAll", func(t *testing.T) {
		cases := []struct {
			name    string
			value   string
			matcher Matcher
			wantErr bool
		}{
			{"equals pass", "hello", Matcher{Op: "equals", Value: "hello"}, false},
			{"equals fail", "hello", Matcher{Op: "equals", Value: "world"}, true},
			{"not_equals pass", "hello", Matcher{Op: "not_equals", Value: "world"}, false},
			{"not_equals fail", "hello", Matcher{Op: "not_equals", Value: "hello"}, true},
			{"contains pass", "hello world", Matcher{Op: "contains", Value: "world"}, false},
			{"contains fail", "hello world", Matcher{Op: "contains", Value: "xyz"}, true},
			{"not_contains pass", "hello", Matcher{Op: "not_contains", Value: "xyz"}, false},
			{"not_contains fail", "hello", Matcher{Op: "not_contains", Value: "ell"}, true},
			{"matches pass", "abc123", Matcher{Op: "matches", Value: `\d+`}, false},
			{"matches fail", "abc", Matcher{Op: "matches", Value: `\d+`}, true},
			{"lt pass", "5", Matcher{Op: "lt", Value: "10"}, false},
			{"lt fail", "10", Matcher{Op: "lt", Value: "5"}, true},
			{"ge pass equal", "5", Matcher{Op: "ge", Value: "5"}, false},
			{"ge fail", "4", Matcher{Op: "ge", Value: "5"}, true},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				err := MatchAll(tc.value, []Matcher{tc.matcher})
				if tc.wantErr && err == nil {
					t.Errorf("expected error, got nil")
				}
				if !tc.wantErr && err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			})
		}

		// MatchAll requires EVERY matcher to pass; the first failure wins.
		if err := MatchAll("hello world", []Matcher{
			{Op: "contains", Value: "hello"},
			{Op: "contains", Value: "world"},
		}); err != nil {
			t.Errorf("all-pass list: unexpected error: %v", err)
		}
		if err := MatchAll("hello world", []Matcher{
			{Op: "contains", Value: "hello"},
			{Op: "contains", Value: "nope"},
		}); err == nil {
			t.Errorf("expected error when one matcher fails")
		}
	})

	t.Run("MatchValueString", func(t *testing.T) {
		cases := []struct {
			name string
			in   any
			want string
		}{
			{"string", "hi", "hi"},
			{"int", 42, "42"},
			{"float64", 3.5, "3.5"},
			{"bool", true, "true"},
			{"nil", nil, ""},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if got := MatchValueString(tc.in); got != tc.want {
					t.Errorf("MatchValueString(%v) = %q, want %q", tc.in, got, tc.want)
				}
			})
		}
	})

	t.Run("MatchValueStrings", func(t *testing.T) {
		if got := MatchValueStrings("solo"); len(got) != 1 || got[0] != "solo" {
			t.Errorf("scalar: got %v, want [solo]", got)
		}
		got := MatchValueStrings([]any{"a", 2, true})
		want := []string{"a", "2", "true"}
		if len(got) != len(want) {
			t.Fatalf("list: got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("list[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})
}
