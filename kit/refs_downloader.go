package kit

import "github.com/opencharly/sdk/spec"

// refs_downloader.go — the swappable remote-repo FETCH BACKEND seam (P7). The interface itself
// relocated to sdk/spec (FLOOR-SLIM axis-A mechanical batch, alongside DocParser/ProjectWalker/
// CandyScanner) so charly core's plugin_inproc.go can type-assert against it without importing
// kit; aliased here so every existing kit.RefsDownloader reference (candy/plugin-refs,
// charly/refs_threaded.go) keeps compiling unchanged. Only the DOWNLOAD is pluggable; the host
// keeps the fetch ORCHESTRATION (local-override resolution, cache-hit short-circuit, and the
// post-fetch schema auto-migration) — the boundary is the backend that turns a (repoPath, version)
// into a populated local cache tree.
type RefsDownloader = spec.RefsDownloader

// DefaultDownloader is the built-in git fetch backend — it delegates to DownloadRepo (git clone into
// the cache). The host uses it until a refs plugin registers a different RefsDownloader. Stays in
// kit (real git-clone I/O, not a pure seam contract).
type DefaultDownloader struct{}

// Download implements RefsDownloader via the git DownloadRepo primitive.
func (DefaultDownloader) Download(repoPath, version string) (string, error) {
	return DownloadRepo(repoPath, version)
}
