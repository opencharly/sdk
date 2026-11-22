package sdk

import (
	"encoding/json"
	"strings"
	"testing"

	pb "github.com/opencharly/spec/proto"
	"github.com/opencharly/spec/spec"
)

// One dispatch serves every builder's three legs, and every failure is loud: an unknown op, an
// undecodable input, and a kit error (here: npm's unparseable manifest) all reach the caller.
func TestInvokeBuilderServesEveryLegLoudly(t *testing.T) {
	in, _ := json.Marshal(spec.BuilderCollectInput{Candy: "web", Manifest: "package.json", ManifestContent: `{"dependencies":{"x":"1"}}`})
	reply, err := InvokeBuilder("npm", &pb.InvokeRequest{Op: OpCollectContext, ParamsJson: in})
	if err != nil {
		t.Fatal(err)
	}
	var got spec.BuilderCollectReply
	if err := json.Unmarshal(reply.GetResultJson(), &got); err != nil || got.Context["packages"] == nil {
		t.Fatalf("collect reply = %s (%v), want the npm packages", reply.GetResultJson(), err)
	}
	bad, _ := json.Marshal(spec.BuilderCollectInput{Candy: "web", Manifest: "package.json", ManifestContent: "{"})
	if _, err := InvokeBuilder("npm", &pb.InvokeRequest{Op: OpCollectContext, ParamsJson: bad}); err == nil {
		t.Error("a kit collect error was swallowed")
	}
	if _, err := InvokeBuilder("npm", &pb.InvokeRequest{Op: OpResolve, ParamsJson: []byte("{")}); err == nil || !strings.Contains(err.Error(), "decode resolve input") {
		t.Errorf("an undecodable input was not refused: %v", err)
	}
	if _, err := InvokeBuilder("npm", &pb.InvokeRequest{Op: "bogus"}); err == nil {
		t.Error("an unknown op was not refused")
	}
}

// The collect leg declares the builder's package output (aur's per-candy pac packages), so the
// deploy-time compile knows it without rendering a stage; a payload builder declares none.
func TestInvokeBuilderCollectDeclaresThePackageOutput(t *testing.T) {
	collect := func(word string) spec.BuilderCollectReply {
		in, _ := json.Marshal(spec.BuilderCollectInput{Candy: "kde-shell", Builder: word, Packages: []string{"kdotool"}})
		reply, err := InvokeBuilder(word, &pb.InvokeRequest{Op: OpCollectContext, ParamsJson: in})
		if err != nil {
			t.Fatalf("%s collect: %v", word, err)
		}
		var got spec.BuilderCollectReply
		if err := json.Unmarshal(reply.GetResultJson(), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := collect("aur"); got.Package == nil || got.Package.Dir != "/tmp/aur-pkgs/kde-shell" || got.Package.Format != "pac" {
		t.Errorf("aur collect package = %+v, want pac packages in /tmp/aur-pkgs/kde-shell", got.Package)
	}
	if got := collect("npm"); got.Package != nil {
		t.Errorf("npm collect declares a package output %+v", got.Package)
	}
}
