package buildkitdrive

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// BuildctlBinary is the CLI the drive speaks to the daemon with.
const BuildctlBinary = "buildctl"

// Build is one image build, expressed as the inputs the drive needs — daemon-agnostic
// and kind-blind: it names a Containerfile, a context and the reference the image must
// end up resolvable BY, never a charly kind, a box, or a candy.
type Build struct {
	// Containerfile is the Containerfile to build.
	Containerfile string
	// Context is the build context directory.
	Context string
	// Ref is the reference the image must be RESOLVABLE BY once the build returns.
	// Under the OCI-export bridge the daemon cannot honour it by itself — see
	// LoadExport in bridge.go, where re-applying it after the load is a named duty.
	Ref string
	// Addr is the daemon address; empty means the caller's already-configured default.
	Addr string
	// Platforms are target platforms, empty for the daemon's own default.
	Platforms []string
	// BuildArgs become --opt build-arg:NAME=VALUE.
	BuildArgs map[string]string
	// NoCache bypasses the daemon's cache lookup for this build.
	NoCache bool
	// OCILayouts are local OCI image layouts the build may read, each addressed from a
	// Containerfile or a `FROM` as `oci-layout://<id>`. They are rendered as
	// `--oci-layout <id>=<dir>`, the same `id=path` grammar ParseOCILayoutSpec accepts and
	// the same form buildctl documents ("Allow build access to the local OCI layout").
	//
	// Rendering the flag is the whole job: buildctl parses it into SolveOpt.OCIStores
	// itself (cmd/buildctl/build.go), so a drive that shells out needs no session code.
	OCILayouts []OCILayout
}

// Validate reports whether the build can be expressed at all. Every failure here is a
// programming error in the caller and is returned rather than papered over with a
// default: a relative Containerfile path or an empty Ref would build something the
// caller cannot then name.
func (b Build) Validate() error {
	if strings.TrimSpace(b.Containerfile) == "" {
		return fmt.Errorf("buildkitdrive: build: no Containerfile")
	}
	if !filepath.IsAbs(b.Containerfile) {
		return fmt.Errorf("buildkitdrive: build: Containerfile %q must be absolute", b.Containerfile)
	}
	if strings.TrimSpace(b.Context) == "" {
		return fmt.Errorf("buildkitdrive: build: no context")
	}
	if !filepath.IsAbs(b.Context) {
		return fmt.Errorf("buildkitdrive: build: context %q must be absolute", b.Context)
	}
	if strings.TrimSpace(b.Ref) == "" {
		return fmt.Errorf("buildkitdrive: build: no Ref — the image would be unaddressable")
	}
	return nil
}

// Args renders the `buildctl build` invocation whose output is an OCI layout archive.
//
// ⚑ The export is the whole point of this shape. The daemon writes into its OWN content
// store, which the run engine cannot read (the two share no store), so the drive's job is
// to hand the image over as a portable archive; bridge.go owns the other half. The
// output type is therefore NOT a caller option: a build whose result the run engine
// cannot see is not a build the drive can hand back.
func (b Build) Args(dest string) ([]string, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(dest) == "" {
		return nil, fmt.Errorf("buildkitdrive: build: no export destination")
	}
	args := []string{"build", "--frontend", "dockerfile.v0"}
	if b.Addr != "" {
		args = append(args, "--addr", b.Addr)
	}
	args = append(args,
		"--local", "context="+b.Context,
		"--local", "dockerfile="+filepath.Dir(b.Containerfile),
		"--opt", "filename="+filepath.Base(b.Containerfile),
	)
	if len(b.Platforms) > 0 {
		args = append(args, "--opt", "platform="+strings.Join(b.Platforms, ","))
	}
	for _, k := range sortedKeys(b.BuildArgs) {
		args = append(args, "--opt", "build-arg:"+k+"="+b.BuildArgs[k])
	}
	if b.NoCache {
		args = append(args, "--no-cache")
	}
	for _, l := range b.OCILayouts {
		if err := l.validate(); err != nil {
			return nil, err
		}
		args = append(args, "--oci-layout", l.ID+"="+l.Dir)
	}
	return append(args, "--output", "type=oci,dest="+dest), nil
}

// ExportFile is the layout archive's path inside a stage directory.
func ExportFile(stageDir, ref string) string {
	return filepath.Join(stageDir, safeName(ref)+".oci.tar")
}

// safeName reduces a reference to something usable as a filename component.
func safeName(ref string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, ref)
}

// StageDir creates the directory an export will be written into. It lives beside the
// daemon's state root, so a build's intermediate archive never lands in a tmpfs.
func StageDir() (string, error) {
	root, err := StateRoot()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "export")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("buildkitdrive: export stage dir %s: %w", dir, err)
	}
	return dir, nil
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
