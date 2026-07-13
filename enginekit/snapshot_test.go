package enginekit

// snapshot_test.go — white-box coverage for the `<engine> ps` parse leg
// (parsePS + parseDockerPortString), re-homed from charly/status_test.go +
// charly/box_labels_cmd_test.go when the engine client moved into enginekit
// (P14a). Same package as the source so the unexported parse functions +
// enginePSRow are reachable.

import "testing"

// --- Engine ps parsing ---

func TestParsePS_Podman(t *testing.T) {
	in := `[{"Names":["charly-ollama"],"State":"running","Status":"Up 3 hours","Ports":[{"host_ip":"127.0.0.1","container_port":11434,"host_port":11434,"range":1,"protocol":"tcp"}]}]`
	rows, err := parsePS(in)
	if err != nil {
		t.Fatalf("parsePS: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].Name != "charly-ollama" || rows[0].State != "running" {
		t.Errorf("name/state = %q/%q", rows[0].Name, rows[0].State)
	}
	if len(rows[0].Ports) != 1 || rows[0].Ports[0].HostPort != 11434 || rows[0].Ports[0].CtrPort != 11434 {
		t.Errorf("ports = %+v", rows[0].Ports)
	}
}

func TestParsePS_Podman_PortRange(t *testing.T) {
	in := `[{"Names":["charly-x"],"State":"running","Status":"Up","Ports":[{"host_ip":"127.0.0.1","container_port":8000,"host_port":8000,"range":3,"protocol":"tcp"}]}]`
	rows, err := parsePS(in)
	if err != nil {
		t.Fatalf("parsePS: %v", err)
	}
	if len(rows[0].Ports) != 3 {
		t.Fatalf("expected range expansion to 3 mappings, got %d", len(rows[0].Ports))
	}
	if rows[0].Ports[2].HostPort != 8002 || rows[0].Ports[2].CtrPort != 8002 {
		t.Errorf("range mapping[2] = %+v, want host=8002 ctr=8002", rows[0].Ports[2])
	}
}

func TestParsePS_DockerNDJSON(t *testing.T) {
	in := `{"Names":"charly-ollama","State":"running","Status":"Up 3 hours","Ports":"127.0.0.1:11434->11434/tcp"}` + "\n" +
		`{"Names":"charly-jupyter","State":"running","Status":"Up 1 hour","Ports":"127.0.0.1:8888->8888/tcp, 0.0.0.0:5900->5900/tcp"}`
	rows, err := parsePS(in)
	if err != nil {
		t.Fatalf("parsePS: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%d, want 2", len(rows))
	}
	if rows[0].Ports[0].HostPort != 11434 || rows[0].Ports[0].HostIP != "127.0.0.1" {
		t.Errorf("row0 port[0] = %+v", rows[0].Ports[0])
	}
	if len(rows[1].Ports) != 2 || rows[1].Ports[1].HostPort != 5900 {
		t.Errorf("row1 ports = %+v", rows[1].Ports)
	}
}

func TestParseDockerPortString_IPv6(t *testing.T) {
	out := parseDockerPortString("[::]:8080->8080/tcp")
	if len(out) != 1 {
		t.Fatalf("got %d entries, want 1", len(out))
	}
	if out[0].HostIP != "::" || out[0].HostPort != 8080 || out[0].CtrPort != 8080 {
		t.Errorf("ipv6 mapping = %+v", out[0])
	}
}

func TestParseDockerPortString_Unpublished(t *testing.T) {
	out := parseDockerPortString("80/tcp")
	if len(out) != 0 {
		t.Errorf("unpublished port should be skipped, got %+v", out)
	}
}

func TestParsePS_PodmanRowsCarryImageRef(t *testing.T) {
	rows, err := parsePS(`[{"Names":["charly-probe"],"State":"running","Status":"Up 2 minutes","Image":"ghcr.io/opencharly/check-box-check:2026.160.0804","Ports":[]}]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Image != "ghcr.io/opencharly/check-box-check:2026.160.0804" {
		t.Errorf("podman ps Image not parsed: %+v", rows)
	}
}
