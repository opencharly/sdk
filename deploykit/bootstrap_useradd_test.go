package deploykit

import (
	"strings"
	"testing"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/spec/spec"
)

// bootstrapCreateBox builds the minimal ResolvedBox that drives WriteBootstrap's
// user-CREATION branch (UserAdopted false).
func bootstrapCreateBox() *buildkit.ResolvedBox {
	return &buildkit.ResolvedBox{
		ResolvedBox: spec.ResolvedBox{
			User:           "user",
			UID:            1000,
			GID:            1000,
			Home:           "/home/user",
			UserAdopted:    false,
			IsExternalBase: true,
		},
	}
}

// TestWriteBootstrap_UserCreationSupportsBusyboxBases asserts the emitted
// user-creation block works on BOTH userlands: shadow-utils bases
// (Fedora/Debian/Arch — groupadd/useradd) and busybox bases (Alpine —
// addgroup/adduser, and no /bin/bash).
//
// Regression: the block previously emitted groupadd/useradd unconditionally, so
// every busybox base failed the build with `/bin/sh: groupadd: not found`
// (exit 127) at the bootstrap step.
func TestWriteBootstrap_UserCreationSupportsBusyboxBases(t *testing.T) {
	var b strings.Builder
	g := &Generator{}
	g.WriteBootstrap(&b, bootstrapCreateBox())
	got := b.String()

	// The shadow-utils leg must still be emitted.
	for _, want := range []string{
		"command -v useradd",
		"groupadd -g 1000 user",
		"useradd -m -u 1000 -g 1000",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("shadow-utils leg missing %q\n--- emitted ---\n%s", want, got)
		}
	}

	// The busybox leg is what the regression is about.
	for _, want := range []string{
		"addgroup -g 1000 user",
		"adduser -D -h /home/user -u 1000 -G user",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("busybox leg missing %q\n--- emitted ---\n%s", want, got)
		}
	}

	// The login shell must be probed, never hardcoded to /bin/bash — Alpine
	// ships no bash, so a hardcoded -s /bin/bash creates a broken login shell.
	if !strings.Contains(got, "LOGIN_SHELL=/bin/sh") || !strings.Contains(got, "[ -x /bin/bash ]") {
		t.Errorf("login shell is not probed\n--- emitted ---\n%s", got)
	}
	if strings.Contains(got, "-s /bin/bash") {
		t.Errorf("login shell is hardcoded to /bin/bash\n--- emitted ---\n%s", got)
	}
}

// TestWriteBootstrap_AdoptedUserEmitsNoUseradd guards the other branch: an
// adopted base user needs no creation commands at all.
func TestWriteBootstrap_AdoptedUserEmitsNoUseradd(t *testing.T) {
	box := bootstrapCreateBox()
	box.UserAdopted = true

	var b strings.Builder
	g := &Generator{}
	g.WriteBootstrap(&b, box)
	got := b.String()

	for _, absent := range []string{"useradd", "adduser", "groupadd", "addgroup"} {
		if strings.Contains(got, absent) && !strings.Contains(got, "no useradd needed") {
			t.Errorf("adopted-user branch emitted %q\n--- emitted ---\n%s", absent, got)
		}
	}
	if !strings.Contains(got, "adopted from base image") {
		t.Errorf("adopted-user branch missing its comment\n--- emitted ---\n%s", got)
	}
}
