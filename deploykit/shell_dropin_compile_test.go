package deploykit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestCompileShellSnippetSteps_GuardsDropins proves the wiring: BOTH paths wrap
// each POSIX snippet in its shell guard. On the container path every
// /etc/profile.d/*.sh is sourced by every POSIX login shell. On the host path the
// `sh` snippet goes to ~/.profile — the SHARED POSIX login profile that bash also
// sources when ~/.bash_profile/~/.bash_login are absent — so the guard is needed
// there too. fish (its own conf.d drop-in) is never guarded.
func TestCompileShellSnippetSteps_GuardsDropins(t *testing.T) {
	const candy = "direnv"
	layer := newTestCandy(candy, spec.CandyModel{
		Shell: &spec.Shell{
			Init: `eval "$(direnv hook ${SHELL_NAME})"`,
		},
	})
	img := &ResolvedBox{ResolvedBox: spec.ResolvedBox{Home: "/home/user"}}

	for _, venue := range []struct {
		name   string
		ctx    HostContext
		prefix string
	}{
		{"container", HostContext{}, "/etc/profile.d/"},
		{"host", HostContext{MachineVenue: true}, ""},
	} {
		t.Run(venue.name, func(t *testing.T) {
			steps := CompileShellSnippetSteps(layer, img, venue.ctx)
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
					t.Errorf("%s %s snippet is UNGUARDED (a sibling shell would run it):\n%s", venue.name, sh, s.Snippet)
				}
			}
			if fish, ok := byShell["fish"]; ok && strings.Contains(fish.Snippet, "BASH_VERSION") {
				t.Errorf("%s fish snippet must not carry a POSIX guard:\n%s", venue.name, fish.Snippet)
			}
		})
	}
}
