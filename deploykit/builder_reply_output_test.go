package deploykit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// builder_reply_output_test.go — the host's receipt check for a builder's OpResolve answer. DESIGN
// §4.4: no version negotiation; a plugin built against a different reply contract "fails at its
// first message, naming the field" (D-PROTO-6). Every case here fails on the pre-change code,
// which accepted any decodable reply — the shape opencharly/sdk#615 shipped an image from.

func TestValidateBuilderResolveReply(t *testing.T) {
	cases := []struct {
		name    string
		reply   spec.BuilderResolveReply
		wantErr string
	}{
		{
			name:    "an old plugin's reply carries no output",
			reply:   spec.BuilderResolveReply{Stage: "FROM x AS y", CopyArtifacts: []string{"COPY --from=y /o /o"}},
			wantErr: "no `output` discriminator",
		},
		{
			name:    "a package output carries no package",
			reply:   spec.BuilderResolveReply{Output: "package", Stage: "FROM x AS y"},
			wantErr: "carries no `package`",
		},
		{
			name:    "a files output carries no copies",
			reply:   spec.BuilderResolveReply{Output: "files", Stage: "FROM x AS y"},
			wantErr: "carries no `copy_artifacts`",
		},
		{
			name:    "an inline output carries no fragment",
			reply:   spec.BuilderResolveReply{Output: "inline"},
			wantErr: "carries no `inline_fragment`",
		},
		{
			name:    "an unknown output",
			reply:   spec.BuilderResolveReply{Output: "binary"},
			wantErr: "unknown `output`",
		},
	}
	for _, c := range cases {
		err := ValidateBuilderResolveReply("aur", c.reply)
		if err == nil {
			t.Errorf("%s: accepted, want an error", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%s: error = %v, want it to name %q", c.name, err, c.wantErr)
		}
		if !strings.Contains(err.Error(), `"aur"`) {
			t.Errorf("%s: error does not name the builder: %v", c.name, err)
		}
	}

	ok := []spec.BuilderResolveReply{
		{Output: "package", Stage: "s", Package: &spec.BuilderPackage{Format: "pac", Dir: "/tmp/aur-pkgs/c", Glob: "*.pkg.tar.zst"}},
		{Output: "files", Stage: "s", CopyArtifacts: []string{"COPY --from=s /usr/lib/node_modules /usr/lib/node_modules"}},
		{Output: "inline", InlineFragment: "RUN cargo build"},
	}
	for _, r := range ok {
		if err := ValidateBuilderResolveReply("npm", r); err != nil {
			t.Errorf("output %q refused: %v", r.Output, err)
		}
	}
}
