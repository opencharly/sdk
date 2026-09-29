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
	// (pre-finalize) require:/candy: refs — see the CollectRemoteRefs closure (spec.WithLocalRawRefs)
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
	// materials[key][ref] is the fetched body of one (repo, git-tag, ref); refReferrers[key][ref]
	// is the UNION of the SCOPE labels (boxes) that referenced that ref anywhere in the closure.
	// Both accumulate across the fix-point rounds, so a ref reached by several independent boxes
	// carries ALL their scopes on ONE candidate — which is what lets the arbiter report a conflict
	// only WITHIN one box (charly#735 §2/§9).
	materials := map[repoVer]map[string]spec.ScannedCandy{}
	refReferrers := map[repoVer]map[string][]string{}
	addReferrers := func(key repoVer, ref string, scopes []string) {
		if refReferrers[key] == nil {
			refReferrers[key] = map[string][]string{}
		}
		for _, s := range scopes {
			if s == "" {
				continue
			}
			refReferrers[key][ref] = appendUnique(refReferrers[key][ref], s)
		}
	}
	scanned := make(map[repoVer]map[string]bool) // (repo, git-tag) -> refs already scanned
	defaultBranches := make(map[string]string)   // repo → resolved default branch

	queue := downloads
	for len(queue) > 0 {
		nextByKey := make(map[repoVer]map[string]bool)
		enqueue := func(repo, ver, bare string, scopes []string) error {
			if ver == "" {
				if b, ok := defaultBranches[repo]; ok {
					ver = b
				} else {
					b, err := gitClient().DefaultBranch(kit.RepoGitURL(repo))
					if err != nil {
						return fmt.Errorf("resolving default branch for %s: %w", repo, err)
					}
					defaultBranches[repo] = b
					ver = b
				}
			}
			key := repoVer{repo, ver}
			// Union the scopes even when this exact (repo, git-tag, ref) was already scanned in
			// an earlier round: the materialization is shared, but the box that reached it now is
			// a new referrer and must appear on the candidate.
			addReferrers(key, bare, scopes)
			if scanned[key][bare] {
				return nil // this exact (repo, git-tag, ref) already fetched; scopes unioned above
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
				// Union the download's OWN referrers even for an already-scanned ref, so the
				// candidate's scope set is complete by the time candidates are built below.
				addReferrers(key, ref, dl.RefReferrers[ref])
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
				if materials[key] == nil {
					materials[key] = make(map[string]spec.ScannedCandy)
				}
				materials[key][ref] = sc

				// A transitive dep inherits the SAME scope(s) as the ref that named it — the dep
				// is part of that referrer's composition, not a new one.
				scopes := dl.RefReferrers[ref]
				// Enqueue this materialization's transitive deps. A plain-name dep
				// is a same-repo sibling at the SAME git tag; an @-ref dep carries
				// its own pinned repo/git-tag.
				enqueueDep := func(dep spec.CandyRefEntry) error {
					if dep.IsRemote() {
						p := spec.ParseRemoteRef(dep.Raw)
						return enqueue(p.RepoPath, p.Version, dep.Bare(), scopes)
					}
					// A ROOT-LEVEL remote candy (the candy de-submodule cutover — a
					// standalone candy repo whose manifest lives at the repo root,
					// SubPathPrefix "") has NO siblings: enqueuing
					// repoPath+"/"+""+dep.Raw would fabricate a wrong sibling ref
					// (github.com/opencharly/layer-cuda/nvidia for a bare nvidia dep)
					// whose sub-path does not exist in the repo — the fetch fix-point's
					// mirror of QualifyRemoteSiblingDeps' skip (#160). The bare name is
					// left untouched and resolves against the scan set (the local
					// library or another downloaded remote).
					if sc.View.SubPathPrefix == "" {
						return nil
					}
					return enqueue(dl.RepoPath, dl.Version, dl.RepoPath+"/"+sc.View.SubPathPrefix+dep.Raw, scopes)
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
			queue = append(queue, RemoteDownload{
				RepoPath:     key.repo,
				Version:      key.ver,
				Refs:         refList,
				RefReferrers: refReferrers[key],
			})
		}
	}

	// Build one candidate per fetched (repo, git-tag, ref), carrying the UNION of the scopes that
	// referenced it. Building AFTER the fix-point (not inline) is what makes the union complete:
	// a ref reached again in a later round contributes its box's scope to the same candidate.
	candidates := make(map[string][]spec.CandyCandidate, len(materials))
	for key, byRef := range materials {
		for ref, sc := range byRef {
			candidates[ref] = append(candidates[ref], spec.CandyCandidate{
				Scanned:   sc,
				GitTag:    key.ver,
				Source:    key.repo + "@" + key.ver,
				Referrers: refReferrers[key][ref],
			})
		}
	}

	// 4. Arbitrate each bare ref by source git tag; materialize the winner.
	combined := make(map[string]spec.ScannedCandy, len(localScanned)+len(candidates))
	for name, sc := range localScanned {
		combined[name] = sc
	}
	for ref, cands := range candidates {
		winner := PickCandyVersion(ref, cands, seams.Diag)
		// The winner's version IS its source git tag (the authored per-entity `version:`
		// is gone), so stamp it onto the resolved model/view before it enters `combined`.
		ws := winner.Scanned
		ws.Model.Version = winner.GitTag
		ws.View.Version = winner.GitTag
		// SHADOWING IS EFFECTIVE, NOT ADVISORY. A local candy of the same name wins, so BOTH
		// keys under which that one logical candy is reachable — its bare name and this full
		// remote ref — must resolve to the LOCAL materialization. Keeping the remote body here
		// (which the note merely announced away) left the map carrying two rival bodies for one
		// globally-unique candy name, so any consumer that iterates the map acted twice on the
		// same candy with different content and Go's map order picked the winner. The
		// plugin loader is where that bit: it host-builds a plugin candy's SourceDir per entry,
		// so a shadowed plugin was built from the local tree or from the OLD pinned remote at
		// random — and a remote whose go-plugin handshake did not agree with the client
		// surfaced as an intermittent "incompatible API version" warning. Resolving both
		// keys to the local body makes the choice deterministic at the source instead of
		// per consumer.
		if local, ok := localScanned[ws.Model.Name]; ok {
			// Same reasoning as the skew advisory: route it through the seam so a caller can
			// collect it as data. It is INFO — the shadow is deliberate and effective, so it
			// must not gate. nil keeps today's stderr behaviour.
			if w := seams.Diag; w != nil {
				w(spec.DiagInfo, "local candy %q shadows remote candy %q", ws.Model.Name, ref)
			} else {
				fmt.Fprintf(os.Stderr, "Notice: local candy %q shadows remote candy %q\n", ws.Model.Name, ref)
			}
			combined[ref] = local
			continue
		}
		combined[ref] = ws
		// ALSO key the remote candy by its bare NAME, so a bare-name ref (a transitive
		// plain-name dep in a pinned tag, e.g. `versatiles-style` inside
		// pod-versatiles-frontend's list) resolves to this remote candy. The refs list
		// registers remote candies by their @github path; without the name key, the
		// build's global candy order (ResolveCandyOrder looks up layers[name]) fails
		// with "unknown candy" for every transitive bare-name dep. Guarded: a name
		// already taken (a local candy, or a same-named remote from another repo — the
		// plugin-mcp conflict class) is NOT overwritten; the conflict check surfaces it.
		if _, exists := combined[ws.Model.Name]; !exists {
			combined[ws.Model.Name] = ws
		}
	}

	// 5. Host-completion (InitSystems, initCfg-gated — nil by default, matching every caller but
	// generate.go; then RunOps + the HasInstallFiles/HasContent fold, unconditional) THEN finalize
	// (bare-string the refs) THEN wrap into the FINAL spec.CandyReader — ONE choke point, over the
	// COMBINED local+remote set.
	return FinalizeScannedCandies(combined, initCfg), nil
}
