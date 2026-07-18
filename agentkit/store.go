package agentkit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/fsnotify/fsnotify"
	"github.com/google/renameio/v2"
	"github.com/opencharly/sdk/spec"
)

// Store is a daemon-free durable agent state directory. Every record is a
// CUE-generated domain type; JSONL event logs are append-only and sequence
// checked under an advisory file lock so separate charly invocations compose.
type Store struct{ Dir string }

func OpenStore(dir string) (*Store, error) {
	if dir == "" {
		state := os.Getenv("XDG_STATE_HOME")
		if state == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			state = filepath.Join(home, ".local", "state")
		}
		dir = filepath.Join(state, "charly", "agents")
	}
	for _, sub := range []string{"sessions", "runs", "events", "terminal", "teams", "federation", "incidents", "rcas", "recoveries", "evidence"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return nil, err
		}
	}
	return &Store{Dir: dir}, nil
}

func (s *Store) PutTeam(v spec.AgentTeamRecord) error { return s.put("teams", string(v.ID), v) }
func (s *Store) Team(id spec.UUIDv7) (spec.AgentTeamRecord, error) {
	var v spec.AgentTeamRecord
	return v, s.get("teams", string(id), &v)
}
func (s *Store) Teams() ([]spec.AgentTeamRecord, error) {
	var out []spec.AgentTeamRecord
	err := s.list("teams", func(path string) error {
		var v spec.AgentTeamRecord
		if err := readJSON(path, &v); err != nil {
			return err
		}
		out = append(out, v)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out, err
}

func (s *Store) PutSession(v spec.AgentSession) error { return s.put("sessions", string(v.ID), v) }
func (s *Store) Session(id spec.UUIDv7) (spec.AgentSession, error) {
	var v spec.AgentSession
	return v, s.get("sessions", string(id), &v)
}
func (s *Store) Sessions() ([]spec.AgentSession, error) {
	var out []spec.AgentSession
	err := s.list("sessions", func(path string) error {
		var v spec.AgentSession
		if err := readJSON(path, &v); err != nil {
			return err
		}
		out = append(out, v)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out, err
}

func (s *Store) PutRun(v spec.AgentRunRequest) error { return s.put("runs", string(v.ID), v) }

// CreateRunOnce atomically reserves an idempotency key across concurrent
// controller processes. It returns the existing record without overwriting it.
func (s *Store) CreateRunOnce(v spec.AgentRunRequest) (spec.AgentRunRequest, bool, error) {
	var result spec.AgentRunRequest
	created := false
	err := s.withLock(func() error {
		entries, err := os.ReadDir(filepath.Join(s.Dir, "runs"))
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			var existing spec.AgentRunRequest
			if err := readJSON(filepath.Join(s.Dir, "runs", entry.Name()), &existing); err != nil {
				return err
			}
			if existing.IdempotencyKey == v.IdempotencyKey {
				result = existing
				return nil
			}
		}
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		path := filepath.Join(s.Dir, "runs", safeName(string(v.ID))+".json")
		if err := atomicWrite(path, append(b, '\n')); err != nil {
			return err
		}
		result, created = v, true
		return nil
	})
	return result, created, err
}
func (s *Store) Run(id spec.UUIDv7) (spec.AgentRunRequest, error) {
	var v spec.AgentRunRequest
	return v, s.get("runs", string(id), &v)
}
func (s *Store) Runs() ([]spec.AgentRunRequest, error) {
	var out []spec.AgentRunRequest
	err := s.list("runs", func(path string) error {
		var v spec.AgentRunRequest
		if err := readJSON(path, &v); err != nil {
			return err
		}
		out = append(out, v)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return string(out[i].ID) < string(out[j].ID) })
	return out, err
}
func (s *Store) FindIdempotency(key string) (spec.AgentRunRequest, bool, error) {
	var found spec.AgentRunRequest
	err := s.list("runs", func(path string) error {
		var v spec.AgentRunRequest
		if err := readJSON(path, &v); err != nil {
			return err
		}
		if v.IdempotencyKey == key {
			found = v
		}
		return nil
	})
	return found, found.ID != "", err
}

func (s *Store) AppendEvent(v spec.AgentEvent) error {
	if v.Sequence < 1 {
		return errors.New("agent store: event sequence must be >= 1")
	}
	return s.withLock(func() error {
		path := filepath.Join(s.Dir, "events", safeName(string(v.RunID))+".jsonl")
		events, err := readEvents(path)
		if err != nil {
			return err
		}
		want := int64(len(events) + 1)
		if v.Sequence != want {
			return fmt.Errorf("agent store: event sequence %d, want %d", v.Sequence, want)
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		if err := useFile(f, func() error {
			b, err := json.Marshal(v)
			if err != nil {
				return err
			}
			if _, err := f.Write(append(b, '\n')); err != nil {
				return err
			}
			return f.Sync()
		}); err != nil {
			return err
		}
		return syncDirectory(filepath.Dir(path))
	})
}

func (s *Store) Events(runID spec.UUIDv7) ([]spec.AgentEvent, error) {
	return readEvents(filepath.Join(s.Dir, "events", safeName(string(runID))+".jsonl"))
}

// AppendTerminalFrame assigns the next durable per-run sequence under the
// cross-process store lock and appends one generated terminal evidence frame.
func (s *Store) AppendTerminalFrame(v spec.TerminalFrame) (spec.TerminalFrame, error) {
	err := s.withLock(func() error {
		path := filepath.Join(s.Dir, "terminal", safeName(string(v.RunID))+".jsonl")
		frames, err := readTerminalFrames(path)
		if err != nil {
			return err
		}
		want := int64(1)
		if len(frames) > 0 {
			want = frames[len(frames)-1].Sequence + 1
		}
		if v.Sequence == 0 {
			v.Sequence = want
		} else if v.Sequence != want && (v.Kind != "resync" || v.Sequence <= want) {
			return fmt.Errorf("agent store: terminal sequence %d, want %d", v.Sequence, want)
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		if err := useFile(f, func() error {
			data, err := json.Marshal(v)
			if err != nil {
				return err
			}
			if _, err := f.Write(append(data, '\n')); err != nil {
				return err
			}
			return f.Sync()
		}); err != nil {
			return err
		}
		return syncDirectory(filepath.Dir(path))
	})
	return v, err
}

func (s *Store) TerminalFrames(runID spec.UUIDv7) ([]spec.TerminalFrame, error) {
	return readTerminalFrames(filepath.Join(s.Dir, "terminal", safeName(string(runID))+".jsonl"))
}

// RequestAbort records cross-process cancellation intent for the synchronous,
// ephemeral controller currently owning a run. No agent daemon is involved.
func (s *Store) RequestAbort(v spec.AgentAbortControl) error {
	return s.withLock(func() error {
		path := filepath.Join(s.Dir, "runs", safeName(string(v.RunID))+".abort")
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return atomicWrite(path, append(data, '\n'))
	})
}

func (s *Store) AbortRequested(runID spec.UUIDv7) (*spec.AgentAbortControl, error) {
	path := filepath.Join(s.Dir, "runs", safeName(string(runID))+".abort")
	var v spec.AgentAbortControl
	err := readJSON(path, &v)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return &v, err
}

// WaitAbort blocks on fsnotify's platform-native directory notification stream
// until the requested run's abort record exists. The pre/post-watch checks
// close the creation race without polling, sleeps, retries, or a permanent
// controller.
func (s *Store) WaitAbort(ctx context.Context, runID spec.UUIDv7) (*spec.AgentAbortControl, error) {
	if control, err := s.AbortRequested(runID); control != nil || err != nil {
		return control, err
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("agent store: watch abort controls: %w", err)
	}
	defer watcher.Close() //nolint:errcheck
	if err := watcher.Add(filepath.Join(s.Dir, "runs")); err != nil {
		return nil, fmt.Errorf("agent store: watch runs directory: %w", err)
	}
	if control, err := s.AbortRequested(runID); control != nil || err != nil {
		return control, err
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case _, ok := <-watcher.Events:
			if !ok {
				return nil, errors.New("agent store: abort notification stream closed")
			}
			if control, err := s.AbortRequested(runID); control != nil || err != nil {
				return control, err
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil, errors.New("agent store: abort error stream closed")
			}
			return nil, fmt.Errorf("agent store: abort notification: %w", err)
		}
	}
}

func (s *Store) ClearAbort(runID spec.UUIDv7) error {
	err := os.Remove(filepath.Join(s.Dir, "runs", safeName(string(runID))+".abort"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Store) PutIncident(v spec.Incident) error { return s.put("incidents", string(v.ID), v) }
func (s *Store) Incident(id spec.UUIDv7) (spec.Incident, error) {
	var v spec.Incident
	return v, s.get("incidents", string(id), &v)
}
func (s *Store) Incidents() ([]spec.Incident, error) {
	var out []spec.Incident
	err := s.list("incidents", func(path string) error {
		var v spec.Incident
		if err := readJSON(path, &v); err != nil {
			return err
		}
		out = append(out, v)
		return nil
	})
	return out, err
}
func (s *Store) PutRCA(v spec.RCARecord) error { return s.put("rcas", string(v.ID), v) }
func (s *Store) RCA(id spec.UUIDv7) (spec.RCARecord, error) {
	var v spec.RCARecord
	return v, s.get("rcas", string(id), &v)
}
func (s *Store) RCAs() ([]spec.RCARecord, error) {
	var out []spec.RCARecord
	err := s.list("rcas", func(path string) error {
		var v spec.RCARecord
		if err := readJSON(path, &v); err != nil {
			return err
		}
		out = append(out, v)
		return nil
	})
	return out, err
}
func (s *Store) PutRecovery(v spec.RecoveryDecision) error {
	return s.put("recoveries", string(v.ID), v)
}
func (s *Store) Recovery(id spec.UUIDv7) (spec.RecoveryDecision, error) {
	var v spec.RecoveryDecision
	return v, s.get("recoveries", string(id), &v)
}
func (s *Store) Recoveries() ([]spec.RecoveryDecision, error) {
	var out []spec.RecoveryDecision
	err := s.list("recoveries", func(path string) error {
		var v spec.RecoveryDecision
		if err := readJSON(path, &v); err != nil {
			return err
		}
		out = append(out, v)
		return nil
	})
	return out, err
}
func (s *Store) PutFederation(v spec.AgentFederationRecord) error {
	return s.put("federation", string(v.ID), v)
}
func (s *Store) Federation() ([]spec.AgentFederationRecord, error) {
	var out []spec.AgentFederationRecord
	err := s.list("federation", func(path string) error {
		var v spec.AgentFederationRecord
		if err := readJSON(path, &v); err != nil {
			return err
		}
		out = append(out, v)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt < out[j].UpdatedAt })
	return out, err
}

func (s *Store) put(sub, id string, value any) error {
	if id == "" {
		return errors.New("agent store: empty record id")
	}
	return s.withLock(func() error {
		b, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		path := filepath.Join(s.Dir, sub, safeName(id)+".json")
		return atomicWrite(path, append(b, '\n'))
	})
}

func atomicWrite(path string, content []byte) error {
	if err := renameio.WriteFile(path, content, 0o600, renameio.WithStaticPermissions(0o600)); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) (returnErr error) {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, dir.Close()) }()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync agent store directory %s: %w", path, err)
	}
	return nil
}

func (s *Store) get(sub, id string, dst any) error {
	return readJSON(filepath.Join(s.Dir, sub, safeName(id)+".json"), dst)
}

func (s *Store) list(sub string, fn func(string) error) error {
	entries, err := os.ReadDir(filepath.Join(s.Dir, sub))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if err := fn(filepath.Join(s.Dir, sub, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) withLock(fn func() error) (returnErr error) {
	f, err := os.OpenFile(filepath.Join(s.Dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, f.Close()) }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, syscall.Flock(int(f.Fd()), syscall.LOCK_UN)) }()
	return fn()
}

func useFile(file *os.File, operation func() error) (returnErr error) {
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	return operation()
}

func readJSON(path string, dst any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return fmt.Errorf("agent store %s: %w", path, err)
	}
	return nil
}

func readEvents(path string) (out []spec.AgentEvent, returnErr error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, f.Close()) }()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var event spec.AgentEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return out, nil
}

func readTerminalFrames(path string) (out []spec.TerminalFrame, returnErr error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, f.Close()) }()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var frame spec.TerminalFrame
		if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
			return nil, err
		}
		out = append(out, frame)
	}
	return out, scanner.Err()
}

func safeName(value string) string {
	for _, r := range value {
		if r != '-' && r != '_' && r != '.' && (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return "invalid"
		}
	}
	return value
}
