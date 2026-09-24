package deploykit

import "testing"

// TestVolumeNameBelongsTo is the regression guard for the volume-teardown /
// listing over-match: a base deploy's volume prefix (charly-<base>-) is itself a
// prefix of every sibling instance's volume name (charly-<base>-<instance>-<vol>),
// and podman's `volume ls --filter name=` is a SUBSTRING match. Without the
// sibling exclusion, `charly remove --purge <base>` (and `charly volume list
// <base>`) would touch a live instance's volumes — silent data loss. The test
// pins the exact names observed in the field.
func TestVolumeNameBelongsTo(t *testing.T) {
	// The field reproduction: base deploy `githubrunner` with instances no-1..no-4.
	dc := &DeployConfig{Deploy: map[string]DeployNode{
		"githubrunner":       {},
		"githubrunner/no-1":  {},
		"githubrunner/no-2":  {},
		"githubrunner/no-3":  {},
		"githubrunner/no-4":  {},
		"unrelated":          {},
		"unrelated/tenant-a": {},
	}}
	baseSiblings := SiblingVolumePrefixes(dc, "githubrunner", "")

	tests := []struct {
		name     string
		vol      string
		box      string
		instance string
		want     bool
	}{
		{"base owns its own state", "charly-githubrunner-state", "githubrunner", "", true},
		{"base owns its own storage", "charly-githubrunner-storage", "githubrunner", "", true},
		{"base must NOT claim instance no-1 state", "charly-githubrunner-no-1-state", "githubrunner", "", false},
		{"base must NOT claim instance no-4 storage", "charly-githubrunner-no-4-storage", "githubrunner", "", false},
		{"instance no-2 owns its own state", "charly-githubrunner-no-2-state", "githubrunner", "no-2", true},
		{"instance no-2 must NOT claim no-2x (dash-anchored)", "charly-githubrunner-no-2x-state", "githubrunner", "no-2", false},
		{"instance no-2 must NOT claim a sibling no-3", "charly-githubrunner-no-3-state", "githubrunner", "no-2", false},
		{"base must NOT claim an unrelated volume", "charly-other-state", "githubrunner", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sib := baseSiblings
			if tc.instance != "" {
				sib = SiblingVolumePrefixes(dc, tc.box, tc.instance)
			}
			if got := VolumeNameBelongsTo(tc.vol, tc.box, tc.instance, sib); got != tc.want {
				t.Errorf("VolumeNameBelongsTo(%q, %q, %q) = %v, want %v", tc.vol, tc.box, tc.instance, got, tc.want)
			}
		})
	}
}

// TestSiblingVolumePrefixes_ExcludesSelf pins the self-exclusion: the returned
// set never contains the queried deploy's own prefix, so a caller can hand it
// straight to VolumeNameBelongsTo without filtering.
func TestSiblingVolumePrefixes_ExcludesSelf(t *testing.T) {
	dc := &DeployConfig{Deploy: map[string]DeployNode{
		"app":      {},
		"app/work": {},
		"app/dev":  {},
	}}
	self := DeployVolumePrefix("app", "")
	for _, p := range SiblingVolumePrefixes(dc, "app", "") {
		if p == self {
			t.Fatalf("SiblingVolumePrefixes included the queried deploy's own prefix %q", self)
		}
	}
	// Nil config (the orphaned-deploy case) yields no siblings — the caller falls
	// back to the bare prefix, matching the pre-existing best-effort contract.
	if got := SiblingVolumePrefixes(nil, "app", ""); got != nil {
		t.Errorf("SiblingVolumePrefixes(nil, ...) = %v, want nil", got)
	}
}
