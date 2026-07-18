package agentkit

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/opencharly/sdk/spec"
)

func TestStoreWaitAbortUsesCrossProcessNotification(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	control := spec.AgentAbortControl{RunID: NewID(), RequestID: NewID(), RequestedAt: "now"}
	result := make(chan *spec.AgentAbortControl, 1)
	errs := make(chan error, 1)
	go func() {
		got, err := store.WaitAbort(ctx, control.RunID)
		result <- got
		errs <- err
	}()
	if err := store.RequestAbort(control); err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if got := <-result; got == nil || got.RequestID != control.RequestID {
		t.Fatalf("abort notification = %#v", got)
	}
}

func TestStorePersistsOrderedTerminalEvidence(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runID := NewID()
	first, err := store.AppendTerminalFrame(spec.TerminalFrame{RunID: runID, Kind: "raw", Stream: "terminal", Data: []byte{0, 1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	code := 7
	second, err := store.AppendTerminalFrame(spec.TerminalFrame{RunID: runID, Kind: "exit", ExitCode: &code})
	if err != nil {
		t.Fatal(err)
	}
	if first.Sequence != 1 || second.Sequence != 2 {
		t.Fatalf("sequences = %d, %d", first.Sequence, second.Sequence)
	}
	frames, err := store.TerminalFrames(runID)
	if err != nil || len(frames) != 2 {
		t.Fatalf("frames = %#v, %v", frames, err)
	}
	if !bytes.Equal(frames[0].Data, []byte{0, 1, 2}) || frames[1].ExitCode == nil || *frames[1].ExitCode != 7 {
		t.Fatalf("terminal evidence did not round trip: %#v", frames)
	}
}

func TestStoreRejectsTerminalSequenceGapBeforeAppend(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runID := NewID()
	if _, err := store.AppendTerminalFrame(spec.TerminalFrame{RunID: runID, Sequence: 2, Kind: "status"}); err == nil {
		t.Fatal("terminal evidence sequence gap was appended")
	}
	frames, err := store.TerminalFrames(runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 0 {
		t.Fatalf("sequence-gap append mutated transcript: %#v", frames)
	}
}

func TestStoreAcceptsExplicitTerminalResynchronizationGap(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runID := NewID()
	if _, err := store.AppendTerminalFrame(spec.TerminalFrame{RunID: runID, Sequence: 3, Kind: "resync", Snapshot: "recovered"}); err != nil {
		t.Fatal(err)
	}
	next, err := store.AppendTerminalFrame(spec.TerminalFrame{RunID: runID, Kind: "status"})
	if err != nil {
		t.Fatal(err)
	}
	if next.Sequence != 4 {
		t.Fatalf("sequence after resync = %d, want 4", next.Sequence)
	}
}

func TestStorePersistsIdempotentRunsAndOrderedEvents(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runID, sessionID, requestID := NewID(), NewID(), NewID()
	run := spec.AgentRunRequest{ID: runID, SessionID: sessionID, RequestID: requestID, IdempotencyKey: "same-request"}
	if err := store.PutRun(run); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.FindIdempotency("same-request")
	if err != nil || !ok || got.ID != runID {
		t.Fatalf("find = %#v, %v, %v", got, ok, err)
	}
	if err := store.AppendEvent(spec.AgentEvent{RunID: runID, Sequence: 1, Type: "started", Time: "now"}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendEvent(spec.AgentEvent{RunID: runID, Sequence: 3, Type: "failed", Time: "now"}); err == nil {
		t.Fatal("sequence gap accepted")
	}
	if err := store.AppendEvent(spec.AgentEvent{RunID: runID, Sequence: 2, Type: "completed", Time: "now"}); err != nil {
		t.Fatal(err)
	}
	events, err := store.Events(runID)
	if err != nil || len(events) != 2 {
		t.Fatalf("events = %#v, %v", events, err)
	}
}

func TestStoreCreateRunOnceIsAtomicAcrossControllers(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const controllers = 16
	results := make(chan spec.AgentRunRequest, controllers)
	var wg sync.WaitGroup
	for range controllers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run := spec.AgentRunRequest{ID: NewID(), SessionID: NewID(), RequestID: NewID(), IdempotencyKey: "one"}
			got, _, err := store.CreateRunOnce(run)
			if err != nil {
				t.Errorf("CreateRunOnce: %v", err)
				return
			}
			results <- got
		}()
	}
	wg.Wait()
	close(results)
	var id spec.UUIDv7
	for result := range results {
		if id == "" {
			id = result.ID
		}
		if result.ID != id {
			t.Errorf("different run IDs: %s and %s", id, result.ID)
		}
	}
}

func TestStorePersistsTeamAuthorityAndCrossProcessAbort(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	team := spec.AgentTeamRecord{
		ID: NewID(),
		Team: spec.AgentTeam{
			Agents: []spec.AgentTeamMember{{Name: "lead", Runtime: "pi"}, {Name: "reviewer", Runtime: "tmux"}},
			Edges:  []spec.AgentDelegationEdge{{From: "lead", To: "reviewer", Allow: []string{"run"}}},
		},
		Sessions:  map[string]spec.UUIDv7{"lead": NewID(), "reviewer": NewID()},
		CreatedAt: "now",
	}
	if err := store.PutTeam(team); err != nil {
		t.Fatal(err)
	}
	got, err := store.Team(team.ID)
	if err != nil || got.Sessions["reviewer"] != team.Sessions["reviewer"] {
		t.Fatalf("team = %#v, %v", got, err)
	}

	control := spec.AgentAbortControl{RunID: NewID(), RequestID: NewID(), RequestedAt: "now"}
	if err := store.RequestAbort(control); err != nil {
		t.Fatal(err)
	}
	pending, err := store.AbortRequested(control.RunID)
	if err != nil || pending == nil || pending.RequestID != control.RequestID {
		t.Fatalf("abort = %#v, %v", pending, err)
	}
	if err := store.ClearAbort(control.RunID); err != nil {
		t.Fatal(err)
	}
	pending, err = store.AbortRequested(control.RunID)
	if err != nil || pending != nil {
		t.Fatalf("cleared abort = %#v, %v", pending, err)
	}
}
