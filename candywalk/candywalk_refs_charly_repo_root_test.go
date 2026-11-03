package candywalk

import (
	"os"
	"path/filepath"
	"testing"
)

// candywalk_refs_charly_repo_root_test.go — the BARE charly repo-root ref as a corpus SOURCE
// (opencharly/sdk#468).
//
// The refusal this file guards was added for ONE form: `@github.com/opencharly/charly/candy/<name>`,
// the pre-cutover IN-REPO ref that resolves to the local charly checkout's candy dir (the local FS
// walk already surfaces that) and that the box submodule repos' own cutover sweeps. Keyed on
// `RepoPath` ALONE it could not tell that form from the BARE repo-root form — the shape
// `opencharly/opencharly`, `spec` and `sdk` are already pinned as, and the shape `charly` authored
// its own skills in (`<root>/.agents/skills/`, 2026-10-23) AFTER the refusal existed.
//
// So the policy keeps the subpath form refused and admits the repo root. Both arms are pinned here,
// because the subpath arm IS the regression guard: a box submodule that still carries the pre-cutover
// ref must not start resolving it through the network.

// TestCollectRefs_CharlyRepoRootIsACorpusSource is the R7 gate on opencharly/sdk#468: the BARE
// repo-root ref must be COLLECTED (it is a corpus source) while BOTH spellings of the pre-cutover
// subpath form stay refused, and a sibling ref in the same list is untouched.
//
// Pre-fix this test collects one ref where two were declared: the charly repo-root pin vanished with
// no error and no message, which is what made three skills unreachable from the corpus.
func TestCollectRefs_CharlyRepoRootIsACorpusSource(t *testing.T) {
	const (
		repoRoot    = "@github.com/opencharly/charly:v2026.296.2303"
		subpath     = "@github.com/opencharly/charly/candy/charly-core:v2026.296.2303"
		subpathBare = "@github.com/opencharly/charly/candy/docs-sources"
		siblingRef  = "@github.com/opencharly/layer-nodejs:v2026.240.0001"
	)
	dir := t.TempDir()
	path := filepath.Join(dir, UnifiedFileName)
	body := `charly-marketplace:
    candy:
        candy:
            - '` + repoRoot + `'
            - '` + subpath + `'
            - '` + subpathBare + `'
            - '` + siblingRef + `'
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got := collectRefs(path)
	want := map[string]bool{repoRoot: false, siblingRef: false}
	for _, ref := range got {
		if _, ok := want[ref]; !ok {
			t.Fatalf("collectRefs returned %q — neither the charly SUBPATH form (the pre-cutover in-repo "+
				"ref, which resolves to the LOCAL checkout and is swept by the box repos' cutover) nor any "+
				"other unexpected ref may be collected here; got %v", ref, got)
		}
		want[ref] = true
	}
	for ref, seen := range want {
		if !seen {
			t.Fatalf("collectRefs DROPPED %q — the bare repo-root form is a corpus SOURCE "+
				"(opencharly/sdk#468) and a sibling declared ref must never vanish silently "+
				"(opencharly/sdk#322); got %v", ref, got)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("collectRefs got %d refs, want %d: %v", len(got), len(want), got)
	}
}

// TestCollectEntitiesRemote_CharlyRepoRootManifestEmitsSkills is the end-to-end arm: a refs list
// declaring the BARE charly repo-root ref must have its ROOT manifest walked and its skill nodes
// unioned in, exactly as the umbrella's three are. It is the walk-level twin of the collector test
// above — the collector admitting the ref is necessary and not sufficient, because a fetched repo
// that is never walked contributes nothing.
//
// Pre-fix this sees zero skill entities from charly (the ref never reaches the resolver).
func TestCollectEntitiesRemote_CharlyRepoRootManifestEmitsSkills(t *testing.T) {
	const key = "github.com/opencharly/charly:v2026.296.2303"

	// The refs list: ONE bare charly repo-root pin, the shape the marketplace refs list uses.
	refsRoot := t.TempDir()
	candyDir := filepath.Join(refsRoot, "candy", "charly-marketplace")
	if err := os.MkdirAll(candyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	refsBody := `charly-marketplace:
    candy:
        candy:
            - '@` + key + `'
`
	if err := os.WriteFile(filepath.Join(candyDir, UnifiedFileName), []byte(refsBody), 0o644); err != nil {
		t.Fatal(err)
	}

	// The fetched tree: charly's real shape — routing-only `skill:` nodes in the ROOT manifest, each
	// owned by the `charly-skill-shelf` concept candy, with the prose under `.agents/skills/<name>/`.
	tree := t.TempDir()
	for _, name := range []string{"charly-version", "go", "go-quality", "plugin"} {
		d := filepath.Join(tree, AgentsSkillsDir, name)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		prose := "---\nname: " + name + "\ndescription: fixture\n---\n\n# " + name + "\n"
		if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(prose), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rootBody := `charly-version:
    skill:
        description: fixture
        family: core
        owner: charly-skill-shelf
go:
    skill:
        description: fixture
        family: internals
        owner: charly-skill-shelf
go-quality:
    skill:
        description: fixture
        family: internals
        owner: charly-skill-shelf
plugin:
    skill:
        description: fixture
        family: internals
        owner: charly-skill-shelf
`
	if err := os.WriteFile(filepath.Join(tree, UnifiedFileName), []byte(rootBody), 0o644); err != nil {
		t.Fatal(err)
	}
	// The fetched layout also carries candy/<name>/ entities (charly ships candy/docs-sources); the
	// walk reads both, so the fixture does too — it must not disturb the skill count.
	dsDir := filepath.Join(tree, "candy", "docs-sources")
	if err := os.MkdirAll(dsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dsBody := `docs-sources:
    candy:
        description: fixture
`
	if err := os.WriteFile(filepath.Join(dsDir, UnifiedFileName), []byte(dsBody), 0o644); err != nil {
		t.Fatal(err)
	}

	var resolved []string
	empty := t.TempDir()
	resolve := func(repoPath, version string) (string, error) {
		resolved = append(resolved, repoPath+":"+version) // the walk's own key form
		if repoPath == "github.com/opencharly/charly" {
			return tree, nil
		}
		return empty, nil
	}

	ents, err := CollectEntitiesRemote([]Root{{Namespace: "", Dir: refsRoot}}, resolve)
	if err != nil {
		t.Fatalf("CollectEntitiesRemote: %v", err)
	}
	var charlyRefs int
	for _, r := range resolved {
		if r == key {
			charlyRefs++
		}
	}
	if charlyRefs != 1 {
		t.Fatalf("the bare charly repo-root ref reached the resolver %d times, want exactly 1 — the "+
			"corpus walk must FETCH it (opencharly/sdk#468); resolver calls: %v", charlyRefs, resolved)
	}

	got := map[string]bool{}
	for _, e := range ents {
		if e.Namespace != key {
			continue
		}
		if e.Kind != "skill" {
			continue
		}
		got[e.Name] = true
		if e.Dir != "." {
			t.Errorf("entity %q came from dir %q, want the repo ROOT (\".\")", e.Name, e.Dir)
		}
		if e.SourceRoot != tree {
			t.Errorf("entity %q has SourceRoot %q, want the fetched tree %q — a schema read must resolve "+
				"against the fetched repo, not the local project root", e.Name, e.SourceRoot, tree)
		}
	}
	for _, name := range []string{"charly-version", "go", "go-quality", "plugin"} {
		if !got[name] {
			t.Fatalf("the fetched charly tree's skill node %q is missing from the walk — its root "+
				"manifest must be read on the FETCHED path; got %v", name, got)
		}
	}
	if len(got) != 4 {
		t.Fatalf("walk emitted %d skill entities from the charly tree, want exactly 4: %v", len(got), got)
	}
}
