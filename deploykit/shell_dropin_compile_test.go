package deploykit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestCompileShellSnippetSteps_GuardsContainerDropins proves the wiring: on the
// CONTAINER-BUILD path (every /etc/profile.d/*.sh is sourced by every POSIX login
// shell) each bash/zsh/sh snippet is wrapped in its shell guard, while the
// HOST/VENUE path (per-shell rc files, already shell-scoped) is left unguarded.
// This is the regression the direnv field bug exposed: bash executed the zsh body.
func TestCompileShellSnippetSteps_GuardsContainerDropins(t *testing.T) {
	const candy = "direnv"
	layer := newTestCandy(candy, spec.CandyModel{
		Shell: &spec.Shell{
			Init: `eval "$(direnv hook ${SHELL_NAME})"`,
		},
	})
	img := &ResolvedBox{ResolvedBox: spec.ResolvedBox{Home: "/home/user"}}

	// Container build path.
	steps := CompileShellSnippetSteps(layer, img, HostContext{})
	byShell := map[string]spec.ShellSnippetStep{}
	for _, st := range steps {
		s, ok := st.(*ShellSnippetStep)
		if !ok {
			t.Fatalf("unexpected step type %T", st)
		}
		byShell[s.Shell] = *s
	}

	for _, sh := range []string{"bash", "zsh", "sh"} {
		s, ok := byShell[sh]
		if !ok {
			t.Fatalf("no snippet emitted for shell %q", sh)
		}
		if !strings.Contains(s.Snippet, "BASH_VERSION") && !strings.Contains(s.Snippet, "ZSH_VERSION") {
			t.Errorf("container %s drop-in is UNGUARDED (bash would run it):\n%s", sh, s.Snippet)
		}
		if !strings.HasPrefix(s.Destination, "/etc/profile.d/") {
			t.Errorf("%s destination = %q, want /etc/profile.d/", sh, s.Destination)
		}
	}

	// fish is shell-scoped by its conf.d drop-in — must NOT be wrapped.
	if fish, ok := byShell["fish"]; ok {
		if strings.Contains(fish.Snippet, "BASH_VERSION") {
			t.Errorf("fish drop-in must not carry a POSIX shell guard:\n%s", fish.Snippet)
		}
	}

	// Host/venue path: per-shell rc files are already shell-scoped — no guard.
	hostSteps := CompileShellSnippetSteps(layer, img, HostContext{MachineVenue: true})
	for _, st := range hostSteps {
		s, _ := st.(*ShellSnippetStep)
		if s == nil {
			continue
		}
		if strings.Contains(s.Snippet, "BASH_VERSION") {
			t.Errorf("host %s snippet must not be guarded (rc file is shell-scoped):\n%s", s.Shell, s.Snippet)
		}
	}
}
