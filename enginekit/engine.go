// Package enginekit is the container-engine client mechanism: the single place
// in the status surface that shells out to podman/docker (ps + inspect + exec)
// and returns structured, batch-derived ContainerSnapshots. All other status
// code consumes ContainerSnapshot and never touches the engine directly.
package enginekit

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
)

// ContainerSnapshot is the cheap, batch-derived view of one charly-* container.
// One snapshot is built from a single `podman ps --format json` row plus the
// matching `podman inspect` blob; both engine calls are batched at the
// EngineClient level (one ps + one inspect per status invocation, not per
// container).
type ContainerSnapshot struct {
	Name        string             // "charly-selkies-desktop-185.52.136.164"
	State       string             // "running" | "exited" | "created" | "paused" | "dead" | "removing"
	Status      string             // human "Up 3 hours"
	Box         string             // base box short-name (filled by Collector after parsing the quadlet description)
	Instance    string             // optional instance suffix, ditto
	ImageRef    string             // full image ref:tag from ps — the deployed artifact identity
	NetworkMode string             // "host" | "bridge" | "container:<id>" | named network
	Ports       []spec.PortMapping // runtime mappings from `podman ps`
	Devices     []string           // /dev/dri/..., nvidia.com/gpu=all, ...
	Mounts      []MountInfo        // live mounts from podman inspect .Mounts (RUNTIME truth — what the container is ACTUALLY mounting, not the OCI label default)
}

// MountInfo represents one live container mount point as reported by
// `podman inspect .Mounts[]`. Source is the host-side path (or volume
// name for type=volume); Destination is the container-side path. Type
// is the engine's mount kind ("bind" / "volume" / "tmpfs"). Used by
// `charly status` to distinguish a `--bind` / `--encrypt` deploy override
// from the image-label default volume backing.
type MountInfo struct {
	Type        string // "bind" | "volume" | "tmpfs"
	Source      string // host path (bind) or volume name (volume)
	Destination string // container path
	Name        string // for type=volume: the named volume; otherwise empty
}

// HostPortFor returns the host IP + host port that maps to the given
// container-side port/proto. Host-networked containers always return
// ("127.0.0.1", ctrPort, true) — there is no NAT mapping but the port is
// reachable on localhost. Returns ("", 0, false) when the port is not
// published.
func (s *ContainerSnapshot) HostPortFor(ctrPort int, proto string) (string, int, bool) {
	if s == nil {
		return "", 0, false
	}
	if s.NetworkMode == "host" {
		return "127.0.0.1", ctrPort, true
	}
	for _, p := range s.Ports {
		if p.CtrPort == ctrPort && (proto == "" || p.Proto == proto || p.Proto == "") {
			ip := p.HostIP
			if ip == "" || ip == "0.0.0.0" || ip == "::" {
				ip = "127.0.0.1"
			}
			return ip, p.HostPort, true
		}
	}
	return "", 0, false
}

// EngineClient is the only place in the status surface that touches
// podman/docker. All other code consumes ContainerSnapshot.
type EngineClient struct {
	bin string // "podman" or "docker"
}

// NewEngineClient builds a client for the given engine name ("podman" or
// "docker", or "auto" which is resolved via kit.EngineBinary).
func NewEngineClient(engine string) *EngineClient {
	return &EngineClient{bin: kit.EngineBinary(engine)}
}

// Bin returns the resolved engine binary name.
func (e *EngineClient) Bin() string { return e.bin }

// SnapshotAll lists charly-* containers (one ps call), then runs one batched
// inspect for the whole set. Returns one ContainerSnapshot per container.
func (e *EngineClient) SnapshotAll(includeAll bool) ([]ContainerSnapshot, error) {
	rows, err := e.runPS(includeAll)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.Name)
	}
	inspects, err := e.runInspect(names)
	if err != nil {
		// Inspect failures shouldn't blank out the whole snapshot — fall
		// back to ps-only data with empty NetworkMode/Devices.
		inspects = nil
	}
	idx := map[string]engineInspectRow{}
	for _, ir := range inspects {
		idx[strings.TrimPrefix(ir.Name, "/")] = ir
	}
	out := make([]ContainerSnapshot, 0, len(rows))
	for _, r := range rows {
		snap := ContainerSnapshot{
			Name:     r.Name,
			State:    r.State,
			Status:   r.Status,
			ImageRef: r.Image,
			Ports:    r.Ports,
		}
		if ir, ok := idx[r.Name]; ok {
			snap.NetworkMode = ir.NetworkMode
			snap.Devices = ir.Devices
			snap.Mounts = ir.Mounts
		}
		out = append(out, snap)
	}
	return out, nil
}

// ExecBatched runs `<engine> exec <container> sh -c '<script>'` and returns
// combined stdout. Used by the GuestProbe batcher to run all probes for one
// container in a single exec session.
func (e *EngineClient) ExecBatched(ctx context.Context, container, script string) (string, error) {
	cmd := exec.CommandContext(ctx, e.bin, "exec", container, "sh", "-c", script)
	out, err := cmd.Output()
	return string(out), err
}

// Snapshot returns a ContainerSnapshot for one named container. Used by
// interactive single-container commands (WlStatusCmd and the like) that need
// probe data without enumerating every charly container.
// Two engine calls (one filtered ps + one inspect of just this name).
func (e *EngineClient) Snapshot(name string) (*ContainerSnapshot, error) {
	out, err := exec.Command(e.bin, "ps", "-a", "--filter", "name="+name, "--format", "json", "--no-trunc").Output()
	if err != nil {
		return nil, fmt.Errorf("ps %s: %w", name, err)
	}
	rows, err := parsePS(string(out))
	if err != nil {
		return nil, err
	}
	var row enginePSRow
	for _, r := range rows {
		if r.Name == name {
			row = r
			break
		}
	}
	if row.Name == "" {
		return nil, fmt.Errorf("container %s not found", name)
	}
	snap := &ContainerSnapshot{
		Name:     row.Name,
		State:    row.State,
		Status:   row.Status,
		ImageRef: row.Image,
		Ports:    row.Ports,
	}
	if inspects, err := e.runInspect([]string{name}); err == nil && len(inspects) > 0 {
		snap.NetworkMode = inspects[0].NetworkMode
		snap.Devices = inspects[0].Devices
		snap.Mounts = inspects[0].Mounts
	}
	return snap, nil
}
