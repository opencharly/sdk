package buildkitdrive

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Runner runs one external command and returns its combined output. It exists so the
// drive's own tests can assert the exact argv SEQUENCE — in particular that the tag step
// follows the load step — without a daemon, an engine, or a container runtime present.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
}

// ExecRunner is the production Runner.
type ExecRunner struct{}

// Run executes the command and folds its output into the error, so a failure names what
// the tool said rather than only its exit status.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// OCIIndex is the layout archive's entry point.
type OCIIndex struct {
	Manifests []OCIDescriptor `json:"manifests"`
}

// OCIDescriptor is one blob pointer inside the layout.
type OCIDescriptor struct {
	MediaType string            `json:"mediaType"`
	Digest    string            `json:"digest"`
	Size      int64             `json:"size"`
	Annots    map[string]string `json:"annotations"`
}

// OCIManifest is the image manifest blob. Its `config` descriptor is the one whose digest
// a container runtime uses as the image ID.
type OCIManifest struct {
	MediaType string          `json:"mediaType"`
	Config    OCIDescriptor   `json:"config"`
	Layers    []OCIDescriptor `json:"layers"`
}

// ErrNoImageInLayout is returned when the archive carries no image to load.
var ErrNoImageInLayout = errors.New("buildkitdrive: the OCI export carries no image manifest")

// ImageID reads the archive's image ID: index.json → the manifest blob → its config digest.
//
// The ID is the CONFIG digest because that is what a container runtime computes an image
// ID from, and it is the value the tag step below has to name. It is read from the ARCHIVE
// rather than scraped from `load`'s stdout: the load's output is not a contract, while the
// layout is one — and the layout is the only place the value exists before the load.
func ImageID(tarPath string) (string, error) {
	manifestDigest, err := firstManifestDigest(tarPath)
	if err != nil {
		return "", err
	}
	// The canonical OCI-layout spelling first (blobs/sha256/<hex>), then the bare
	// <hex>.json some exporters emit: the ARCHIVE is the authority, not an assumption
	// about which spelling it used.
	hex := strings.TrimPrefix(manifestDigest, "sha256:")
	blob, err := readTarEntry(tarPath, "blobs/sha256/"+hex)
	if err != nil {
		blob, err = readTarEntry(tarPath, hex+".json")
		if err != nil {
			return "", fmt.Errorf("buildkitdrive: manifest blob %s: %w", manifestDigest, err)
		}
	}
	var manifest OCIManifest
	if err := json.Unmarshal(blob, &manifest); err != nil {
		return "", fmt.Errorf("buildkitdrive: manifest blob %s: %w", manifestDigest, err)
	}
	if manifest.Config.Digest == "" {
		return "", fmt.Errorf("buildkitdrive: manifest %s names no config", manifestDigest)
	}
	return manifest.Config.Digest, nil
}

func firstManifestDigest(tarPath string) (string, error) {
	raw, err := readTarEntry(tarPath, "index.json")
	if err != nil {
		return "", fmt.Errorf("buildkitdrive: %s: %w", tarPath, err)
	}
	var idx OCIIndex
	if err := json.Unmarshal(raw, &idx); err != nil {
		return "", fmt.Errorf("buildkitdrive: index.json: %w", err)
	}
	if len(idx.Manifests) == 0 {
		return "", ErrNoImageInLayout
	}
	return idx.Manifests[0].Digest, nil
}

// readTarEntry returns the bytes of one entry, tolerating both the bare and the
// blobs/sha256/ spellings of an OCI layout.
func readTarEntry(tarPath, name string) ([]byte, error) {
	f, err := os.Open(tarPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // read-only open; the close error carries nothing
	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("no %s in the archive", name)
		}
		if err != nil {
			return nil, err
		}
		clean := strings.TrimPrefix(hdr.Name, "./")
		if clean == name {
			return io.ReadAll(tr)
		}
	}
}

// LoadExport hands a `buildctl` OCI export to the RUN engine, and is the half of the
// bridge that makes the image REACHABLE.
//
// ⚑ THE TAG RE-APPLICATION IS A NAMED DUTY OF THIS FUNCTION, NOT A CAVEAT. A container
// engine's `load` returns success while landing an OCI-exported image UNTAGGED
// (`<none>:<none>`), because the layout's index.json carries only
// `org.opencontainers.image.created` and no `org.opencontainers.image.ref.name`. So a
// build that "succeeds" this way leaves the image unaddressable by its own CalVer
// reference, and every downstream verb that resolves it by name (labels, deploy, status,
// clean) breaks. The duty is therefore three steps, not two — load, then tag by the
// archive's image ID, then VERIFY the reference resolves — and the verification is part
// of the function so a silent failure cannot pass for a successful build.
func LoadExport(ctx context.Context, r Runner, engineBinary, tarPath, ref string) error {
	if r == nil {
		return fmt.Errorf("buildkitdrive: LoadExport: no runner")
	}
	if strings.TrimSpace(engineBinary) == "" {
		return fmt.Errorf("buildkitdrive: LoadExport: no engine binary")
	}
	if strings.TrimSpace(ref) == "" {
		return fmt.Errorf("buildkitdrive: LoadExport: no ref")
	}
	id, err := ImageID(tarPath)
	if err != nil {
		return err
	}
	if _, err := r.Run(ctx, engineBinary, "load", "-i", tarPath); err != nil {
		return fmt.Errorf("buildkitdrive: load %s: %w", tarPath, err)
	}
	if _, err := r.Run(ctx, engineBinary, "tag", id, ref); err != nil {
		return fmt.Errorf("buildkitdrive: tag %s as %s: %w", id, ref, err)
	}
	// The reference must now RESOLVE. Without this step the duty could be skipped and the
	// build would still look green.
	if _, err := r.Run(ctx, engineBinary, "image", "inspect", "--format", "{{.Id}}", ref); err != nil {
		return fmt.Errorf("buildkitdrive: the loaded image is not resolvable as %s: %w", ref, err)
	}
	return nil
}
