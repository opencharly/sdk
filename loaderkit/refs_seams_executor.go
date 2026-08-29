package loaderkit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/opencharly/sdk"
	"github.com/opencharly/spec/proc"
	"github.com/opencharly/spec/refs"
	"github.com/opencharly/spec/spec"
)

// refs_seams_executor.go — the ONE executor-backed spec.RefsCollectSeams builder (K-wave 2 cone R1,
// A2). It is the sibling of load_via_executor.go / resolve_via_executor.go in this package: the
// placement-invariant "build the registry-coupled legs from the reverse channel the host threads on
// ctx" helper every plugin that drives a FETCH shares.
//
// It lived as a private `refsSeams` in candy/plugin-loader until A2. candy/plugin-build then needed
// the identical value to call EnsureRepoDownloaded / CollectRemoteRefsOpts itself instead of
// round-tripping through charly's `buildengine-ensure-repo` / `buildengine-collect-remote-refs` host
// legs — and a second private copy there is exactly the R3 duplicate this program removes, so the
// builder is hoisted here and candy/plugin-loader now calls it. Same consolidation the sdk kit
// already applies to LoaderExecutor (load_via_executor.go's doc comment records the precedent).
//
// Every leg goes through the ONE dispatch any plugin uses to reach a peer — the host's
// InvokeProvider, retrieved from the in-proc executor the host threads onto ctx
// (sdk.ExecutorFromContext). The host's own InvokeProvider lazily connects an unregistered target,
// so a peer not yet reached in this process still resolves.

// RefsSeamsFromContext builds the remote-repo fetch legs for one call. ctx MUST carry the host
// reverse channel; a caller without one is a contract bug, not a degraded mode, so it fails loudly
// rather than silently fetching nothing.
func RefsSeamsFromContext(ctx context.Context) (spec.RefsCollectSeams, error) {
	ex, ok := sdk.ExecutorFromContext(ctx)
	if !ok {
		return spec.RefsCollectSeams{}, fmt.Errorf("refs: no host reverse channel on context (loader not compiled-in?)")
	}
	return RefsSeamsFromExecutor(ctx, ex), nil
}

// RefsSeamsFromExecutor is the explicit-executor form, for a caller that already holds the
// *sdk.Executor its Invoke was handed (candy/plugin-build's resolve legs) rather than fishing it
// back off ctx.
func RefsSeamsFromExecutor(ctx context.Context, ex *sdk.Executor) spec.RefsCollectSeams {
	return spec.RefsCollectSeams{
		Downloader:   peerDownloader{ctx: ctx, ex: ex},
		MigrateCache: func(path string) error { return migrateCacheViaPeer(ctx, ex, path) },
		ResolveLocal: func(body json.RawMessage) (*spec.ResolvedLocal, error) {
			return resolveLocalViaPeer(ctx, ex, body)
		},
		// The env var NAME lives in spec/proc (shared with candy/plugin-check's bed session, which
		// computes its own override independently); reading its VALUE is plain os.Getenv, never
		// something core had to do for us.
		OverrideEnvValue: os.Getenv(proc.RepoOverrideEnv),
		// The centralized git layer: a project-scoped GitClient whose cache lives
		// under the project's charly dir (or the repo cache dir). Every git
		// operation in the loader goes through it, so a git command runs only when
		// the answer is not already cached (issue #423, #208).
		LatestTag: gitClient().LatestTag,
	}
}

// gitClient is the process-wide centralized git layer (spec/refs.GitClient). Its
// cache lives under the project's charly dir when CHARLY_PROJECT_DIR is set, else
// the repo cache dir. Constructed once and shared by every loader consumer.
var gitClientOnce sync.Once
var gitClientInstance *refs.GitClient

func gitClient() *refs.GitClient {
	gitClientOnce.Do(func() {
		dir := os.Getenv(spec.ProjectDirEnv)
		if dir != "" {
			dir = filepath.Join(dir, ".charly", "cache")
		}
		gitClientInstance = refs.NewGitClient(dir)
	})
	return gitClientInstance
}

// peerDownloader is the spec.RefsDownloader face of the registered refs BACKEND, reached over
// InvokeProvider instead of a typed in-proc handle. The backend itself is unchanged and unaware:
// candy/plugin-refs serves the same Download behind both its typed method and its OpResolve leg.
// Swapping the refs plugin still swaps the backend — that is the whole point of the seam, and it
// survives this move intact.
type peerDownloader struct {
	ctx context.Context
	ex  *sdk.Executor
}

func (d peerDownloader) Download(repoPath, version string) (string, error) {
	params, err := json.Marshal(spec.RefsDownloadInput{RepoPath: repoPath, Version: version})
	if err != nil {
		return "", fmt.Errorf("refs download: marshal input: %w", err)
	}
	resJSON, err := d.ex.InvokeProvider(d.ctx, "refs", "refs", sdk.OpResolve, params, nil, sdk.InvokeProviderOpts{})
	if err != nil {
		return "", err
	}
	var reply spec.RefsDownloadReply
	if len(resJSON) > 0 {
		if uerr := json.Unmarshal(resJSON, &reply); uerr != nil {
			return "", fmt.Errorf("refs download: decode reply: %w", uerr)
		}
	}
	if reply.Dir == "" {
		return "", fmt.Errorf("refs download: backend returned no cache dir for %s@%s", repoPath, version)
	}
	return reply.Dir, nil
}

// migrateCacheViaPeer brings a freshly-fetched remote-repo cache's PROJECT files up to the head
// schema, via the compiled-in command:migrate plugin. `--project-only` never touches the per-host
// overlay (a remote fetch must not mutate the user's deploy state); `--quiet` discards progress
// output; `--dir` targets the cache tree. Byte-identical argv to the deleted core
// autoMigrateCacheProjectOnly.
func migrateCacheViaPeer(ctx context.Context, ex *sdk.Executor, path string) error {
	params, err := json.Marshal(map[string]any{"args": []string{"--project-only", "--quiet", "--dir", path}})
	if err != nil {
		return err
	}
	_, err = ex.InvokeProvider(ctx, "command", "migrate", sdk.OpRun, params, nil, sdk.InvokeProviderOpts{})
	return err
}

// resolveLocalViaPeer projects one opaque `kind:local` template body into a *spec.ResolvedLocal via
// candy/plugin-substrate's OpResolve leg — the same request envelope charly's own
// substrate_template_resolve.go builds.
func resolveLocalViaPeer(ctx context.Context, ex *sdk.Executor, body json.RawMessage) (*spec.ResolvedLocal, error) {
	params, err := json.Marshal(spec.SubstrateTemplateResolveRequest{Local: &spec.LocalResolveInput{Local: body}})
	if err != nil {
		return nil, fmt.Errorf("local resolve: marshal input: %w", err)
	}
	resJSON, err := ex.InvokeProvider(ctx, "kind", "local", sdk.OpResolve, params, nil, sdk.InvokeProviderOpts{})
	if err != nil {
		return nil, err
	}
	var reply spec.LocalResolveReply
	if len(resJSON) > 0 {
		if uerr := json.Unmarshal(resJSON, &reply); uerr != nil {
			return nil, fmt.Errorf("local resolve: decode reply: %w", uerr)
		}
	}
	return reply.Resolved, nil
}
