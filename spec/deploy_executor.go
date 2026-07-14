package spec

// deploy_executor.go — the InstallPlan IR's EXECUTION contract (P4 "IR E-envelope"):
// the DeployExecutor interface every host-side executor implements, the BuilderRunOpts
// it carries, and the EmitOpts cross-cutting toggle bundle. These live in spec (not
// deploykit) because they are part of the E-envelope an out-of-process deploy/step
// plugin resolves against — EmitOpts.ParentExec threads a DeployExecutor through the
// nested-deploy tree, and the executor IMPLEMENTATIONS (sdk/kit) implement THIS
// interface. Homing them in spec keeps sdk/kit's own reverse-channel executor
// (kit.DeployExecutor, a DISTINCT interface) collision-free.

import (
	"context"
	"errors"
)

// ErrNotSupported is returned by RunInteractive/RunStream on a venue that cannot host a
// LIVE-STDIO session (a capture-only / non-interactive executor). Callers surface it as
// "shell/logs not supported on this substrate" — an explicit, compile-forced failure, never
// a silent no-op (F12; the honest form of the executor-matrix ErrNotSupported path).
var ErrNotSupported = errors.New("live-stdio (interactive/stream) not supported on this venue")

// DeployExecutor is the in-process host-side executor: the concrete venue an
// InstallPlan's steps are applied against (ShellExecutor on the operator's
// machine, SSHExecutor over ssh, NestedExecutor through a jump). The three
// implementations live in sdk/kit; the InstallPlan IR (EmitOpts.ParentExec)
// carries one through the nested-deploy tree. DISTINCT from kit.DeployExecutor
// (the reverse-channel executor a plugin serves over gRPC) — same role, different
// wire shape (typed EmitOpts + file paths here; JSON-marshalled opts + byte content there).
type DeployExecutor interface {
	// Venue returns a stable identifier for where this executor's
	// commands physically run. Examples:
	//
	//   "local"                            — ShellExecutor.
	//   "ssh://arch@127.0.0.1:2224"        — SSHExecutor.
	//   "nested:podman exec stack/local"   — NestedExecutor over local.
	//   "nested:ssh vm/local"              — NestedExecutor over SSH.
	//
	// The string is used as a map key for per-venue ledgers, so it
	// must be stable across invocations for the same logical target.
	// Not a URL — don't parse it; just compare.
	Venue() string

	// RunSystem executes a bash script with root privileges. On the
	// host, this is `sudo bash -s <<<script`; on the VM target, it's
	// `ssh <user>@<host> sudo bash -s <<<script`. The script body runs
	// with set -e semantics at the caller's discretion.
	RunSystem(ctx context.Context, script string, opts EmitOpts) error

	// RunUser executes a bash script as the invoking user (no sudo).
	// On the host, it's `bash -s <<<script`; on VM, `ssh <user>@<host>
	// bash -s <<<script` where <user> is the unprivileged guest user.
	RunUser(ctx context.Context, script string, opts EmitOpts) error

	// RunBuilder invokes the multi-stage builder image (podman run
	// <builder>) to compile pixi/npm/cargo/aur artifacts. On the host
	// this calls the existing BuilderRun helper. On VM deploys, the
	// builder runs *on the host* and artifacts are scp'd into the
	// guest via PutFile — podman inside the guest is not required.
	RunBuilder(ctx context.Context, opts BuilderRunOpts) ([]byte, error)

	// PutFile places a file at a remote path. ownerRoot == true means
	// the file is chown'd to root:root and chmod'd according to mode.
	// On the host, this is a plain os.WriteFile (plus sudo chown when
	// ownerRoot). On VM, this is scp into a tmp location followed by
	// `sudo install -m <mode> -o root -g root` on the guest.
	PutFile(ctx context.Context, localPath, remotePath string, mode uint32, ownerRoot bool, opts EmitOpts) error

	// GetFile retrieves the contents of a file on the venue. asRoot==true
	// runs the read via sudo to handle paths the deploying user cannot
	// access (e.g. /etc/rancher/k3s/k3s.yaml on a k3s server). On the
	// host, this is os.ReadFile (or `sudo cat` when asRoot). On VM, this
	// is `ssh <host> sudo cat <path>` with stdout captured. On nested
	// executors, delegates through the jump via the parent's own RunSystem
	// semantics. Used by layer_artifacts.go to publish files back to the
	// operator after deploy completion.
	GetFile(ctx context.Context, remotePath string, asRoot bool, opts EmitOpts) ([]byte, error)

	// RunCapture executes a single shell command (or short bash script) on
	// the venue and returns stdout/stderr/exit/err separately. Used by the
	// declarative test runner (testrun.go) to probe target state without
	// the streamed-output ergonomics of RunSystem/RunUser. No root
	// escalation — callers add `sudo` explicitly when needed; mirrors the
	// previous test-time Executor.Exec semantics. After the executor-
	// hierarchy cutover (2026-04), this is the single capture-output
	// method used by every probe across `charly check live`, `charly check box`, and
	// `charly check` scoring.
	RunCapture(ctx context.Context, script string) (stdout, stderr string, exit int, err error)

	// RunInteractive runs a command on the venue wired to the operator's LIVE TTY:
	// stdin/stdout/stderr are INHERITED from the host charly process (the reverse-channel
	// server runs IN that process, so os.Std* IS the operator's terminal). The child
	// (`podman exec -it` / `ssh -t`) owns the PTY, resize (SIGWINCH), and Ctrl-C. Blocks
	// until the session ends; returns its exit code. NOT ctx-deadlined — the TTY owns its
	// lifetime (the hostBuildCli doctrine). Stdio NEVER crosses the plugin wire — only the
	// script + the exit code (F12; the live-stdio sibling of RunCapture). Consumers: `charly
	// shell` (-it) and `charly cmd` (-i, stdin piped). A venue that cannot host an interactive
	// session returns ErrNotSupported.
	RunInteractive(ctx context.Context, script string) (exit int, err error)

	// RunStream runs a command on the venue streaming stdout/stderr LIVE to the operator
	// (inherited os.Stdout/os.Stderr; NO stdin). Blocks until exit. Same host-holds-the-
	// terminal semantics as RunInteractive (F12). Consumer: `charly logs --follow`. Returns
	// ErrNotSupported on a venue that cannot stream.
	RunStream(ctx context.Context, script string) (exit int, err error)

	// Kind returns a coarse classification of the venue used by the test
	// runner for reporting and skip decisions. Values:
	//   "host"      — ShellExecutor (operator's machine)
	//   "container" — NestedExecutor with JumpPodmanExec / JumpDockerExec
	//   "vm"        — SSHExecutor or NestedExecutor with JumpSSH/JumpVirshConsole
	// Replaces the test-time Executor.Kind() method deleted in the
	// 2026-04 executor-hierarchy cutover.
	Kind() string

	// ResolveHome returns the absolute path of $HOME for the named user
	// on the venue. Empty user means "the executor's default user" (the
	// invoking operator for ShellExecutor; the SSH login user for
	// SSHExecutor). Implementations consult `getent passwd` so they
	// don't depend on $HOME being set in the calling environment — that
	// matters for SSH executors where the operator's $HOME has nothing
	// to do with the remote user's home, and for ShellExecutor when the
	// caller wants a different user's home (e.g. running as root but
	// resolving an unprivileged user's home).
	//
	// Bundled as part of the 2026-05 shell:-schema cutover. Replaces the
	// `the local deploy target.HostHome = os.Getenv("HOME")` static-field
	// initialization that mis-targeted SSH deploys: the operator's
	// $HOME is not the remote user's home, so every shell-rc edit
	// (env.d sourcing block included) was landing in the wrong place
	// for `host: user@machine` deploys.
	ResolveHome(ctx context.Context, user string) (string, error)
}

// BuilderRunOpts describes one `podman run <builder>` invocation. The BuilderRun
// implementation (host-engine builder exec) lives in sdk/kit; this options struct
// is spec-homed because DeployExecutor.RunBuilder carries it across the IR.
type BuilderRunOpts struct {
	Engine       string // "podman" or "docker"; default "podman"
	BuilderImage string // full image ref, e.g. "ghcr.io/opencharly/fedora-builder:latest"
	CandyDir     string // absolute path to candy source (bind-mounted as /work)
	ScriptBody   string // shell script contents to pass to bash -c

	// ResolveImage + EnsureImage inject the image-presence behavior that used to
	// read *Config directly (Config stays a charly kernel type; the executor is an
	// sdk mechanism reached through this seam). ResolveImage maps a short/namespaced
	// builder ref to its concrete podman storage key (side-effect-free — safe in the
	// dry-run path). EnsureImage guarantees the image is present, falling back to a
	// local `charly box build <basename>` when it is project-buildable. The charly
	// callers close over Cfg + ProjectDir; see charly/ensure_image.go.
	ResolveImage func(image string) (string, error)
	EnsureImage  func(ctx context.Context, image string) error

	// Bind-mounts. Keys are container paths; values are host paths.
	// Set by the caller based on the builder kind — pixi/npm/cargo use
	// the same HOME-subdir layout, aur uses a tmpdir for /tmp/aur-pkgs.
	BindMounts map[string]string

	// Env vars to set inside the container (in addition to HOME).
	Env map[string]string

	// HostHome is the invoking user's absolute home dir. Set via HOME=
	// inside the container so path-baking (pixi shebangs, etc.) resolves
	// to a path that's valid both inside (via bind-mount) and outside.
	HostHome string

	// DryRun returns the command line that would run without executing.
	// Used for --dry-run deploy.
	DryRun bool

	// RunAsRoot spawns the container as UID 0 instead of the host's
	// UID. Needed for builders whose script body uses `sudo` against
	// users that don't exist in the builder image's /etc/passwd —
	// e.g. AUR's makepkg+yay flow inside a non-OCI-staged builder
	// image. Under rootless podman, root-in-container maps to the
	// host's UID, so file ownership in bind-mounts stays correct.
	RunAsRoot bool
}

// EmitOpts carries cross-cutting toggles passed by command-line flags.
// Gates are checked per-step by the target; target-specific options (the
// container target's registry auth, the host target's --yes, --dry-run)
// are bundled here too.
type EmitOpts struct {
	DryRun               bool
	FormatJSON           bool // print IR as JSON on stdout instead of table
	AllowRepoChanges     bool
	AllowRootTasks       bool
	WithServices         bool
	SkipIncompatible     bool
	AssumeYes            bool // skip sudo preflight, confirmation prompts
	Verify               bool // run layer tests after install
	Pull                 bool // force re-fetch of remote refs / image pull
	BuilderImageOverride string

	// ParentExec is the DeployExecutor of the parent deployment in a
	// nested tree. Non-nil iff this target is dispatched as a child of
	// another — BundleAddCmd's tree walker builds the chain root-first
	// and passes the immediate ancestor's executor here. Targets that
	// support being nested (host, container, vm) compose their own
	// executor over ParentExec via NestedExecutor; leaf-only targets
	// (kubernetes) ignore it and error if non-nil.
	//
	// When nil, the target runs against its natural root venue
	// (ShellExecutor for host, a fresh SSHExecutor for vm, etc.)
	// — preserving the flat-schema behavior for v2 configs that happen
	// to have no `children:`.
	ParentExec DeployExecutor

	// ParentNode is the BundleNode above this target in the tree.
	// Useful for targets that need parent-level context beyond the
	// executor (e.g. a vm child wants to know its parent container's
	// name to wire network forwarding). nil at the root.
	ParentNode *BundleNode

	// Path is the dotted-path identifier of this node (e.g.
	// "stack.web.db"). Used for logging + ledger keying.
	Path string
}

// ContextOrDefault returns opts' context if one's attached, or a background context.
func (o EmitOpts) ContextOrDefault() context.Context {
	return context.Background()
}
