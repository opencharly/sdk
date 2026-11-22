package kit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// builder_output_test.go — every BUILT-IN builder reply names its output. The host refuses a reply
// without the discriminator (deploykit.ValidateBuilderResolveReply), so a built-in that stopped
// stamping it would be refused at runtime with nothing in the unit suite to say why
// (opencharly/sdk#615). Each case here fails if the corresponding `Output:` stamp is removed.
func TestBuilderResolveStampsTheOutputPerBuilder(t *testing.T) {
	want := map[string]string{
		"pixi":  BuilderOutputFiles,
		"npm":   BuilderOutputFiles,
		"mise":  BuilderOutputFiles,
		"aur":   BuilderOutputPackage,
		"cargo": BuilderOutputInline,
	}
	for word, output := range want {
		in := spec.BuilderResolveInput{
			Candy:     "c",
			Builder:   word,
			StageName: "c-" + word + "-build",
		}
		reply, err := BuilderResolve(word, in)
		if err != nil {
			t.Errorf("%s: BuilderResolve: %v", word, err)
			continue
		}
		if reply.Output != output {
			t.Errorf("%s: Output = %q, want %q", word, reply.Output, output)
		}
	}
}
