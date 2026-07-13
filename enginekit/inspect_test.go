package enginekit

// inspect_test.go — white-box coverage for the `<engine> inspect` parse leg
// (parseInspect + mountsFromInspect), re-homed from
// charly/status_live_mounts_test.go when the engine client moved into
// enginekit (P14a). Same package as the source so the unexported parse
// functions + engineInspectRow are reachable.

import "testing"

// TestParseInspect_LiveMounts asserts the JSON .Mounts[] block from a
// realistic `podman inspect` blob is parsed into MountInfo correctly.
// Mirrors the actual immich quadlet's live mount layout — three encrypted
// FUSE binds + workspace bind + two named volumes — so it doubles as the
// JSON-shape regression guard for the data flow that feeds collectOne when a
// running container is queried.
//
// NOTE (P14a re-home trim): the original test also fed the parsed mounts
// through the package-main renderers isEncryptedPlainPath + formatLiveMounts
// (the (enc)-suffix end-to-end block). Those functions did NOT move into
// enginekit — they stay in package main — so the main-side assertions are
// dropped here and replaced with exact Source/Type/Name assertions that
// exercise parseInspect just as fully; the isEncryptedPlainPath /
// formatLiveMounts coverage remains in package main.
func TestParseInspect_LiveMounts(t *testing.T) {
	// Realistic blob shape; both podman + docker emit Mounts[] with these
	// fields. Trimmed to just the fields charly status reads.
	blob := []byte(`[{
		"Name": "/charly-immich",
		"HostConfig": {"NetworkMode": "charly"},
		"Mounts": [
			{"Type": "bind", "Name": "", "Source": "/home/u/.local/share/charly/encrypted/charly-immich-library/plain", "Destination": "/home/user/.immich/library"},
			{"Type": "bind", "Name": "", "Source": "/home/u/.local/share/charly/encrypted/charly-immich-cache/plain", "Destination": "/home/user/.immich/cache"},
			{"Type": "bind", "Name": "", "Source": "/home/u/.local/share/charly/encrypted/charly-immich-pgdata/plain", "Destination": "/home/user/.postgresql/data"},
			{"Type": "bind", "Name": "", "Source": "/home/u/projects/charly", "Destination": "/workspace"},
			{"Type": "volume", "Name": "charly-immich-import", "Source": "/var/lib/containers/storage/volumes/charly-immich-import/_data", "Destination": "/home/user/.immich/import"},
			{"Type": "volume", "Name": "charly-immich-external", "Source": "/var/lib/containers/storage/volumes/charly-immich-external/_data", "Destination": "/home/user/.immich/external"}
		]
	}]`)
	rows, err := parseInspect(blob)
	if err != nil {
		t.Fatalf("parseInspect: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if got := len(rows[0].Mounts); got != 6 {
		t.Fatalf("got %d mounts, want 6", got)
	}

	// Index by Destination for clear assertions.
	byDest := map[string]MountInfo{}
	for _, m := range rows[0].Mounts {
		byDest[m.Destination] = m
	}

	if m, ok := byDest["/home/user/.immich/library"]; !ok || m.Type != "bind" ||
		m.Source != "/home/u/.local/share/charly/encrypted/charly-immich-library/plain" {
		t.Errorf("library mount: got type=%q source=%q; want bind + the encrypted-plain source",
			m.Type, m.Source)
	}
	if m, ok := byDest["/workspace"]; !ok || m.Type != "bind" || m.Source != "/home/u/projects/charly" {
		t.Errorf("workspace mount: got type=%q source=%q; want bind + /home/u/projects/charly",
			m.Type, m.Source)
	}
	if m, ok := byDest["/home/user/.immich/import"]; !ok || m.Type != "volume" || m.Name != "charly-immich-import" {
		t.Errorf("import mount: got type=%q name=%q; want type=volume name=charly-immich-import",
			m.Type, m.Name)
	}
}

// TestMountsFromInspect_MissingMountsField covers the defensive path —
// some inspect blobs (e.g., for created-but-never-started containers, or
// older podman versions) lack the Mounts key entirely. Must return nil
// without panicking.
func TestMountsFromInspect_MissingMountsField(t *testing.T) {
	cases := []struct {
		name string
		raw  map[string]any
	}{
		{"no Mounts key", map[string]any{"Name": "/charly-foo"}},
		{"Mounts is null", map[string]any{"Mounts": nil}},
		{"Mounts is empty array", map[string]any{"Mounts": []any{}}},
		{"Mounts contains non-map garbage", map[string]any{"Mounts": []any{"string", 42, nil}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mountsFromInspect(tc.raw)
			if len(got) != 0 {
				t.Errorf("got %d mounts, want 0; %v", len(got), got)
			}
		})
	}
}
