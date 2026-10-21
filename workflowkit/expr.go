// Package workflowkit holds the engine-agnostic workflow IR helpers: the lobster
// expression grammar (expr-lang plus a grammar-enforcing AST patcher), the
// `${arg}` template expansion, task-arg resolution, the dependency
// closure, pipeline validation, the charly.yml / .lobster LOWERING, the
// cron→OnCalendar conversion, and the workflow graph renderers.
//
// It is a MECHANISM library (kind-blind, capability-free): nothing here dials a
// provider or knows a plugin word. The lobster engine (plugin-lobster) and any
// future engine (github-actions, …) share ONE lowering + ONE validator.
package workflowkit

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/vm"
	"github.com/tidwall/gjson"
)

// Scope is the set of per-step result envelopes an expression evaluates against:
// step id -> the step's raw output, a JSON envelope such as
// `{"json":{...},"stdout":"..."}`. `$fetch.json.name` looks up `json.name` in the
// `fetch` entry with gjson (a `.json` accessor is the author's, not the engine's:
// the whole envelope is the lookup root, matching upstream lobster).
type Scope map[string]string

// refRe matches a lobster step reference `$<id>.<path>` (the path may be dotted).
// The rewrite turns it into `ref("<id>", "<path>")`, which expr evaluates.
var refRe = regexp.MustCompile(`\$([A-Za-z0-9_-]+)\.([A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)*)`)

// envRe matches an environment reference `${env.NAME}` — the spelling the org's own
// migration emits for the retired entity-env form (`$env.NAME` ⇒ `${env.NAME}`,
// plugin-migrate's documented ref rewrite), and the ONE way an expression reads the
// process environment.
var envRe = regexp.MustCompile(`\$\{env\.([A-Za-z_][A-Za-z0-9_]*)\}`)

// lobsterCalls are the only call names the grammar admits, each ONE named form rather than a
// namespace: `ref(...)` for a step reference and `env(...)` for an environment read. Anything
// else — a builtin, a method, an `env.NAME` member — stays refused, so a workflow remains
// exportable to real lobster and no expression can read a namespace nothing defines.
var lobsterCalls = map[string]bool{"ref": true, "env": true}

// rewriteEnvRefs rewrites every `${env.NAME}` in s into an `env("NAME")` call. expr cannot
// parse the brace form at all (`unexpected token Bracket("{")`), so the rewrite is what makes
// the authored spelling reachable; the call it emits is the form the patcher then vets.
func rewriteEnvRefs(s string) string {
	return envRe.ReplaceAllStringFunc(s, func(m string) string {
		return fmt.Sprintf("env(%q)", envRe.FindStringSubmatch(m)[1])
	})
}

// RewriteRefs rewrites every `$<id>.<path>` step reference in s into a
// `ref("<id>", "<path>")` call. Exported so the migration hook (plugin-migrate)
// can reuse the ONE ref grammar (R3).
func RewriteRefs(s string) string {
	return refRe.ReplaceAllStringFunc(s, func(m string) string {
		sm := refRe.FindStringSubmatch(m)
		return fmt.Sprintf("ref(%q, %q)", sm[1], sm[2])
	})
}

// lobsterPatch enforces lobster's expression grammar on an expr AST:
//
//   - a bare identifier becomes a STRING literal (lobster's RHS rule: `state == open`
//     compares against the string "open"), except the reserved true/false and the two
//     CALL names `ref`/`env` — a callee must stay an identifier for the call to resolve;
//   - `null` becomes `nil` (expr's null);
//   - only `|| && == != < <= > >= or and` may be binary operators — `in`,
//     arithmetic, and every builtin/call other than `ref`/`env` are REJECTED, so a
//     workflow stays exportable to real lobster.
//
// It is a faithful port of the RDD prototype (`rdd/libs/main.go`), which was
// verified against upstream lobster 2026.9.16.
type lobsterPatch struct{ err error }

func (p *lobsterPatch) Visit(node *ast.Node) {
	switch n := (*node).(type) {
	case *ast.IdentifierNode:
		switch n.Value {
		case "true", "false", "ref", "env":
		case "null":
			ast.Patch(node, &ast.NilNode{})
		default:
			ast.Patch(node, &ast.StringNode{Value: n.Value})
		}
	case *ast.BinaryNode:
		switch n.Operator {
		case "||", "&&", "==", "!=", "<", "<=", ">", ">=", "or", "and":
		default:
			p.err = fmt.Errorf("operator %q is not lobster grammar", n.Operator)
		}
	case *ast.CallNode:
		id, ok := n.Callee.(*ast.IdentifierNode)
		if !ok || !lobsterCalls[id.Value] {
			p.err = fmt.Errorf("function calls are not lobster grammar")
			return
		}
		// The arity is part of the FORM: `env()` with no variable names nothing, and a
		// load-time refusal beats a run-time one (the whole point of this gate).
		if id.Value == "env" && len(n.Arguments) != 1 {
			p.err = fmt.Errorf("env(...) takes exactly one variable name")
		}
	case *ast.UnaryNode:
		if n.Operator != "!" && n.Operator != "not" {
			p.err = fmt.Errorf("unary %q is not lobster grammar", n.Operator)
		}
	case *ast.ArrayNode, *ast.MapNode, *ast.ConditionalNode, *ast.BuiltinNode, *ast.MemberNode, *ast.SliceNode, *ast.PredicateNode:
		p.err = fmt.Errorf("%T is not lobster grammar", n)
	}
}

// compile builds an expr program for a lobster expression, applying the `$ref` and
// `${env.NAME}` rewrites and the grammar patcher. refFn supplies the step-reference lookup
// (it closes over the Scope for Eval, and is a stub for Validate).
func compile(expression string, refFn func(params ...any) (any, error)) (*vm.Program, error) {
	p := &lobsterPatch{}
	program, err := expr.Compile(
		rewriteEnvRefs(RewriteRefs(expression)),
		expr.Function("ref", refFn),
		expr.Function("env", envFn),
		expr.Patch(p),
		expr.AsBool(),
	)
	if err == nil && p.err != nil {
		err = p.err
	}
	return program, err
}

// envFn is the `env("NAME")` resolver behind `${env.NAME}`: the PROCESS ENVIRONMENT of the
// engine, which is the source the retired `$env.NAME` read (plugin-pipeline's surviving
// `envMap()` is built from `os.Environ()`), so this restores a measured capability rather
// than inventing one. An UNSET name reads as the empty string (os.Getenv's own contract).
func envFn(params ...any) (any, error) {
	if len(params) != 1 {
		return nil, fmt.Errorf("env(...) takes exactly one variable name")
	}
	name, ok := params[0].(string)
	if !ok {
		return nil, fmt.Errorf("env(...) takes a string variable name, got %T", params[0])
	}
	return envLiteral(os.Getenv(name)), nil
}

// envLiteral renders an environment VALUE as the lobster LITERAL it would have been if the
// value had been substituted into the expression as TEXT — which is exactly what the retired
// `$env.NAME` form did, so this is the parity definition rather than a convenience.
//
// It matters in the consumer's own guard: `when: '!(${env.EVAL_PUBLISH} != true)'` must RUN
// when the variable is `true`. Handing back the raw string would make it `"true" != true` —
// always true — and the stage would be skipped forever. The literal reading makes it
// `true != true`, which is what the author wrote.
func envLiteral(v string) any {
	switch v {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}
	if n, err := strconv.Atoi(v); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		return f
	}
	return v
}

// Validate compiles expression under the lobster grammar with a stub reference
// resolver and reports the first grammar/type error. It never evaluates.
func Validate(expression string) error {
	_, err := compile(expression, func(params ...any) (any, error) { return nil, nil })
	return err
}

// Eval compiles and runs a lobster boolean expression against scope. `true`/`false`
// literals must be lowercase (lobster's spelling). The result must be a bool.
func Eval(expression string, scope Scope) (bool, error) {
	ref := func(params ...any) (any, error) {
		if len(params) != 2 {
			return nil, nil
		}
		name, _ := params[0].(string)
		path, _ := params[1].(string)
		raw, ok := scope[name]
		if !ok {
			return nil, nil
		}
		r := gjson.Get(raw, path)
		if !r.Exists() {
			return nil, nil
		}
		return r.Value(), nil
	}
	program, err := compile(expression, ref)
	if err != nil {
		return false, err
	}
	v, err := expr.Run(program, nil)
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("expression %q did not evaluate to a bool (got %T)", expression, v)
	}
	return b, nil
}

// RefNames returns the distinct step ids referenced by expression, in first-seen
// order. It is the seam the validator/lowering uses to know which earlier steps a
// `when:`/`stdin:`/plan consumes (refs to earlier steps only).
func RefNames(expression string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range refRe.FindAllStringSubmatch(expression, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// RefIDs reduces a list of `$<id>.<path>` references to their distinct step ids, in
// order. It is the seam the validator uses on a plan's collected references.
func RefIDs(refs []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range refs {
		id := strings.TrimPrefix(r, "$")
		if i := strings.IndexByte(id, '.'); i >= 0 {
			id = id[:i]
		}
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// HasRef reports whether s contains any `$<id>.<path>` step reference.
func HasRef(s string) bool { return strings.Contains(s, "$") && refRe.MatchString(s) }
