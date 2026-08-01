package kit

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/opencharly/spec/refs"
)

// refs_git.go — the kit tail of the remote-repo GIT primitives (P7). The pure git-exec + string
// primitives + cache-path helpers RELOCATED to the spec/refs fabric slice (#55 fabric-primitive
// extraction); charly core reaches them via spec/refs directly. This file keeps ONLY:
//   - DiscoverRemoteCandy, which needs the kit-local layout constant DefaultCandyDir; and
//   - thin re-export shims for the primitives with a remaining sdk/candy caller (sdk/loaderkit +
//     candy/plugin-refs's DefaultDownloader), so those keep compiling unchanged. The shims are
//     tracked residue that die when loaderkit + plugin-refs migrate onto spec/refs in their cones.
var (
	RepoGitURL       = refs.RepoGitURL       // sdk/loaderkit
	GitDefaultBranch = refs.GitDefaultBranch // sdk/loaderkit
	CompareSemver    = refs.CompareSemver    // sdk/loaderkit + candy/plugin-box
	DownloadRepo     = refs.DownloadRepo     // kit refs_downloader.go DefaultDownloader
)

// DiscoverRemoteCandy returns the list of candy names in a remote repo directory
func DiscoverRemoteCandy(repoDir string) ([]string, error) {
	candiesDir := filepath.Join(repoDir, DefaultCandyDir)
	entries, err := os.ReadDir(candiesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
