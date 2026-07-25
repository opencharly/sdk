package loaderkit

import (
	"errors"
	"testing"

	"github.com/opencharly/sdk/spec"
)

// TestMaterialize_NotFoundPolicy exercises every arm of Materialize's not-found dispatch policy —
// the four branches it runs when the DecodeEntity seam reports the provider registry has no
// provider for the node's discriminator. The registry-coupled DecodeEntity/BuildBundleEntity work
// is the host's (clause M); this test drives the pure policy with mock seams.
func TestMaterialize_NotFoundPolicy(t *testing.T) {
	cases := []struct {
		name       string
		disc       string
		found      bool          // DecodeEntity result
		decodeErr  error         // DecodeEntity error
		threaded   spec.Threaded // recognition snapshot (clause D)
		inConnect  bool          // InKindConnectPass result
		connectErr error         // DeclaredKindConnectError result
		wantBundle bool          // BuildBundleEntity must have been called
		wantErr    bool
	}{
		{
			name: "found by registry → folded, no fallback",
			disc: "pod", found: true,
			threaded: spec.Threaded{},
		},
		{
			name: "DecodeEntity error propagates",
			disc: "pod", decodeErr: errors.New("decode boom"),
			threaded: spec.Threaded{},
			wantErr:  true,
		},
		{
			name:       "recognized deploy substrate → routes to bundle builder",
			disc:       "exampledeploy",
			threaded:   spec.Threaded{DeploySubstrates: map[string]bool{"exampledeploy": true}},
			wantBundle: true,
		},
		{
			name:      "declared kind inside connect pre-pass → deferred (skip, no error)",
			disc:      "widget",
			threaded:  spec.Threaded{Kinds: map[string]bool{"widget": true}},
			inConnect: true,
		},
		{
			name:       "declared kind with retained connect error → warned + skipped (no error)",
			disc:       "widget",
			threaded:   spec.Threaded{Kinds: map[string]bool{"widget": true}},
			connectErr: errors.New("provider build failed"),
		},
		{
			name:     "declared kind, unconnected, not yet reached → warned + skipped (no error)",
			disc:     "widget",
			threaded: spec.Threaded{Kinds: map[string]bool{"widget": true}},
		},
		{
			name:     "unrecognized discriminator → hard load error",
			disc:     "bogus",
			threaded: spec.Threaded{},
			wantErr:  true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bundleCalled := false
			seams := spec.MaterializeSeams{
				DecodeEntity: func(spec.ParsedNode, *spec.MaterializedProject) (bool, error) {
					return c.found, c.decodeErr
				},
				BuildBundleEntity: func(spec.ParsedNode, *spec.MaterializedProject) error {
					bundleCalled = true
					return nil
				},
				InKindConnectPass:        func() bool { return c.inConnect },
				DeclaredKindConnectError: func(string) error { return c.connectErr },
			}
			acc := &spec.MaterializedProject{}
			err := Materialize(spec.ParsedNode{Name: "n", Disc: c.disc}, c.threaded, seams, acc)
			if (err != nil) != c.wantErr {
				t.Fatalf("Materialize err = %v, wantErr %v", err, c.wantErr)
			}
			if bundleCalled != c.wantBundle {
				t.Fatalf("BuildBundleEntity called = %v, want %v", bundleCalled, c.wantBundle)
			}
		})
	}
}
