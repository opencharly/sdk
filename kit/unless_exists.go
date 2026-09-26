package kit

// unless_exists.go — the `unless_exists` capability GATE, the ONE definition of the
// guard, importable by BOTH the container build emitter (via the deploykit alias) and
// the host/machine-venue op renderer (RenderOpCommand). Relocated here from deploykit
// (R3: one copy; the host path previously had NO guard at all — RCA 2026-09-26).

import (
	"fmt"
	"strings"

	"github.com/opencharly/spec/shellquote"
)

// WrapUnlessExists and WrapUnlessExistsBlock wrap cmd in the `unless_exists` capability
// GATE, or return cmd unchanged when the guard is empty. They share the gate's SEMANTICS
// and differ only in how the guarded list is terminated, because their call sites sit in
// genuinely different lexical contexts:
//
//   - WrapUnlessExists      `else { cmd; }; fi`   — one line. EmitDownload's payload is a
//     single-quoted argument on ONE Containerfile RUN line, where a literal newline would
//     end the instruction, so `;` is the only terminator available.
//   - WrapUnlessExistsBlock `else {\ncmd\n}; fi` — a payload that is a heredoc body or a
//     multi-line script, already multi-line by construction, where a newline costs nothing.
//
// The gate is evaluated where the step runs: on a distro that packages the tool, `distro:`
// package sections compile into the plan before the `plan:` steps, so the guarded path
// already exists by the time the step runs. The guard is shell-quoted in BOTH positions so
// a path containing a double quote or `$(…)` cannot corrupt the test or the message.
//
// verb names the step kind in the skip message ("download", "run"), because a log that
// prints "skipping: X already present" for three different reasons is worse than one that
// says which step declined to run.
func WrapUnlessExists(cmd, guard, verb string) string {
	return wrapUnlessExists(cmd, guard, verb, " : ; ", "; ")
}

// WrapUnlessExistsBlock is the newline-terminated form, for a payload that is already multi-line.
func WrapUnlessExistsBlock(cmd, guard, verb string) string {
	return wrapUnlessExists(strings.TrimRight(cmd, "\n"), guard, verb, "\n:\n", "\n")
}

func wrapUnlessExists(cmd, guard, verb, prefix, term string) string {
	g := strings.TrimSpace(guard)
	if g == "" {
		return cmd
	}
	q := shellquote.ShellQuote(g)
	head := fmt.Sprintf(`if [ -e %s ]; then echo "skipping %s: %s already present"; else `, q, verb, q)
	// `{ list; }` requires a NON-EMPTY list — the same rule as the terminator, on its other end.
	// A `run:` step whose command is empty or comment-only would otherwise emit `{ }` and fail,
	// turning a working no-op step into a syntax error the moment a guard is added to it. The
	// leading `:` in prefix is the shell no-op: it guarantees at least one command in the list
	// for every body. A body that is nothing BUT whitespace collapses to the no-op alone.
	if strings.TrimSpace(cmd) == "" {
		return head + "{ :; }; fi"
	}
	return head + fmt.Sprintf("{%s%s%s}; fi", prefix, cmd, term)
}
