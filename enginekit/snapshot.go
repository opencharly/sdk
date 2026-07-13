package enginekit

// snapshot.go — the `<engine> ps` leg: list charly-* containers and parse both
// podman's JSON-array shape and docker's stringly-typed / NDJSON shape into
// enginePSRow + structured spec.PortMapping port lists.

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/opencharly/sdk/spec"
)

type enginePSRow struct {
	Name   string
	State  string
	Status string
	Image  string // full image ref:tag as reported by ps
	Ports  []spec.PortMapping
}

func (e *EngineClient) runPS(includeAll bool) ([]enginePSRow, error) {
	args := []string{"ps", "--filter", "name=charly-", "--format", "json", "--no-trunc"}
	if includeAll {
		args = append(args, "-a")
	}
	out, err := exec.Command(e.bin, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}
	return parsePS(string(out))
}

// podmanPSEntry matches Podman's structured JSON shape.
type podmanPSEntry struct {
	Names  []string     `json:"Names"`
	Status string       `json:"Status"`
	State  string       `json:"State"`
	Image  string       `json:"Image"`
	Ports  []podmanPort `json:"Ports"`
}

type podmanPort struct {
	HostIP        string `json:"host_ip"`
	ContainerPort int    `json:"container_port"`
	HostPort      int    `json:"host_port"`
	Range         int    `json:"range"`
	Protocol      string `json:"protocol"`
}

// dockerPSEntry matches Docker's stringly-typed JSON shape.
type dockerPSEntry struct {
	Names  string `json:"Names"`
	Status string `json:"Status"`
	State  string `json:"State"`
	Image  string `json:"Image"`
	Ports  string `json:"Ports"`
}

// parsePS handles podman's JSON-array shape and docker's NDJSON shape.
func parsePS(data string) ([]enginePSRow, error) {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" {
		return nil, nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var pe []podmanPSEntry
		if err := json.Unmarshal([]byte(trimmed), &pe); err == nil {
			out := make([]enginePSRow, 0, len(pe))
			for _, e := range pe {
				out = append(out, enginePSRow{
					Name:   firstName(e.Names),
					State:  e.State,
					Status: e.Status,
					Image:  e.Image,
					Ports:  fromPodmanPorts(e.Ports),
				})
			}
			return out, nil
		}
		var de []dockerPSEntry
		if err := json.Unmarshal([]byte(trimmed), &de); err != nil {
			return nil, fmt.Errorf("parsing ps JSON: %w", err)
		}
		return fromDockerPSRows(de), nil
	}
	// docker ps emits NDJSON by default
	var de []dockerPSEntry
	for line := range strings.SplitSeq(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry dockerPSEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("parsing ps JSON line: %w", err)
		}
		de = append(de, entry)
	}
	return fromDockerPSRows(de), nil
}

func firstName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	// Podman may return comma-joined names — first wins.
	return strings.Split(names[0], ",")[0]
}

func fromPodmanPorts(in []podmanPort) []spec.PortMapping {
	if len(in) == 0 {
		return nil
	}
	out := make([]spec.PortMapping, 0, len(in))
	for _, p := range in {
		span := max(p.Range, 1)
		for i := range span {
			out = append(out, spec.PortMapping{
				HostIP:   p.HostIP,
				HostPort: p.HostPort + i,
				CtrPort:  p.ContainerPort + i,
				Proto:    p.Protocol,
			})
		}
	}
	return out
}

func fromDockerPSRows(rows []dockerPSEntry) []enginePSRow {
	out := make([]enginePSRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, enginePSRow{
			Name:   strings.Split(r.Names, ",")[0],
			State:  r.State,
			Status: r.Status,
			Image:  r.Image,
			Ports:  parseDockerPortString(r.Ports),
		})
	}
	return out
}

// parseDockerPortString parses docker ps's flattened port string. Docker emits
// entries separated by ", "; each entry is one of:
//
//	"<bind>:<host>-><container>/<proto>"   (published, IPv4 or [IPv6])
//	"<host>-><container>/<proto>"          (published, no bind)
//	"<container>/<proto>"                  (unpublished — skipped)
func parseDockerPortString(s string) []spec.PortMapping {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []spec.PortMapping
	for item := range strings.SplitSeq(s, ",") {
		item = strings.TrimSpace(item)
		before, after, ok := strings.Cut(item, "->")
		if !ok {
			continue // unpublished
		}
		left, right := before, after
		var bindIP string
		var hostPortStr string
		// Handle "[v6]:port" and "v4:port" and bare "port"
		if strings.HasPrefix(left, "[") {
			if i := strings.Index(left, "]:"); i > 0 {
				bindIP = left[1:i]
				hostPortStr = left[i+2:]
			}
		} else if i := strings.LastIndex(left, ":"); i >= 0 {
			bindIP = left[:i]
			hostPortStr = left[i+1:]
		} else {
			hostPortStr = left
		}
		hostPort, err := strconv.Atoi(strings.TrimSpace(hostPortStr))
		if err != nil || hostPort <= 0 {
			continue
		}
		proto := "tcp"
		ctrStr := right
		if before, after, ok := strings.Cut(right, "/"); ok {
			proto = strings.TrimSpace(after)
			ctrStr = before
		}
		ctrPort, err := strconv.Atoi(strings.TrimSpace(ctrStr))
		if err != nil || ctrPort <= 0 {
			continue
		}
		out = append(out, spec.PortMapping{
			HostIP:   bindIP,
			HostPort: hostPort,
			CtrPort:  ctrPort,
			Proto:    proto,
		})
	}
	return out
}
