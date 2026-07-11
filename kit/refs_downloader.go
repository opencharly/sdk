package kit

// refs_downloader.go — the swappable remote-repo FETCH BACKEND seam (P7). The host dispatches every
// cache-miss download through a RefsDownloader; the DEFAULT (candy/plugin-refs, delegating to
// DownloadRepo below) fetches via git, and an alternative refs plugin can serve a different backend
// (OCI/S3-hosted candies) by registering a different RefsDownloader — the "alternative ref backends"
// unlock. This mirrors the loader's DocParser seam (sdk/loaderkit): a typed interface a compiled-in
// plugin implements alongside its provider, so the host calls it in-proc with no wire envelope.
//
// Only the DOWNLOAD is pluggable; the host keeps the fetch ORCHESTRATION (local-override resolution,
// cache-hit short-circuit, and the post-fetch schema auto-migration), because those compose core-only
// concerns (the command:migrate invoke, the superproject-identity override) — the boundary is the
// backend that turns a (repoPath, version) into a populated local cache tree.
type RefsDownloader interface {
	// Download fetches repoPath@version into the local repo cache and returns the cache path.
	// Called only on a cache MISS (the host checks IsRepoCached first).
	Download(repoPath, version string) (string, error)
}

// DefaultDownloader is the built-in git fetch backend — it delegates to DownloadRepo (git clone into
// the cache). The host uses it until a refs plugin registers a different RefsDownloader.
type DefaultDownloader struct{}

// Download implements RefsDownloader via the git DownloadRepo primitive.
func (DefaultDownloader) Download(repoPath, version string) (string, error) {
	return DownloadRepo(repoPath, version)
}
