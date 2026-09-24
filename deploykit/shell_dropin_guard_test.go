package deploykit

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestGuardShellDropin_Unit pins the emitted guard shape per shell.
func TestGuardShellDropin_Unit(t *testing.T) {
	tests := []struct {
		shell         string
		wantCond      string
		wantUnchanged bool
	}{
		{"bash", `[ -n "${BASH_VERSION:-}" ] && [ -z "${ZSH_VERSION:-}" ]`, false},
		{"zsh", `[ -n "${ZSH_VERSION:-}" ]`, false},
		{"sh", `[ -z "${BASH_VERSION:-}" ] && [ -z "${ZSH_VERSION:-}" ]`, false},
		{"fish", "", true},
	}
	body := "eval \"$(direnv hook X)\"\n"
	for _, tc := range tests {
		t.Run(tc.shell, func(t *testing.T) {
			got := GuardShellDropin(tc.shell, body)
			if tc.wantUnchanged {
				if got != body {
					t.Errorf("GuardShellDropin(%q) changed a shell-scoped body: %q", tc.shell, got)
				}
				return
			}
			if !strings.Contains(got, tc.wantCond) {
				t.Errorf("GuardShellDropin(%q) missing guard %q:\n%s", tc.shell, tc.wantCond, got)
			}
			if !strings.HasSuffix(got, "fi\n") || !strings.Contains(got, "if ") {
				t.Errorf("GuardShellDropin(%q) not wrapped in if/fi:\n%s", tc.shell, got)
			}
			if !strings.Contains(got, strings.TrimRight(body, "\n")) {
				t.Errorf("GuardShellDropin(%q) dropped the body:\n%s", tc.shell, got)
			}
		})
	}
}

// TestGuardShellDropin_RealShells proves the fix against REAL shells: a bash
// login shell must NOT execute the zsh or sh bodies, and must not error doing
// so. This is the exact field regression — bash sources every
// /etc/profile.d/*.sh, so before the guard it ran the zsh body (syntax error)
// and the sh body.
//
// The complementary "sh runs its own body" half is asserted only when the
// host's `sh` is a genuine non-bash shell (dash/ash/busybox): on a base where
// /bin/sh is bash in sh-compat mode, BASH_VERSION is set and the sh guard
// (correctly) declines, because bash already has its own drop-in.
func TestGuardShellDropin_RealShells(t *testing.T) {
	body := `echo "$SHELL_MARKER" >> "$MARKER_FILE"`

	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed — cannot run the cross-shell regression")
	}

	run := func(interp, guard string) (bool, []byte) {
		marker := t.TempDir() + "/marker"
		script := GuardShellDropin(guard, body)
		cmd := exec.Command(interp, "-lc", script)
		cmd.Env = append(os.Environ(), "MARKER_FILE="+marker, "SHELL_MARKER="+guard)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s executing the %q guard failed: %v\n%s", interp, guard, err, out)
		}
		_, statErr := os.Stat(marker)
		return statErr == nil, out
	}

	// The load-bearing regression: bash must be inert for the zsh and sh guards.
	if ran, out := run("bash", "zsh"); ran {
		t.Errorf("bash executed the ZSH drop-in body (the original bug)\n%s", out)
	}
	if ran, out := run("bash", "sh"); ran {
		t.Errorf("bash executed the SH drop-in body — BASH_VERSION should have gated it out\n%s", out)
	}
	// And bash does run its own guard.
	if ran, _ := run("bash", "bash"); !ran {
		t.Error("bash did NOT run its own bash drop-in body — the guard is over-strict")
	}

	// Genuine non-bash `sh` (when present) runs only the sh guard.
	if _, err := exec.LookPath("sh"); err == nil {
		// Detect whether this `sh` masquerades as bash.
		probe := exec.Command("sh", "-c", `[ -n "${BASH_VERSION:-}" ] && echo bash`)
		pb, _ := probe.Output()
		if strings.TrimSpace(string(pb)) == "bash" {
			t.Log("host /bin/sh is bash in sh-compat mode — skipping the sh-runs-sh assertion")
		} else {
			if ran, out := run("sh", "sh"); !ran {
				t.Errorf("genuine sh did NOT run the sh guard\n%s", out)
			}
			if ran, out := run("sh", "bash"); ran {
				t.Errorf("genuine sh executed the BASH drop-in body\n%s", out)
			}
		}
	}
}
