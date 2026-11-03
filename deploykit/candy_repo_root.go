package deploykit

import (
	"path/filepath"
	"strings"
)

// candy_repo_root.go — the ONE definition of "the repo root of the repo that owns this candy".
//
// WHY IT IS A SHARED FACT AND NOT A LOCAL DETAIL. A skill body (and a sub-agent definition) lives
// at `<repo root>/.agents/skills/<name>/SKILL.md` — the repo root, because that is the directory a
// harness rooted at the repo discovers natively, and it is the base the marketplace corpus reader
// already resolves against (`plugin-marketplace/…/skillfile.go`, which joins the entity's
// SourceRoot — the REF root — with the owner's declared tree). The label collector used to resolve
// the same file against `<owner candy SourceDir>`, which is the same directory ONLY when the candy
// sits at its repo's root. For a candy under `candy/<name>/` the two bases diverge, and the corpus
// and the label then read two different trees:
//
//	$ charly marketplace: skill "arch-aur-test": no body — neither an inline `content:` nor a
//	  SKILL.md under [.agents/skills]
//
// measured on 48 bodies across 10 whole-repo refs (opencharly/marketplace#522). One base, one
// layout, two readers is the fix; this file holds the base.
//
// THE RULE, in the loader's own terms. A candy's SourceDir is one of exactly two shapes:
//
//   - `<repo>` — a ROOT-LEVEL standalone candy (the candy de-submodule cutover: the manifest is
//     the repo's own charly.yml). SubPathPrefix is "" and SourceDir IS the repo root.
//   - `<repo>/<subpath><name>` — a candy in a subdirectory of a multi-candy repo. The loader records
//     the subpath as `spec.CandyView.SubPathPrefix` for a REMOTE candy ("candy/", from
//     loaderkit.scanRepoCandyDir / scan_candy.go) and leaves it empty for a LOCAL one
//     (scan_candy.go: "filled by the resolve projector for remote candies, not at scan time"), so
//     the same shape is recovered structurally for a local candy: `<repo>/candy/<name>` is the
//     local project loader's ONLY candy placement (`discover: candy`), which is why the suffix is
//     recoverable at all rather than guessed.
//
// Nothing here probes the filesystem: the base is derived from the identity the loader already
// resolved, so a repository cannot be "found" by the presence of an `.agents` tree. A layout probe
// would accept both trees and put the dual-source state back (R5) — the base must be computable
// without looking at what happens to exist.
func candyRepoRoot(l CandyModel) (string, bool) {
	if l == nil {
		return "", false
	}
	dir := l.GetSourceDir()
	if dir == "" {
		return "", false
	}
	name := l.GetName()
	// A remote candy carries the subpath the loader recorded. Strip it; a SourceDir that does not
	// end with it means the resolved identity and the on-disk path disagree, which the caller must
	// treat as a finding rather than paper over with the candy dir.
	if prefix := l.GetSubPathPrefix(); prefix != "" {
		if name == "" {
			return "", false
		}
		trimmed, ok := strings.CutSuffix(dir, filepath.Join(prefix, name))
		if !ok {
			return "", false
		}
		root := strings.TrimRight(trimmed, string(filepath.Separator))
		return root, root != ""
	}
	// A local candy: the project loader's `candy/<name>` placement, recovered structurally.
	if name != "" {
		if trimmed, ok := strings.CutSuffix(dir, filepath.Join("candy", name)); ok {
			root := strings.TrimRight(trimmed, string(filepath.Separator))
			return root, root != ""
		}
	}
	// A root-level candy: the candy's directory IS the repo root.
	return dir, true
}
