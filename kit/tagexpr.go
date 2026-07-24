package kit

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Tag expressions — the `--tag` / `--tag-exclude` filter surface, shared by the check
// engine and any plugin candy that filters a plan by tag.
//
// Grammar (Cucumber-compatible minimal subset):
//
//	disjunction = conjunction ( "or" conjunction )*
//	conjunction = atom ( "and" atom )*
//	atom        = "@" IDENT | "not" atom | "(" disjunction ")"
//
// A leading '@' is optional, so `--tag smoke` and `--tag @smoke` behave identically.
// The empty expression matches every tag set (a default that filters nothing out).

// TagExpr is an opaque compiled tag expression. Nil means "match everything" (no
// filter), so callers can write `if expr.Match(tags)` without a nil check.
type TagExpr struct {
	node tagNode
	raw  string
}

// String returns the raw source the expression was compiled from.
func (t *TagExpr) String() string {
	if t == nil {
		return ""
	}
	return t.raw
}

// Match reports whether the tag set satisfies the expression. A nil TagExpr matches
// everything.
func (t *TagExpr) Match(tags []string) bool {
	if t == nil || t.node == nil {
		return true
	}
	set := make(map[string]bool, len(tags))
	for _, tag := range tags {
		set[NormalizeTag(tag)] = true
	}
	return t.node.check(set)
}

// ValidateTagExpr syntax-checks an optional --tag expression (rejecting a malformed one)
// without keeping the parsed *TagExpr — moved from charly/check_feature_run.go (CHECK-wave), a
// pure wrapper with zero core-state coupling. It does NOT apply the parsed expression as a step
// filter: kit.RunPlan (the walk both hostFeatureBox, still core, and
// candy/plugin-check's pluginCheckRunFeatureLive drive) takes no tag-filter parameter and walks
// every step unconditionally — a confirmed, RCA'd, non-blocking gap (per-tag filtering was never
// wired past this parse), routed to the next check-correctness thematic batch.
func ValidateTagExpr(tag string) error {
	if strings.TrimSpace(tag) == "" {
		return nil
	}
	_, err := ParseTagExpr(tag)
	return err
}

// ParseTagExpr compiles a tag expression. Empty / whitespace input produces a nil
// TagExpr that matches everything. A syntax error is returned rather than silently
// matching or silently failing.
func ParseTagExpr(src string) (*TagExpr, error) {
	trimmed := strings.TrimSpace(src)
	if trimmed == "" {
		return nil, nil
	}
	toks, err := lexTagExpr(trimmed)
	if err != nil {
		return nil, err
	}
	p := &tagParser{toks: toks}
	node, err := p.parseDisjunction()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.toks) {
		return nil, fmt.Errorf("unexpected token %q at position %d", p.toks[p.pos].value, p.pos)
	}
	return &TagExpr{node: node, raw: trimmed}, nil
}

// CombineTagFilters composes an include-filter and an exclude-filter into one
// effective expression. Either side may be nil:
//
//   - include nil, exclude nil  → always matches
//   - include X,   exclude nil  → matches when X is true
//   - include nil, exclude Y    → matches when Y is false
//   - include X,   exclude Y    → matches when X is true AND Y is false
func CombineTagFilters(include, exclude *TagExpr) *TagExpr {
	if include == nil && exclude == nil {
		return nil
	}
	var node tagNode
	switch {
	case include != nil && exclude != nil:
		node = &tagAnd{left: include.node, right: &tagNot{of: exclude.node}}
	case include != nil:
		node = include.node
	case exclude != nil:
		node = &tagNot{of: exclude.node}
	}
	raw := ""
	if include != nil {
		raw = include.raw
	}
	if exclude != nil {
		if raw != "" {
			raw = "(" + raw + ") and not (" + exclude.raw + ")"
		} else {
			raw = "not (" + exclude.raw + ")"
		}
	}
	return &TagExpr{node: node, raw: raw}
}

// NormalizeTag strips a single leading '@' so `@smoke` and `smoke` are identical —
// authors commonly write `@smoke` from Gherkin habit, but the sigil is optional in
// the YAML surface.
func NormalizeTag(t string) string {
	t = strings.TrimSpace(t)
	if strings.HasPrefix(t, "@") {
		return t[1:]
	}
	return t
}

// EffectiveTags normalizes and de-dups a step's tags, preserving first-seen order.
// (Per-step tags only — there is no group-level tag inheritance.)
func EffectiveTags(stepTags []string) []string {
	if len(stepTags) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(stepTags))
	var out []string
	for _, t := range stepTags {
		t = NormalizeTag(t)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// ---------------------------------------------------------------------------
// AST + evaluator
// ---------------------------------------------------------------------------

type tagNode interface {
	check(set map[string]bool) bool
}

type tagLeaf struct{ name string }

func (l *tagLeaf) check(set map[string]bool) bool { return set[l.name] }

type tagNot struct{ of tagNode }

func (n *tagNot) check(set map[string]bool) bool { return !n.of.check(set) }

type tagAnd struct{ left, right tagNode }

func (a *tagAnd) check(set map[string]bool) bool {
	return a.left.check(set) && a.right.check(set)
}

type tagOr struct{ left, right tagNode }

func (o *tagOr) check(set map[string]bool) bool {
	return o.left.check(set) || o.right.check(set)
}

// ---------------------------------------------------------------------------
// Lexer + parser
// ---------------------------------------------------------------------------

type tagTokenKind int

const (
	tokIdent tagTokenKind = iota
	tokAnd
	tokOr
	tokNot
	tokLParen
	tokRParen
)

type tagToken struct {
	kind  tagTokenKind
	value string
}

// isTagIdentStart reports whether r may BEGIN a tag identifier (after the optional
// leading '@'). Deliberately NARROWER than isTagIdentRune: `-`, `.` and `:` may appear
// INSIDE a tag (`ci-smoke`, `v1.2`, `fedora:43`) but may not start one, so `a and -b`
// is a lexer error rather than a silently-accepted expression. Keeping the two
// predicates distinct is the point — collapsing them widens the grammar.
func isTagIdentStart(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// isTagIdentRune reports whether r may appear INSIDE a tag identifier, after its first
// character. Letters and digits are Unicode-aware; the punctuation set is what tag names
// actually carry.
func isTagIdentRune(r rune) bool {
	return isTagIdentStart(r) || r == '-' || r == '.' || r == ':'
}

func lexTagExpr(src string) ([]tagToken, error) {
	var out []tagToken
	i := 0
	for i < len(src) {
		// Decode a full RUNE, not a byte: `rune(src[i])` mangles every multi-byte
		// codepoint into its lead byte, which unicode.IsLetter then rejects — so a
		// non-ASCII tag failed with "unexpected character" despite the Unicode-aware
		// predicates directly below it.
		r, size := utf8.DecodeRuneInString(src[i:])
		if r == utf8.RuneError && size <= 1 {
			return nil, fmt.Errorf("tag expression: invalid UTF-8 at position %d", i)
		}
		switch {
		case unicode.IsSpace(r):
			i += size
		case r == '(':
			out = append(out, tagToken{kind: tokLParen, value: "("})
			i += size
		case r == ')':
			out = append(out, tagToken{kind: tokRParen, value: ")"})
			i += size
		case r == '@' || isTagIdentStart(r):
			start := i
			if r == '@' {
				i += size
			}
			for i < len(src) {
				c, csize := utf8.DecodeRuneInString(src[i:])
				if c == utf8.RuneError && csize <= 1 {
					return nil, fmt.Errorf("tag expression: invalid UTF-8 at position %d", i)
				}
				if !isTagIdentRune(c) {
					break
				}
				i += csize
			}
			word := src[start:i]
			switch strings.ToLower(word) {
			case "and":
				out = append(out, tagToken{kind: tokAnd, value: "and"})
			case "or":
				out = append(out, tagToken{kind: tokOr, value: "or"})
			case "not":
				out = append(out, tagToken{kind: tokNot, value: "not"})
			default:
				out = append(out, tagToken{kind: tokIdent, value: NormalizeTag(word)})
			}
		default:
			return nil, fmt.Errorf("tag expression: unexpected character %q at position %d", r, i)
		}
	}
	return out, nil
}

type tagParser struct {
	toks []tagToken
	pos  int
}

func (p *tagParser) peek() (tagToken, bool) {
	if p.pos >= len(p.toks) {
		return tagToken{}, false
	}
	return p.toks[p.pos], true
}

func (p *tagParser) consume() tagToken { //nolint:unparam // parser API: returns the consumed token for callers that need it
	t := p.toks[p.pos]
	p.pos++
	return t
}

func (p *tagParser) parseDisjunction() (tagNode, error) {
	left, err := p.parseConjunction()
	if err != nil {
		return nil, err
	}
	for {
		t, ok := p.peek()
		if !ok || t.kind != tokOr {
			break
		}
		p.consume()
		right, err := p.parseConjunction()
		if err != nil {
			return nil, err
		}
		left = &tagOr{left: left, right: right}
	}
	return left, nil
}

func (p *tagParser) parseConjunction() (tagNode, error) {
	left, err := p.parseAtom()
	if err != nil {
		return nil, err
	}
	for {
		t, ok := p.peek()
		if !ok || t.kind != tokAnd {
			break
		}
		p.consume()
		right, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		left = &tagAnd{left: left, right: right}
	}
	return left, nil
}

func (p *tagParser) parseAtom() (tagNode, error) {
	t, ok := p.peek()
	if !ok {
		return nil, fmt.Errorf("tag expression: unexpected end of input")
	}
	switch t.kind {
	case tokNot:
		p.consume()
		inner, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		return &tagNot{of: inner}, nil
	case tokLParen:
		p.consume()
		inner, err := p.parseDisjunction()
		if err != nil {
			return nil, err
		}
		closeTok, ok := p.peek()
		if !ok || closeTok.kind != tokRParen {
			return nil, fmt.Errorf("tag expression: expected ')' but got %q", closeTok.value)
		}
		p.consume()
		return inner, nil
	case tokIdent:
		p.consume()
		return &tagLeaf{name: t.value}, nil
	default:
		return nil, fmt.Errorf("tag expression: unexpected token %q", t.value)
	}
}
