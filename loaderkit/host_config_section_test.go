package loaderkit

// host_config_section_test.go — the per-host `clean:` section reader (opencharly/sdk#565).
//
// The four arms are the reader's CONTRACT, not decoration: an authored ceiling comes back as a
// value; an authored document with no `clean:` is nil-and-no-error (unset is a legitimate answer);
// no document at all is nil-and-no-error (nothing authored, the consumer falls back); and an
// UNREADABLE document is an ERROR — because a config that cannot be read must never be mistaken
// for one that says nothing, or the consumer's "an invalid ceiling is refused" rule collapses into
// "silently use the default".
//
// The arm that makes this a fix rather than a test of a getter is the FIRST one: the value is read
// from the document, and it is exactly the value the delegating loader loses. The PR body runs the
// red arm — the reader's last step replaced by `deploykit.ProjectDeployConfig(uf).Clean` — and this
// file is what goes red.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/sdk"
	pb "github.com/opencharly/spec/proto"
	"github.com/opencharly/spec/spec"
	"google.golang.org/grpc"
)

// stubBootstrapExecutorClient is a minimal pb.ExecutorServiceClient double: it EMBEDS the generated
// client interface (nil) so only HostBuild needs a body, and answers ONLY the bootstrap leg — the
// one host call this narrow reader makes (it does not walk, materialize or project). The pattern is
// loaderkit/resolve_cluster_via_executor_test.go's.
type stubBootstrapExecutorClient struct {
	pb.ExecutorServiceClient
	// transform, when set, rewrites the root document bytes the way a bootstrap plugin would.
	transform func([]byte) []byte
}

func (s stubBootstrapExecutorClient) HostBuild(_ context.Context, in *pb.HostBuildRequest, _ ...grpc.CallOption) (*pb.HostBuildReply, error) {
	if in.GetKind() != "loader-bootstrap" {
		panic("stubBootstrapExecutorClient: unexpected host-build kind " + in.GetKind())
	}
	out := in.GetSpecJson()
	if s.transform != nil {
		out = s.transform(out)
	}
	return &pb.HostBuildReply{ResultJson: out}, nil
}

// cleanSectionBudgetDoc is the issue's own fixture: a `cache:` block beside the `clean:` one, with
// the 256 GiB ceiling the operator wants.
const cleanSectionBudgetDoc = `cache:
  git: {}
clean:
  store_budget:
    bytes: 274877906944
`

// writeHostDoc writes <dir>/charly.yml and isolates the SYSTEM config layer, so no host file can
// leak into the assertion (the config stack merges system → in-dir).
func writeHostDoc(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(SystemConfigEnv, filepath.Join(t.TempDir(), "absent-system.yml"))
	if err := os.WriteFile(filepath.Join(dir, spec.UnifiedFileName), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", spec.UnifiedFileName, err)
	}
	return dir
}

// TestLoadHostCleanSectionReadsTheAuthoredBudget is the arm the delegating loader cannot pass: the
// 256 GiB ceiling authored in the per-host document comes back through the reader.
func TestLoadHostCleanSectionReadsTheAuthoredBudget(t *testing.T) {
	dir := writeHostDoc(t, cleanSectionBudgetDoc)
	ex := sdk.NewInProcExecutor(stubBootstrapExecutorClient{})

	section, err := LoadHostCleanSectionViaExecutor(context.Background(), ex, dir)
	if err != nil {
		t.Fatalf("LoadHostCleanSectionViaExecutor: %v", err)
	}
	if section == nil || section.StoreBudget == nil {
		t.Fatalf("Clean = %+v, want the authored store budget (the section was dropped)", section)
	}
	if got := section.StoreBudget.Bytes; got != 274877906944 {
		t.Errorf("clean.store_budget.bytes = %d, want 274877906944", got)
	}
}

// TestLoadHostCleanSectionAbsentSectionIsNil — an authored per-host document with no `clean:` block
// answers (nil, nil): "unset" must reach the consumer as a fallback, never as a zero ceiling and
// never as an error.
func TestLoadHostCleanSectionAbsentSectionIsNil(t *testing.T) {
	dir := writeHostDoc(t, "cache:\n  git: {}\n")
	ex := sdk.NewInProcExecutor(stubBootstrapExecutorClient{})

	section, err := LoadHostCleanSectionViaExecutor(context.Background(), ex, dir)
	if err != nil {
		t.Fatalf("LoadHostCleanSectionViaExecutor: %v", err)
	}
	if section != nil {
		t.Errorf("Clean = %+v, want nil for a document that authors no clean section", section)
	}
}

// TestLoadHostCleanSectionAbsentDocumentIsNil — no per-host document at all: (nil, nil), the same
// answer a fresh machine gives.
func TestLoadHostCleanSectionAbsentDocumentIsNil(t *testing.T) {
	t.Setenv(SystemConfigEnv, filepath.Join(t.TempDir(), "absent-system.yml"))
	ex := sdk.NewInProcExecutor(stubBootstrapExecutorClient{})

	section, err := LoadHostCleanSectionViaExecutor(context.Background(), ex, t.TempDir())
	if err != nil {
		t.Fatalf("LoadHostCleanSectionViaExecutor: %v", err)
	}
	if section != nil {
		t.Errorf("Clean = %+v, want nil when no document exists", section)
	}
}

// TestLoadHostCleanSectionUnreadableIsAnError — a document that cannot be decoded must REFUSE, not
// report "no ceiling": the consumer can only fail loud if it is told the difference.
func TestLoadHostCleanSectionUnreadableIsAnError(t *testing.T) {
	dir := writeHostDoc(t, "clean:\n  store_budget: [unclosed\n")
	ex := sdk.NewInProcExecutor(stubBootstrapExecutorClient{})

	if _, err := LoadHostCleanSectionViaExecutor(context.Background(), ex, dir); err == nil {
		t.Fatal("an unreadable per-host document reported no error — \"absent\" and \"unparsed\" must differ")
	}
}

// TestLoadHostCleanSectionResolvesTheStandardPath — an empty dir names the standard per-host
// location, resolved through the ONE resolver (spec.DefaultDeployConfigPath, which honours
// CHARLY_DEPLOY_CONFIG) rather than composed here.
func TestLoadHostCleanSectionResolvesTheStandardPath(t *testing.T) {
	path := filepath.Join(writeHostDoc(t, cleanSectionBudgetDoc), spec.UnifiedFileName)
	t.Setenv(spec.DeployConfigEnv, path)
	ex := sdk.NewInProcExecutor(stubBootstrapExecutorClient{})

	section, err := LoadHostCleanSectionViaExecutor(context.Background(), ex, "")
	if err != nil {
		t.Fatalf("LoadHostCleanSectionViaExecutor(\"\"): %v", err)
	}
	if section == nil || section.StoreBudget == nil || section.StoreBudget.Bytes != 274877906944 {
		t.Fatalf("Clean = %+v, want the authored store budget from %s", section, path)
	}
}

// TestLoadHostCleanSectionNilExecutorIsLoud — no reverse channel is a hard error naming the
// placement, never a silent nil.
func TestLoadHostCleanSectionNilExecutorIsLoud(t *testing.T) {
	dir := writeHostDoc(t, cleanSectionBudgetDoc)
	if _, err := LoadHostCleanSectionViaExecutor(context.Background(), nil, dir); err == nil {
		t.Fatal("want an error for a nil executor, got nil")
	} else if !strings.Contains(err.Error(), "reverse channel") {
		t.Fatalf("the failure does not name the missing reverse channel: %v", err)
	}
}
