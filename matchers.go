package sdk

import (
	"github.com/opencharly/spec/matchers"
	"github.com/opencharly/spec/spec"
)

// matchers.go — re-export of the goss-style matcher evaluation engine (sliced to
// github.com/opencharly/spec/matchers in the strict-cleanup cutover, #55 import-purity). The
// matcher VALUE types (Matcher, MatcherList) stay in spec/spec (union_types.go,
// #Matcher / #MatcherList); the evaluation logic (MatchAll + the private matchOne/matchNumeric
// + MatchValueString(s)) lives in spec/matchers so the value type and its logic stay close (the
// engine drags only stdlib regexp/strconv/fmt/strings — NO cuelang/grpc/go-plugin). Re-exported
// here so the core check runner + out-of-tree verb plugins (which import only the SDK) compile
// UNCHANGED; the private matchOne/matchNumeric are exercised by spec/matchers' own
// matchers_test.go (a private symbol cannot be reached cross-package, so its tests moved with
// it — the standard Go pattern). charly core re-points to matchers.MatchAll directly (the
// import-purity gate).

// Matcher is re-exported from charly/spec so an out-of-tree plugin reaches the
// matcher value type through the SDK alone (an external plugin imports no other
// charly package).
type Matcher = spec.Matcher

// MatchAll returns nil if every matcher succeeds against the value. The first
// failure wins (reports the specific unmet expectation).
//
// Takes []Matcher rather than MatcherList so callers can pass any named slice
// type whose underlying element is Matcher (e.g. ContainsList) without an
// explicit conversion at every call site.
//
// Relocated to spec/spec; re-exported here so the core check runner and out-of-tree
// verb plugins compile UNCHANGED.
var MatchAll = matchers.MatchAll

// MatchValueString coerces a matcher's stored Value (any) to a string. For
// numeric types it renders canonically; for everything else it falls back
// to fmt.Sprint.
//
// Relocated to spec/spec; re-exported here.
var MatchValueString = matchers.MatchValueString

// MatchValueStrings handles list-valued matchers like {contains: [a, b]}.
// A scalar value becomes a singleton list.
//
// Relocated to spec/spec; re-exported here.
var MatchValueStrings = matchers.MatchValueStrings
