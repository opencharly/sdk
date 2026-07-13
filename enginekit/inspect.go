package enginekit

// inspect.go — the `<engine> inspect` leg: pull NetworkMode / Devices / Mounts
// (and CDI/GPU markers) out of the raw inspect blobs both engines emit, tolerant
// of the docker/podman casing differences via a generic-map scan.

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type engineInspectRow struct {
	Name        string
	NetworkMode string
	Devices     []string
	Mounts      []MountInfo
}

func (e *EngineClient) runInspect(names []string) ([]engineInspectRow, error) {
	if len(names) == 0 {
		return nil, nil
	}
	args := append([]string{"inspect"}, names...)
	out, err := exec.Command(e.bin, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("inspecting containers: %w", err)
	}
	return parseInspect(out)
}

// parseInspect decodes the array of inspect blobs both engines emit by default.
// We pull only the fields we need and tolerate the docker/podman casing
// differences by indexing into a generic map.
func parseInspect(data []byte) ([]engineInspectRow, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "[]" {
		return nil, nil
	}
	var raws []map[string]any
	if err := json.Unmarshal([]byte(trimmed), &raws); err != nil {
		return nil, fmt.Errorf("parsing inspect JSON: %w", err)
	}
	out := make([]engineInspectRow, 0, len(raws))
	for _, r := range raws {
		row := engineInspectRow{Name: stringAt(r, "Name")}
		hc, _ := r["HostConfig"].(map[string]any)
		if hc != nil {
			row.NetworkMode = stringAt(hc, "NetworkMode")
			row.Devices = devicesFromHostConfig(hc)
		}
		row.Mounts = mountsFromInspect(r)
		// CDI / GPU detection — both engines surface this slightly
		// differently; the union covers podman (CDI in HostConfig.Devices /
		// Annotations / nvidia.com/gpu request) and docker (--gpus →
		// HostConfig.DeviceRequests). One scan over the raw map catches all.
		if hasGPU(r) {
			row.Devices = append([]string{"nvidia.com/gpu=all"}, row.Devices...)
		}
		out = append(out, row)
	}
	return out, nil
}

// mountsFromInspect pulls the .Mounts[] array out of a raw inspect blob.
// Both podman and docker emit the same shape: an array of objects with
// Type / Source / Destination / Name keys (Name is empty for type=bind).
// This is the LIVE mount data — what the container is actually bound to,
// independent of the OCI label default volume layout.
func mountsFromInspect(raw map[string]any) []MountInfo {
	mountsAny, ok := raw["Mounts"].([]any)
	if !ok {
		return nil
	}
	out := make([]MountInfo, 0, len(mountsAny))
	for _, m := range mountsAny {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, MountInfo{
			Type:        stringAt(mm, "Type"),
			Source:      stringAt(mm, "Source"),
			Destination: stringAt(mm, "Destination"),
			Name:        stringAt(mm, "Name"),
		})
	}
	return out
}

func stringAt(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// devicesFromHostConfig pulls Devices[].PathOnHost out of a HostConfig blob.
func devicesFromHostConfig(hc map[string]any) []string {
	devs, ok := hc["Devices"].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, d := range devs {
		dm, ok := d.(map[string]any)
		if !ok {
			continue
		}
		if p := stringAt(dm, "PathOnHost"); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// hasGPU returns true when any nvidia.com/gpu / Gpus / DeviceRequests marker
// is present in the inspect blob. Cheap string scan over the JSON is enough —
// false positives are harmless (they just add a "gpu" device token).
func hasGPU(raw map[string]any) bool {
	b, _ := json.Marshal(raw)
	s := string(b)
	return strings.Contains(s, "nvidia.com/gpu") ||
		strings.Contains(s, `"Gpus"`) ||
		strings.Contains(s, `"DeviceRequests"`) && strings.Contains(s, "nvidia")
}
