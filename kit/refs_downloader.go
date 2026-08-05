package kit

import "github.com/opencharly/spec/spec"

// refs_downloader.go — the swappable remote-repo FETCH BACKEND seam (P7). The interface itself
// relocated to sdk/spec (FLOOR-SLIM axis-A mechanical batch, alongside DocParser/ProjectWalker/
// CandyScanner) so charly core's plugin_inproc.go can type-assert against it without importing
// kit; aliased here so every existing kit.RefsDownloader reference (candy/plugin-refs,
// charly/refs_threaded.go) keeps compiling unchanged. Only the DOWNLOAD is pluggable — the backend
// that turns a (repoPath, version) into a populated local cache tree. The fetch ORCHESTRATION
// (local-override resolution, cache-hit short-circuit, and the post-fetch schema auto-migration)
// used to be a P7-era "the host keeps it" boundary; K1 unit 4 relocated it to
// sdk/loaderkit.EnsureRepoDownloaded (reached through the ProjectLoader seam), since the v2
// end-state ("core does not parse config, resolve, build, deploy, or check") supersedes that
// earlier boundary the same way K1 unit 1 already superseded materialize.go's own former "stays
// core, clause M" self-classification. The host still supplies the registry-touching legs
// (the resolved RefsDownloader, the migrate-command dispatch) as spec.RefsCollectSeams callbacks.
type RefsDownloader = spec.RefsDownloader

// DefaultDownloader is the built-in git fetch backend — it delegates to DownloadRepo (git clone into
// the cache). The host uses it until a refs plugin registers a different RefsDownloader. Stays in
// kit (real git-clone I/O, not a pure seam contract).
type DefaultDownloader struct{}

// Download implements RefsDownloader via the git DownloadRepo primitive.
func (DefaultDownloader) Download(repoPath, version string) (string, error) {
	return DownloadRepo(repoPath, version)
}
