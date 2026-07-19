package agentkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/sdk/spec"
)

// Variable-width fractional seconds defeat lexicographic order: ".9Z" and
// ".900000001Z" share the prefix ".9", so as TEXT the longer one sorts first
// ('0' < 'Z'), while TEMPORALLY .9 (900ms) is the earlier time. The list
// views must order by parsed time.Time, and an unparseable timestamp must
// surface an error instead of a silent misorder.
func TestStoreListsSortTimestampsTemporally(t *testing.T) {
	const (
		earlier = "2026-07-19T10:00:00.9Z"
		later   = "2026-07-19T10:00:00.900000001Z"
	)
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mkTeam := func(createdAt string) spec.AgentTeamRecord {
		return spec.AgentTeamRecord{
			ID: NewID(),
			Team: spec.AgentTeam{
				Agents: []spec.AgentTeamMember{{Name: "lead", Runtime: "pi"}, {Name: "reviewer", Runtime: "tmux"}},
				Edges:  []spec.AgentDelegationEdge{{From: "lead", To: "reviewer", Allow: []string{"run"}}},
			},
			Sessions:  map[string]spec.UUIDv7{"lead": NewID()},
			CreatedAt: createdAt,
		}
	}
	lateTeam, earlyTeam := mkTeam(later), mkTeam(earlier)
	for _, team := range []spec.AgentTeamRecord{lateTeam, earlyTeam} {
		if err := store.PutTeam(team); err != nil {
			t.Fatal(err)
		}
	}
	teams, err := store.Teams()
	if err != nil {
		t.Fatal(err)
	}
	if len(teams) != 2 || teams[0].ID != earlyTeam.ID || teams[1].ID != lateTeam.ID {
		t.Fatalf("teams order = %#v, want earlier (.9Z) before later (.900000001Z)", teams)
	}

	mkSession := func(createdAt string) spec.AgentSession {
		return spec.AgentSession{ID: NewID(), Runtime: "pi", State: "active", CreatedAt: createdAt, UpdatedAt: createdAt}
	}
	lateSession, earlySession := mkSession(later), mkSession(earlier)
	for _, session := range []spec.AgentSession{lateSession, earlySession} {
		if err := store.PutSession(session); err != nil {
			t.Fatal(err)
		}
	}
	sessions, err := store.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 || sessions[0].ID != earlySession.ID || sessions[1].ID != lateSession.ID {
		t.Fatalf("sessions order = %#v, want earlier (.9Z) before later (.900000001Z)", sessions)
	}

	mkNode := func(updatedAt string) spec.AgentFederationRecord {
		return spec.AgentFederationRecord{ID: NewID(), Node: "box", Owner: "operator", State: "active", UpdatedAt: updatedAt}
	}
	lateNode, earlyNode := mkNode(later), mkNode(earlier)
	for _, node := range []spec.AgentFederationRecord{lateNode, earlyNode} {
		if err := store.PutFederation(node); err != nil {
			t.Fatal(err)
		}
	}
	nodes, err := store.Federation()
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || nodes[0].ID != earlyNode.ID || nodes[1].ID != lateNode.ID {
		t.Fatalf("federation order = %#v, want earlier (.9Z) before later (.900000001Z)", nodes)
	}

	if err := store.PutTeam(mkTeam("not-a-timestamp")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Teams(); err == nil || !strings.Contains(err.Error(), "parse record timestamp") {
		t.Fatalf("unparseable timestamp must surface an error, got %v", err)
	}
}

// AbortRequested must never return a partially decoded record alongside its
// decode error: callers probe `control != nil || err != nil`, so a non-nil
// control with a non-nil error would masquerade as a valid abort request.
func TestStoreAbortRequestedNeverReturnsPartialRecordWithError(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runID := NewID()
	path := filepath.Join(store.Dir, "runs", string(runID)+".abort")
	if err := os.WriteFile(path, []byte("{corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	control, err := store.AbortRequested(runID)
	if err == nil {
		t.Fatal("corrupt abort record decoded without error")
	}
	if control != nil {
		t.Fatalf("decode error returned a partial record: %#v", control)
	}
}
