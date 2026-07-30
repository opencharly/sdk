package loaderkit

import (
	"fmt"
	"os"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// scan_orchestrate.go — the candy-scan fetch-fixpoint ORCHESTRATION (K3 U4-b), relocated verbatim
// from charly/layers.go's scanCandyFromLocal so a plugin (candy/plugin-build) can run the whole
// scan→fetch→qualify→arbitrate→finalize pipeline without the host. Every genuinely host-coupled leg
// — the reachability-walk remote-ref collect, the git clone/cache (+ auto-migrate), and the
// per-candy remote manifest scan — is an INJECTED ScanSeams closure the caller supplies (charly
// captures its registry + refs backend; candy/plugin-build captures InvokeProvider/reverse legs in
// U6), exactly the opts-agnostic seam pattern loaderkit.ResolveProjectSeams established in U2. The
// pure mechanism — the fix-point queue, per-entity-version arbitration (PickCandyVersion), and the
// finalize choke point — lives here.

// RemoteDownload is DEFINED in the dedicated spec module (spec/spec/remote_download.go, #55 2b
// Class A); this alias keeps the scan mechanism that produces it (+ candy/plugin-build's resolve
// legs) terse.
type RemoteDownload = spec.RemoteDownload

// ScanSeams carries the host-coupled legs ScanCandyFromLocal reaches for the fetch fix-point. DEFINED
// in the dedicated spec module (spec/spec/scan_seams.go, #55 C3b-ii) so it can type the
// spec.ProjectLoader.ScanCandyFromLocal seam method charly core reaches instead of importing loaderkit;
// this forwarder (mirroring the RemoteDownload alias above) keeps loaderkit's own signature +
// candy/plugin-build's scanSeamsLeg call sites terse. The caller builds these as closures capturing its
// config/opts + host mechanisms (registry, refs backend); the pure fix-point below never inspects a
// package-main type.
type ScanSeams = spec.ScanSeams

// ScanCandyFromLocal is the scan pipeline's step-2-onward body (remote-ref collect, fix-point fetch,
// per-entity-version arbitration, host-completion + finalize), relocated verbatim from
// charly/layers.go's scanCandyFromLocal. A caller that already has a source of localScanned (the
// root project scan, or a namespace's own projectCandiesScanned set) reaches the SAME pipeline by
// supplying the seams. Behavior-identical to the pre-move function: same steps 2-5, same order.
// initCfg is the project init: vocabulary threaded into the FINAL finalize choke point (nil for a
// non-generate caller — matches the pre-move opts.InitCfg).
func ScanCandyFromLocal(localScanned map[string]spec.ScannedCandy, initCfg *buildkit.InitConfig, seams ScanSeams) (map[string]spec.CandyReader, error) {
	// 2. Collect remote refs from @-prefixed candy references, PLUS every local candy's raw
	// (pre-finalize) require:/candy: refs — see the host CollectRemoteRefs closure (withLocalRawRefs)
	// for why the wrapped-view walk CollectRemoteRefsOpts does on its own can't discover these alone.
	downloads, err := seams.CollectRemoteRefs(localScanned)
	if err != nil {
		return nil, err
	}

	if len(downloads) == 0 {
		return FinalizeScannedCandies(localScanned, initCfg), nil
	}

	// 3. Per-entity-version resolution. The git tag is ONLY the fetch coordinate;
	// the authority is each candy's own `version:`, read AFTER fetch. So fetch
	// EVERY distinct (repo, git-tag) referenced (directly or transitively),
	// collect each materialization as a candidate, then arbitrate per bare ref by
	// per-entity version (PickCandyVersion). A remote candy's plain-name
	// require:/candy: dep is a same-repo sibling at the SAME git tag; an @-ref
	// dep carries its own repo/git-tag. Fix-point until no new (repo, git-tag,
	// ref) surfaces, so cross-repo transitive closures are fully materialized.
	type repoVer struct{ repo, ver string }
	candidates := make(map[string][]spec.CandyCandidate) // bare ref -> all fetched materializations
	scanned := make(map[repoVer]map[string]bool)         // (repo, git-tag) -> refs already scanned
	defaultBranches := make(map[string]string)           // repo → resolved default branch

	queue := downloads
	for len(queue) > 0 {
		nextByKey := make(map[repoVer]map[string]bool)
		enqueue := func(repo, ver, bare string) error {
			if ver == "" {
				if b, ok := defaultBranches[repo]; ok {
					ver = b
				} else {
					b, err := kit.GitDefaultBranch(kit.RepoGitURL(repo))
					if err != nil {
						return fmt.Errorf("resolving default branch for %s: %w", repo, err)
					}
					defaultBranches[repo] = b
					ver = b
				}
			}
			key := repoVer{repo, ver}
			if scanned[key][bare] {
				return nil // this exact (repo, git-tag, ref) already scanned
			}
			if nextByKey[key] == nil {
				nextByKey[key] = make(map[string]bool)
			}
			nextByKey[key][bare] = true
			return nil
		}

		for _, dl := range queue {
			key := repoVer{dl.RepoPath, dl.Version}
			done := scanned[key]
			if done == nil {
				done = make(map[string]bool)
				scanned[key] = done
			}
			wantRefs := make(map[string]bool)
			for _, ref := range dl.Refs {
				if !done[ref] {
					wantRefs[ref] = true
				}
			}
			if len(wantRefs) == 0 {
				continue
			}
			cachePath, err := seams.EnsureRepo(dl.RepoPath, dl.Version)
			if err != nil {
				return nil, fmt.Errorf("downloading %s:%s: %w", dl.RepoPath, dl.Version, err)
			}
			remoteCandies, err := seams.ScanRemote(cachePath, dl.RepoPath, wantRefs)
			if err != nil {
				return nil, fmt.Errorf("scanning %s:%s: %w", dl.RepoPath, dl.Version, err)
			}
			for ref := range wantRefs {
				done[ref] = true
			}
			for ref, sc := range remoteCandies {
				if sc.Model.Version == "" {
					return nil, fmt.Errorf("remote candy %q (from %s@%s) declares no version:; its producer repo must declare one", ref, dl.RepoPath, dl.Version)
				}
				candidates[ref] = append(candidates[ref], spec.CandyCandidate{
					Scanned: sc,
					Version: sc.Model.Version,
					GitTag:  dl.Version,
					Source:  dl.RepoPath + "@" + dl.Version,
				})

				// Enqueue this materialization's transitive deps. A plain-name dep
				// is a same-repo sibling at the SAME git tag; an @-ref dep carries
				// its own pinned repo/git-tag.
				enqueueDep := func(dep spec.CandyRefEntry) error {
					if dep.IsRemote() {
						p := spec.ParseRemoteRef(dep.Raw)
						return enqueue(p.RepoPath, p.Version, dep.Bare())
					}
					return enqueue(dl.RepoPath, dl.Version, dl.RepoPath+"/"+sc.View.SubPathPrefix+dep.Raw)
				}
				for _, dep := range sc.Refs.Require {
					if err := enqueueDep(dep); err != nil {
						return nil, err
					}
				}
				for _, dep := range sc.Refs.IncludedCandy {
					if err := enqueueDep(dep); err != nil {
						return nil, err
					}
				}
			}
		}

		queue = nil
		for key, refs := range nextByKey {
			refList := make([]string, 0, len(refs))
			for r := range refs {
				refList = append(refList, r)
			}
			queue = append(queue, RemoteDownload{RepoPath: key.repo, Version: key.ver, Refs: refList})
		}
	}

	// 4. Arbitrate each bare ref by per-entity version; materialize the winner.
	combined := make(map[string]spec.ScannedCandy, len(localScanned)+len(candidates))
	for name, sc := range localScanned {
		combined[name] = sc
	}
	for ref, cands := range candidates {
		winner := PickCandyVersion(ref, cands)
		if _, ok := localScanned[winner.Scanned.Model.Name]; ok {
			fmt.Fprintf(os.Stderr, "Note: local candy %q shadows remote candy %q\n", winner.Scanned.Model.Name, ref)
		}
		combined[ref] = winner.Scanned
	}

	// 5. Host-completion (InitSystems, initCfg-gated — nil by default, matching every caller but
	// generate.go; then RunOps + the HasInstallFiles/HasContent fold, unconditional) THEN finalize
	// (bare-string the refs) THEN wrap into the FINAL spec.CandyReader — ONE choke point, over the
	// COMBINED local+remote set.
	return FinalizeScannedCandies(combined, initCfg), nil
}
