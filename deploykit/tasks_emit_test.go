package deploykit

import (
	"strings"
	"testing"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/vmshared"
)

// TestTaskRunsAsRoot pins the stage-user decision: RunAs empty inherits the
// image build user (root only when the image builds as root); an explicit
// RunAs wins. This is the ownership pivot for every cache mount — getting it
// wrong root-ifies a non-root stage's cache (the curl-23 build failure).
func TestTaskRunsAsRoot(t *testing.T) {
	userImg := &buildkit.ResolvedBox{User: "user", UID: 1000, GID: 1000, Home: "/home/user"}
	rootImg := &buildkit.ResolvedBox{User: "root", UID: 0, GID: 0, Home: "/root"}
	cases := []struct {
		name  string
		runAs string
		img   *buildkit.ResolvedBox
		want  bool
	}{
		{"empty RunAs on a non-root image inherits the stage's non-root user", "", userImg, false},
		{"empty RunAs on a root image is root", "", rootImg, true},
		{"explicit root RunAs on a non-root image is root", "root", userImg, true},
		{"explicit 0 RunAs on a non-root image is root", "0", userImg, true},
		{"${USER} RunAs on a non-root image is non-root", "${USER}", userImg, false},
	}
	for _, c := range cases {
		if got := taskRunsAsRoot(c.runAs, c.img); got != c.want {
			t.Errorf("%s: taskRunsAsRoot(%q, uid=%d) = %v, want %v", c.name, c.runAs, c.img.UID, got, c.want)
		}
	}
}

// TestEmitDownload_DownloadsCacheOwnership: the shared downloads cache follows
// the stage user — non-root stages get an uid/gid-owned cache (so one
// root-stage download can never poison every non-root stage's downloads), root
// stages keep the shared form.
func TestEmitDownload_DownloadsCacheOwnership(t *testing.T) {
	op := vmshared.Op{Download: "https://example.com/x.tar.gz", Extract: "none", To: "/usr/local/bin/x"}

	var nonRoot strings.Builder
	img := &buildkit.ResolvedBox{User: "user", UID: 1000, GID: 1000, Home: "/home/user"}
	if err := EmitDownload(&nonRoot, op, img); err != nil {
		t.Fatalf("EmitDownload: %v", err)
	}
	if out := nonRoot.String(); !strings.Contains(out, "--mount=type=cache,id=charly-tmp-downloads-uid1000,dst=/tmp/downloads,uid=1000,gid=1000") {
		t.Errorf("non-root stage must get an uid/gid-owned downloads cache mount:\n%s", out)
	}

	var root strings.Builder
	img.User = "root"
	img.UID = 0
	img.GID = 0
	if err := EmitDownload(&root, op, img); err != nil {
		t.Fatalf("EmitDownload: %v", err)
	}
	out := root.String()
	if !strings.Contains(out, "--mount=type=cache,id=charly-tmp-downloads,dst=/tmp/downloads,sharing=locked") {
		t.Errorf("root stage must keep the shared downloads cache mount:\n%s", out)
	}
	if strings.Contains(out, "charly-tmp-downloads-uid") {
		t.Errorf("root stage must not get a uid-scoped cache id:\n%s", out)
	}
}
