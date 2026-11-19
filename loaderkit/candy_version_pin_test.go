package loaderkit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// A COMMIT SHA THAT LOSES TO A TAG MUST SAY SO, AND SAY WHAT TO DO INSTEAD.
//
// This is opencharly/charly#1215. A `distro-*` box pinned a layer at its then-current 40-hex
// revision while the layer's own `require:` named a release tag in the same box scope. The
// arbiter (correctly) picked the tag, and the message said "resolved to multiple git TAGS ...
// using newest referenced v2026.323.0252" — naming neither the SHA's fate nor a way forward, so
// the failure read as impossible to fix and the composing R10 bed seemed unreachable until the
// layer merged and was tagged.
//
// The arbitration MUST NOT change: a SHA carries no recency, and letting it win is the regression
// opencharly/charly#1179 fixed. What changes is that the losing non-CalVer ref is now NAMED, with
// the producer-first action that resolves it.
func TestPickCandyVersionCommitPinLosingToTagSaysWhatToDo(t *testing.T) {
	const sha = "219f643b404acdba32592784eb23ca488fe57f1b"
	cands := []spec.CandyCandidate{
		{GitTag: "v2026.323.0252", Source: "hub@v2026.323.0252", Referrers: []string{"box=nvidia-cdi"}},
		{GitTag: sha, Source: "hub@" + sha, Referrers: []string{"box=nvidia-cdi"}},
	}
	d := &diagCollector{}
	best := PickCandyVersion("github.com/opencharly/layer-cuda", cands, d.sink())

	if best.GitTag != "v2026.323.0252" {
		t.Fatalf("arbiter picked %q; a revision must never outrank a release tag", best.GitTag)
	}
	if len(d.levels) != 1 || d.levels[0] != spec.DiagInfo {
		t.Fatalf("expected exactly one INFO diagnostic, got %v", d.levels)
	}
	msg := d.msgs[0]
	for _, want := range []string{
		sha,                       // the pin the reader made
		"box=nvidia-cdi",          // the scope it is in
		"cannot be arbitrated",    // the mechanism
		"no recency",              // why the SHA loses
		"`require:`",              // where to act
		"producer-first",          // the order
		"using newest referenced", // the pre-existing sentence still leads
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("diagnostic does not carry %q:\n%s", want, msg)
		}
	}
}

// TWO TAGS in one scope keep the generic sentence and gain NO pin remedy: nothing there is a
// revision, and explaining a SHA that is not in the diff would be noise on a case the reader can
// already act on.
func TestPickCandyVersionTwoTagsGetNoPinRemedy(t *testing.T) {
	cands := []spec.CandyCandidate{
		{GitTag: "v2026.235.2056", Source: "hub@v2026.235.2056", Referrers: []string{"box=shared"}},
		{GitTag: "v2026.239.1624", Source: "hub@v2026.239.1624", Referrers: []string{"box=shared"}},
	}
	d := &diagCollector{}
	PickCandyVersion("github.com/opencharly/layer-dev-tools", cands, d.sink())
	if len(d.msgs) != 1 {
		t.Fatalf("expected exactly one diagnostic, got %d", len(d.msgs))
	}
	if strings.Contains(d.msgs[0], "cannot be arbitrated") {
		t.Errorf("a tag-vs-tag skew must not be explained as a revision pin:\n%s", d.msgs[0])
	}
}

// A BRANCH ref is non-CalVer for the same reason a SHA is, and loses the same way — so it is named
// the same way, as "a ref that is not a CalVer tag" rather than as a revision
// (opencharly/charly#1214 is the sibling failure of the same pin form).
func TestPickCandyVersionBranchRefLosingToTagIsNamed(t *testing.T) {
	const branch = "fix/offload-model-when-idle"
	cands := []spec.CandyCandidate{
		{GitTag: "v2026.323.0252", Source: "hub@v2026.323.0252", Referrers: []string{"box=comfyui-cdi"}},
		{GitTag: branch, Source: "hub@" + branch, Referrers: []string{"box=comfyui-cdi"}},
	}
	d := &diagCollector{}
	best := PickCandyVersion("github.com/opencharly/pod-comfyui", cands, d.sink())
	if best.GitTag != "v2026.323.0252" {
		t.Fatalf("arbiter picked %q, want the release tag", best.GitTag)
	}
	msg := d.msgs[0]
	if !strings.Contains(msg, branch) || !strings.Contains(msg, "not a CalVer tag") {
		t.Errorf("a losing branch ref must be named and described:\n%s", msg)
	}
}

// A revision in a DIFFERENT box is an independent composition, not a conflict: the scope rule is
// checked BEFORE the remedy, so nothing is reported (and no remedy can leak out of the silent
// path).
func TestPickCandyVersionRevisionInAnotherBoxIsSilent(t *testing.T) {
	const sha = "219f643b404acdba32592784eb23ca488fe57f1b"
	cands := []spec.CandyCandidate{
		{GitTag: "v2026.323.0252", Source: "hub@v2026.323.0252", Referrers: []string{"box=one-box"}},
		{GitTag: sha, Source: "hub@" + sha, Referrers: []string{"box=other-box"}},
	}
	d := &diagCollector{}
	best := PickCandyVersion("github.com/opencharly/layer-cuda", cands, d.sink())
	if len(d.levels) != 0 {
		t.Fatalf("a cross-box difference must stay silent, got %q", d.msgs)
	}
	if best.GitTag != "v2026.323.0252" {
		t.Errorf("the release tag must still win, got %q", best.GitTag)
	}
}

func TestIsCommitSHA(t *testing.T) {
	for _, c := range []struct {
		ref  string
		want bool
	}{
		{"219f643b404acdba32592784eb23ca488fe57f1b", true},
		{"219F643B404ACDBA32592784EB23CA488FE57F1B", true},
		{"219f643b404acdba32592784eb23ca488fe57f1", false},   // 39 hex
		{"219f643b404acdba32592784eb23ca488fe57f1bX", false}, // 41 chars
		{"fix/offload-model-when-idle", false},               // a branch
		{"v2026.323.0252", false},                            // a tag
		{"", false},                                          //
	} {
		if got := isCommitSHA(c.ref); got != c.want {
			t.Errorf("isCommitSHA(%q) = %v, want %v", c.ref, got, c.want)
		}
	}
}
