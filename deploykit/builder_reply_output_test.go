package deploykit

import (
	"strings"
	"testing"

	pb "github.com/opencharly/spec/proto"
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

// The SEAM is where a plugin's answer actually arrives: `resolveBuilderStage` decodes it and must
// refuse an old-style reply there. Deleting the validation call makes this test fail — which is
// the point (R10: the changed path must execute).
func TestResolveBuilderStageRefusesAnOldStyleReply(t *testing.T) {
	ctx, ex := testExecutorInvoke(func(in *pb.InvokeProviderRequest) (*pb.InvokeReply, error) {
		if in.GetClass() != "builder" || in.GetReserved() != "aur" {
			t.Fatalf("unexpected dispatch: class=%q word=%q", in.GetClass(), in.GetReserved())
		}
		// exactly what plugin-builder-aur:v2026.271.2317 answered: a stage and payload copies,
		// no output discriminator, no package.
		return &pb.InvokeReply{ResultJson: []byte(`{"stage":"FROM arch-builder AS c-aur-build","copy_artifacts":["COPY --from=c-aur-build /tmp/aur-pkgs/ /tmp/aur-pkgs/"]}`)}, nil
	})
	c := renderSeamCaller{ctx: ctx, ex: ex}
	img := &spec.ResolvedBox{Name: "b"}
	_, err := c.resolveBuilderStage("aur", spec.BuilderResolveInput{Candy: "c"}, img)
	if err == nil {
		t.Fatal("the seam accepted a reply with no `output`: an old plugin would ship without its package again")
	}
	if !strings.Contains(err.Error(), "no `output` discriminator") {
		t.Fatalf("error = %v, want it to name the missing discriminator", err)
	}
}

// And the same seam accepts a modern reply, so the check is not simply refusing everything.
func TestResolveBuilderStageAcceptsAModernReply(t *testing.T) {
	ctx, ex := testExecutorInvoke(func(in *pb.InvokeProviderRequest) (*pb.InvokeReply, error) {
		return &pb.InvokeReply{ResultJson: []byte(`{"output":"files","stage":"FROM node:20 AS c-npm-build","copy_artifacts":["COPY --from=c-npm-build /usr/local/lib/node_modules /usr/local/lib/node_modules"]}`)}, nil
	})
	c := renderSeamCaller{ctx: ctx, ex: ex}
	reply, err := c.resolveBuilderStage("npm", spec.BuilderResolveInput{Candy: "c"}, &spec.ResolvedBox{Name: "b"})
	if err != nil {
		t.Fatalf("the seam refused a well-formed reply: %v", err)
	}
	if reply.Output != "files" {
		t.Errorf("Output = %q, want files", reply.Output)
	}
}
