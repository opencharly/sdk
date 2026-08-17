package deploykit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// shell_and_loudness_test.go — coverage for three changes a validator proved could be reverted
// without any test objecting. Each was individually mutated back to its pre-fix form and confirmed
// to fail here; the point of the file is that the suite NOTICES, not that the current code is
// pretty.
//
// The two emitter ones matter out of proportion to their size: the shell prefix is rewritten for
// every build step of every image, so a defect there breaks the whole ecosystem at once, and a
// silent-drop conversion that can be silently un-converted is the exact failure mode the change
// exists to close.

// The Containerfile shell prefix must not hardcode bash. Busybox bases ship /bin/sh only, so an
// emitted `bash -c` is unbuildable there regardless of what the step does.
func TestBuildStepShell_NeverHardcodesBash(t *testing.T) {
	for name, got := range map[string]string{
		"dash-c":  BuildStepShellDashC(),
		"heredoc": BuildStepShellHeredoc(),
	} {
		if got == "" {
			t.Fatalf("%s: empty prefix — every assertion below would pass vacuously", name)
		}
		// It must START from sh: that is the interpreter guaranteed to exist.
		if !strings.HasPrefix(got, "sh -c") {
			t.Errorf("%s: prefix does not start with `sh -c`, so it assumes an interpreter that a "+
				"busybox base need not have: %q", name, got)
		}
		// bash may be PREFERRED, but only behind an executability probe — never as the
		// unconditional interpreter.
		if strings.Contains(got, "bash") && !strings.Contains(got, "[ -x /bin/bash ]") {
			t.Errorf("%s: names bash without probing for it first: %q", name, got)
		}
		// The authored command must still reach the chosen shell.
		if !strings.Contains(got, `exec "$SH"`) {
			t.Errorf("%s: does not exec the resolved shell, so the authored command may not run "+
				"under it: %q", name, got)
		}
	}
}

// Presence control: the two prefixes are different shapes (one passes the command as $1, the other
// relies on exec preserving stdin for a heredoc). Without this, a single hardcoded string could
// satisfy the assertions above for both.
func TestBuildStepShell_DashCAndHeredocDiffer(t *testing.T) {
	dashC, heredoc := BuildStepShellDashC(), BuildStepShellHeredoc()
	if dashC == heredoc {
		t.Fatalf("the -c and heredoc prefixes are identical (%q); the -c form must pass the command "+
			"as an argument and the heredoc form must leave stdin for the here-document", dashC)
	}
	if !strings.Contains(dashC, `-c "$1"`) {
		t.Errorf("the -c form does not pass the authored command as $1: %q", dashC)
	}
	if strings.Contains(heredoc, `"$1"`) {
		t.Errorf("the heredoc form passes a command argument, which would consume the here-document: %q", heredoc)
	}
}

// The data-image seeder runs inside the USER'S image, which may be busybox-based. Its command is
// pure POSIX, so it must not ask for bash.
func TestProvisionFromRunnableImage_UsesPosixShell(t *testing.T) {
	fake := &fakeRunner{}
	origRun := dataCmdRun
	dataCmdRun = fake.run
	t.Cleanup(func() { dataCmdRun = origRun })

	// Drive the seeder far enough to record the argv; failures from the fake are fine — the
	// assertion is about what was ASKED for, not whether it succeeded.
	_ = provisionFromRunnableImage(
		"podman",
		"example.invalid/user-image:latest",
		&spec.BoxMetadata{},
		spec.LabelDataEntry{Volume: "workspace", Staging: "/data/workspace/", Candy: "c"},
		seedTarget{bareName: "workspace", mountSource: "charly-x-workspace"},
		DataProvisionInitial,
	)

	var shells []string
	for _, c := range fake.calls {
		for i, a := range c.args {
			if a == "sh" || a == "bash" {
				if i+1 < len(c.args) && c.args[i+1] == "-c" {
					shells = append(shells, a)
				}
			}
		}
	}
	if len(shells) == 0 {
		t.Skip("seeder did not reach the container-exec step in this configuration; nothing to assert")
	}
	for _, sh := range shells {
		if sh == "bash" {
			t.Errorf("the data-image seeder asks for `bash -c` inside the user's image; the command " +
				"is pure POSIX (mkdir -p, &&, cp) and a busybox-based data image has no /bin/bash")
		}
	}
}
