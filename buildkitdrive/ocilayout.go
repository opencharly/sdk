// Package buildkitdrive: OCI-layout import support.
//
// A drive that shells out to `buildctl` cannot resolve an `oci-layout://` source,
// because that scheme is served by the CLIENT LIBRARY through a buildkit session
// content store — not by the CLI's own flags. So a drive built on exec alone can
// export an image and cannot import one, which is what makes a dependent solve
// impossible: the exporter deletes its OCI export after LoadExport and there is
// nothing left that a later build can read.
//
// This file is the missing half. It opens an OCI image layout (the on-disk form
// storekit writes: `oci-layout` + `index.json` + `blobs/<alg>/<digest>` — see
// storekit.LayoutFile) as a containerd content store, which is the value the
// buildkit session serves for an `oci-layout://` image source.
//
// The implementation mirrors buildkit's own reference parser
// (cmd/buildkit/build/ocilayout.go in module github.com/moby/buildkit), which is
// an internal command package and therefore not importable; the two lines of
// substance — `local.NewStore(dir)` and the id->store map — are kept identical so
// the two cannot drift in behaviour.
package buildkitdrive

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/plugins/content/local"

	"github.com/opencharly/sdk/storekit"
)

// OCILayout is one named layout exposed to a solve, addressed as `oci-layout://<ID>`.
type OCILayout struct {
	// ID is the name the solve refers to; buildkit resolves `oci-layout://<ID>`.
	ID string
	// Dir is the layout root — the directory holding `oci-layout` and `index.json`.
	Dir string
}

// ParseOCILayoutSpec parses an `id=path/to/layout` specification, the form buildctl's
// `--oci-layout` flag takes, so a drive can pass the same string it would have passed
// on the command line.
func ParseOCILayoutSpec(spec string) (OCILayout, error) {
	id, dir, ok := strings.Cut(spec, "=")
	if !ok || id == "" || dir == "" {
		return OCILayout{}, &OcilayoutSpecError{Spec: spec}
	}
	return OCILayout{ID: id, Dir: dir}, nil
}

// Store opens the layout as a containerd content store. Reading the layout through a
// content.Store is what makes an `oci-layout://` source resolvable inside a solve;
// nothing here mutates the layout.
func (l OCILayout) Store() (content.Store, error) {
	if err := l.validate(); err != nil {
		return nil, err
	}
	return local.NewStore(l.Dir)
}

// validate checks that the directory really is an OCI image layout.
//
// local.NewStore does NOT validate: pointed at an empty directory it happily initialises
// a fresh, empty layout, so a typo'd path yields a solve that reads no content and reports
// no error. Both consumers of a layout need the same check — Store() before opening it as
// a content store, and Build.Args before handing the path to buildctl — so it lives here
// once rather than in each of them.
func (l OCILayout) validate() error {
	if l.ID == "" {
		return &OcilayoutSpecError{Spec: l.Dir, Reason: "layout has no id (a solve addresses it as oci-layout://<id>)"}
	}
	for _, required := range []string{storekit.LayoutFile, "index.json"} {
		if _, err := os.Stat(filepath.Join(l.Dir, required)); err != nil {
			return &OcilayoutSpecError{Spec: l.Dir, Reason: "not an OCI image layout (missing " + required + ")"}
		}
	}
	return nil
}

// ContentStores opens every layout and returns the id -> store map a buildkit session
// serves. Duplicate ids are an error rather than a silent last-wins, because a
// silently shadowed layout is a solve reading content nobody asked it to read.
func ContentStores(layouts ...OCILayout) (map[string]content.Store, error) {
	out := make(map[string]content.Store, len(layouts))
	for _, l := range layouts {
		if _, dup := out[l.ID]; dup {
			return nil, &OcilayoutSpecError{Spec: l.ID, Reason: "duplicate layout id"}
		}
		cs, err := l.Store()
		if err != nil {
			return nil, err
		}
		out[l.ID] = cs
	}
	return out, nil
}

// OcilayoutSpecError reports a malformed `id=path` layout specification.
type OcilayoutSpecError struct {
	Spec   string
	Reason string
}

func (e *OcilayoutSpecError) Error() string {
	reason := e.Reason
	if reason == "" {
		reason = "want 'id=path/to/layout'"
	}
	return "oci-layout: invalid specification " + quote(e.Spec) + ": " + reason
}

func quote(s string) string { return "'" + s + "'" }
