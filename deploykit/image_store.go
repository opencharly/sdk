package deploykit

// image_store.go — W3.7's ONE image-store surface, addressed through the engine
// capability table so no code path spells an engine binary or branches on an
// engine word it does not have to.
//
// The operations an engine family actually differs on are narrow, and every one
// of them is a capability FACT rather than a name switch:
//
//   - the CLI binary                       spec.EngineBinaries[row.Binary]
//   - the presence argv                    row.ImageExistsArgv
//   - the daemon address / namespace       row.Address / row.Namespace, folded by
//                                          ResolveContainerdTarget into ContainerdTarget
//   - the rootful privilege prefix         row.PrivilegePrefix (via ContainerdTarget)
//
// Everything else is the same CLI shape for podman, docker and nerdctl (`load -i`,
// `pull`, `tag`, `rmi -f`, `images --format`). Keeping ONE implementation with
// those facts injected is what makes a second engine cost a capability row, not a
// sibling store implementation (R2).
//
// ROOTFUL CONTAINERD IDENTITY IS VERIFIED, NEVER ASSUMED. A permission-denied
// rootful socket silently reroutes `nerdctl` to the rootless daemon, so every
// operation against a rootful addressed store first runs `nerdctl info` and
// refuses when SecurityOptions reports `rootless` (W3.7, measured on the spike).

import (
	"context"
	"fmt"
	"strings"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/container"
	"github.com/opencharly/spec/shellquote"
	spec "github.com/opencharly/spec/spec"
)

// ImageStore is the ONE store surface the image resolver addresses, whatever the
// engine behind it. `Import` takes an OCI archive whose index carries the
// `io.containerd.image.name=<full-ref>` annotation; the load applies that
// reference with no tag step (measured on every engine family — see the W3.7
// annotation control).
type ImageStore interface {
	// Has reports whether the store holds ref by name. A store that answers
	// "absent" must not hide a daemon failure: only the engine's own
	// not-found exit is reported as false.
	Has(ctx context.Context, ref string) (bool, error)
	// Import loads the OCI archive at ociSource into the store and verifies
	// that the store addresses ref afterwards. A dangling import (the archive
	// carried no naming annotation) is an error, never a silent success.
	Import(ctx context.Context, ociSource, ref string) error
	// Pull fetches ref from its registry into the store.
	Pull(ctx context.Context, ref string) error
	// Tag applies dst to an already-present src.
	Tag(ctx context.Context, src, dst string) error
	// Remove best-effort removes every named reference.
	Remove(ctx context.Context, refs ...string) error
	// List returns every reference the store addresses, in the engine's order.
	List(ctx context.Context) ([]string, error)
}

const (
	// imageNameAnnotation is the annotation that NAMES an image inside an OCI
	// layout index. It is NOT org.opencontainers.image.ref.name: a buildkit
	// `--output type=oci,name=<full-ref>` export writes the full `<repo>:<tag>`
	// in io.containerd.image.name and only the TAG in ref.name (W3.7 spike).
	imageNameAnnotation = "io.containerd.image.name"

	// imageAbsentExit is the engine's own "no such image" exit. Every other
	// non-zero exit is a store/daemon failure and is surfaced, not folded into
	// a false absence.
	imageAbsentExit = 1

	// imageListFormat is the one listing template the three CLIs share. It
	// yields one `repo:tag` per line; untagged entries are filtered below.
	imageListFormat = "{{.Repository}}:{{.Tag}}"

	// importArgv is `<binary> load -i <archive>` for podman, docker and
	// nerdctl alike (docker 29.9.2 accepts the OCI layout directly).
	importArgv = "load"
)

// untaggedMarkers are the values an engine prints in place of a repository or
// tag for an image no reference names. A nerdctl OCI load also creates an
// `<untagged>` index image beside the named one; it is tolerated, never
// treated as a failure.
var untaggedMarkers = []string{"<none>", "<untagged>"}

// engineImageStore is the CLI-backed ImageStore. One implementation, configured
// entirely from the capability row and the resolved target.
type engineImageStore struct {
	exec    DeployExecutor
	engine  spec.EngineName
	binary  string
	rootful bool

	// globalArgs are the engine's daemon-addressing flags, already rendered and
	// quoted (`--address '<path>' --namespace '<ns>'`). Empty for an engine
	// with no daemon address fact (podman, docker).
	globalArgs []string
	address    string

	// privilegePrefix is applied before the binary when the target is rootful
	// AND the capability carries one (`sudo -n`). An engine whose rootful
	// posture is the executor's own carries no prefix.
	privilegePrefix string

	// hasArgv is the capability's presence argv (`image exists` for podman,
	// `image inspect` for docker/containerd).
	hasArgv []string
}

var _ ImageStore = (*engineImageStore)(nil)

// NewImageStore builds the store for an ALREADY-RESOLVED target. The engine
// word must be a real engine name (not `auto`, not a typo): the resolver that
// produced it already rejected both, and a store built for a fallback engine
// would be the silent-wrong-answer class this contract exists to remove.
func NewImageStore(exec DeployExecutor, target ContainerdTarget) (ImageStore, error) {
	if exec == nil {
		return nil, fmt.Errorf("image store: nil executor")
	}
	engine := string(target.Engine)
	if !container.IsEngineName(engine) {
		return nil, container.EngineValidationError("image store engine", engine)
	}
	capability, ok := kit.EngineCapabilityFor(engine)
	if !ok {
		return nil, fmt.Errorf("image store: engine %q has no capability row", engine)
	}
	if capability.Binary == "" {
		return nil, fmt.Errorf("image store: engine %q has no CLI binary fact", engine)
	}
	if len(capability.ImageExistsArgv) == 0 {
		return nil, fmt.Errorf("image store: engine %q has no presence argv fact", engine)
	}

	store := &engineImageStore{
		exec:    exec,
		engine:  spec.EngineName(engine),
		binary:  capability.Binary,
		rootful: target.Rootful,
		hasArgv: append([]string(nil), capability.ImageExistsArgv...),
	}

	// The daemon address is the capability fact that identifies an
	// addressed-daemon engine. Its resolved value comes from
	// ResolveContainerdTarget; the row's own value is the built-in default.
	if capability.Address != "" {
		address := target.Address
		if address == "" {
			address = capability.Address
		}
		namespace := target.Namespace
		if namespace == "" {
			namespace = capability.Namespace
		}
		store.address = address
		store.globalArgs = append(store.globalArgs,
			"--address", shellquote.ShellQuote(address),
			"--namespace", shellquote.ShellQuote(namespace))
	}

	if target.Rootful {
		store.privilegePrefix = target.PrivilegePrefix
	}

	return store, nil
}

// command renders one engine invocation from TOKENS. Fixed words (subcommands,
// flags) are passed verbatim; every caller-supplied VALUE is wrapped by
// shellquote.ShellQuote at the call site, so a reference can never be split by
// the shell while the engine's own vocabulary stays readable.
func (s *engineImageStore) command(tokens ...string) string {
	parts := make([]string, 0, 2+len(s.globalArgs)+len(tokens))
	if s.privilegePrefix != "" {
		parts = append(parts, s.privilegePrefix)
	}
	parts = append(parts, s.binary)
	parts = append(parts, s.globalArgs...)
	parts = append(parts, tokens...)
	return strings.Join(parts, " ")
}

// verifyDaemon refuses to treat a rootless daemon as the rootful store. It runs
// before EVERY operation (not once per store) because the silent reroute is
// per-invocation: the same command can land in a different daemon than the
// previous one.
func (s *engineImageStore) verifyDaemon(ctx context.Context) error {
	if !s.rootful || s.address == "" {
		return nil
	}
	stdout, stderr, code, err := s.exec.RunCapture(ctx, s.command("info"))
	if err != nil {
		return fmt.Errorf("verify the rootful store at %s: %w", s.address, err)
	}
	if code != 0 {
		return fmt.Errorf("verify the rootful store at %s: `%s info` exit %d: %s",
			s.address, s.binary, code, strings.TrimSpace(stderr))
	}
	for _, option := range securityOptions(stdout) {
		if option == "rootless" {
			return fmt.Errorf(
				"rootful store requested at %s, but `%s info` reports the ROOTLESS daemon (SecurityOptions: rootless): "+
					"a permission-denied rootful socket reroutes to rootless, so this operation would address the wrong store",
				s.address, s.binary)
		}
	}
	return nil
}

// securityOptions extracts the indented entries under `Security Options:` in
// `nerdctl info` output. A rootful daemon prints seccomp/cgroup entries (or
// nothing); a rootless daemon prints `rootless`, and that one word is the
// discriminator.
func securityOptions(info string) []string {
	var options []string
	inSection := false
	for _, line := range strings.Split(info, "\n") {
		trimmed := strings.TrimSpace(line)
		if !inSection {
			if strings.HasPrefix(trimmed, "Security Options:") {
				inSection = true
			}
			continue
		}
		if trimmed == "" {
			break
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			break
		}
		options = append(options, trimmed)
	}
	return options
}

// run issues one engine operation and surfaces a non-zero exit with its stderr.
func (s *engineImageStore) run(ctx context.Context, args ...string) error {
	_, stderr, code, err := s.exec.RunCapture(ctx, s.command(args...))
	if err != nil {
		return fmt.Errorf("%s %s: %w", s.binary, args[0], err)
	}
	if code != 0 {
		return fmt.Errorf("%s %s: exit %d: %s", s.binary, args[0], code, strings.TrimSpace(stderr))
	}
	return nil
}

// Has probes presence through the capability's own argv.
func (s *engineImageStore) Has(ctx context.Context, ref string) (bool, error) {
	if err := s.verifyDaemon(ctx); err != nil {
		return false, err
	}
	argv := make([]string, 0, len(s.hasArgv)+1)
	argv = append(argv, s.hasArgv...)
	argv = append(argv, shellquote.ShellQuote(ref))
	_, stderr, code, err := s.exec.RunCapture(ctx, s.command(argv...))
	if err != nil {
		return false, fmt.Errorf("probe %s in the %s store: %w", ref, s.engine, err)
	}
	switch code {
	case 0:
		return true, nil
	case imageAbsentExit:
		if err := s.confirmDaemonReachable(ctx); err != nil {
			return false, err
		}
		return false, nil
	default:
		return false, fmt.Errorf("probe %s in the %s store: exit %d: %s",
			ref, s.engine, code, strings.TrimSpace(stderr))
	}
}

// confirmDaemonReachable turns the engine's not-found exit into "absent" only
// when the daemon itself answered. docker and nerdctl both exit 1 for a missing
// image AND for an unreachable daemon, so reading an unqualified exit 1 as
// absence would hide a daemon failure — this interface explicitly forbids that.
// A rootful addressed store already proved identity at the top of the operation,
// so it needs no second probe.
func (s *engineImageStore) confirmDaemonReachable(ctx context.Context) error {
	if s.rootful && s.address != "" {
		return nil
	}
	_, stderr, code, err := s.exec.RunCapture(ctx, s.command("info"))
	if err != nil {
		return fmt.Errorf("the %s store answered \"not found\", but its daemon probe failed: %w", s.engine, err)
	}
	if code != 0 {
		return fmt.Errorf(
			"the %s store answered \"not found\", but `%s info` exits %d — the daemon is not reachable, so absence cannot be concluded: %s",
			s.engine, s.binary, code, strings.TrimSpace(stderr))
	}
	return nil
}

// Import loads the archive, then PROVES the store addresses ref. The proof is
// the annotation control at the engine boundary: an archive built without
// `io.containerd.image.name` lands untagged, so `Has` answers false and the
// import is refused rather than reported successful.
func (s *engineImageStore) Import(ctx context.Context, ociSource, ref string) error {
	if strings.TrimSpace(ociSource) == "" {
		return fmt.Errorf("import %s: no OCI archive source", ref)
	}
	if err := s.verifyDaemon(ctx); err != nil {
		return err
	}
	if err := s.run(ctx, importArgv, "-i", shellquote.ShellQuote(ociSource)); err != nil {
		return fmt.Errorf("import %s: %w", ociSource, err)
	}
	present, err := s.Has(ctx, ref)
	if err != nil {
		return err
	}
	if !present {
		return fmt.Errorf(
			"imported %s, but the %s store does not address %q: the OCI archive must carry the %s annotation naming the full reference "+
				"(an archive built without it lands untagged)",
			ociSource, s.engine, ref, imageNameAnnotation)
	}
	return nil
}

// Pull fetches ref from its registry.
func (s *engineImageStore) Pull(ctx context.Context, ref string) error {
	if err := s.verifyDaemon(ctx); err != nil {
		return err
	}
	return s.run(ctx, "pull", shellquote.ShellQuote(ref))
}

// Tag applies dst to src.
func (s *engineImageStore) Tag(ctx context.Context, src, dst string) error {
	if err := s.verifyDaemon(ctx); err != nil {
		return err
	}
	return s.run(ctx, "tag", shellquote.ShellQuote(src), shellquote.ShellQuote(dst))
}

// Remove is BEST-EFFORT, one reference per invocation: a missing ref must not
// stop the rest from being removed (the engine exits non-zero on the first miss
// and would otherwise leave later refs behind). The exit code is ignored by
// design; an executor failure — the store being unreachable — is still surfaced.
func (s *engineImageStore) Remove(ctx context.Context, refs ...string) error {
	if len(refs) == 0 {
		return nil
	}
	if err := s.verifyDaemon(ctx); err != nil {
		return err
	}
	for _, ref := range refs {
		if ref == "" {
			continue
		}
		if _, _, _, err := s.exec.RunCapture(ctx, s.command("rmi", "-f", shellquote.ShellQuote(ref))); err != nil {
			return fmt.Errorf("remove %s from the %s store: %w", ref, s.engine, err)
		}
	}
	return nil
}

// List returns the store's references, dropping untagged entries.
func (s *engineImageStore) List(ctx context.Context) ([]string, error) {
	if err := s.verifyDaemon(ctx); err != nil {
		return nil, err
	}
	stdout, stderr, code, err := s.exec.RunCapture(ctx, s.command("images", "--format", shellquote.ShellQuote(imageListFormat)))
	if err != nil {
		return nil, fmt.Errorf("list the %s store: %w", s.engine, err)
	}
	if code != 0 {
		return nil, fmt.Errorf("list the %s store: exit %d: %s", s.engine, code, strings.TrimSpace(stderr))
	}
	return parseImageList(stdout), nil
}

// parseImageList turns one `repo:tag` line per image into references, dropping
// the engine's untagged markers.
func parseImageList(output string) []string {
	refs := make([]string, 0, strings.Count(output, "\n")+1)
	for _, line := range strings.Split(output, "\n") {
		ref := strings.TrimSpace(line)
		if ref == "" || isUntaggedRef(ref) {
			continue
		}
		refs = append(refs, ref)
	}
	return refs
}

// isUntaggedRef reports whether any component is an engine placeholder.
func isUntaggedRef(ref string) bool {
	for _, marker := range untaggedMarkers {
		if strings.Contains(ref, marker) {
			return true
		}
	}
	return false
}
