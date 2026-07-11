package kit

import (
	"fmt"
	"os"
	"path/filepath"
)

// refs_cache.go — the remote-repo cache LOCATION helpers (P7). Pure path computation over
// $CHARLY_REPO_CACHE / ~/.cache/charly/repos, shared by core (reconcile / the collection walk)
// and the refs fetch backend (candy/plugin-refs) that clones into these paths.

// RepoCacheDir returns the cache directory for remote repos.
// Uses $CHARLY_REPO_CACHE env var if set, otherwise ~/.cache/charly/repos/.
func RepoCacheDir() (string, error) {
	if envDir := os.Getenv("CHARLY_REPO_CACHE"); envDir != "" {
		return envDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, ".cache", "charly", "repos"), nil
}

// RepoCachePath returns the cache path for a specific repo version.
// e.g. ~/.cache/charly/repos/github.com/org/repo@v1.0.0/
func RepoCachePath(repoPath, version string) (string, error) {
	cacheDir, err := RepoCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, repoPath+"@"+version), nil
}

// IsRepoCached checks if a repo version is already in the cache.
func IsRepoCached(repoPath, version string) (bool, error) {
	cachePath, err := RepoCachePath(repoPath, version)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
