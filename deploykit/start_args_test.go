package deploykit

import (
	"reflect"
	"slices"
	"testing"

	"github.com/opencharly/sdk/spec"
)

func TestBuildStartArgs(t *testing.T) {
	args := BuildStartArgs("docker", "ghcr.io/opencharly/fedora-test:latest", 1000, 1000, nil, "charly-fedora-test", nil, nil, false, "127.0.0.1", nil, spec.SecurityConfig{}, []string{"supervisord", "-n", "-c", "/etc/supervisord.conf"}, "/workspace")
	want := []string{
		"docker", "run", "-d", "--rm",
		"--name", "charly-fedora-test",
		"-w", "/workspace",
		"ghcr.io/opencharly/fedora-test:latest",
		"supervisord", "-n", "-c", "/etc/supervisord.conf",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("BuildStartArgs() =\n  %v\nwant\n  %v", args, want)
	}
}

func TestBuildStartArgsPodman(t *testing.T) {
	args := BuildStartArgs("podman", "ghcr.io/opencharly/fedora-test:latest", 1000, 1000, nil, "charly-fedora-test", nil, nil, false, "127.0.0.1", nil, spec.SecurityConfig{}, []string{"supervisord", "-n", "-c", "/etc/supervisord.conf"}, "/workspace")
	want := []string{
		"podman", "run", "-d", "--rm",
		"--name", "charly-fedora-test",
		"-w", "/workspace",
		"ghcr.io/opencharly/fedora-test:latest",
		"supervisord", "-n", "-c", "/etc/supervisord.conf",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("BuildStartArgs(podman) =\n  %v\nwant\n  %v", args, want)
	}
}

func TestBuildStartArgsWithPorts(t *testing.T) {
	args := BuildStartArgs("docker", "ghcr.io/opencharly/fedora-test:latest", 1000, 1000, []string{"9090:9090", "8080:8080"}, "charly-fedora-test", nil, nil, false, "127.0.0.1", nil, spec.SecurityConfig{}, []string{"supervisord", "-n", "-c", "/etc/supervisord.conf"}, "/workspace")
	want := []string{
		"docker", "run", "-d", "--rm",
		"--name", "charly-fedora-test",
		"-w", "/workspace",
		"-p", "127.0.0.1:9090:9090",
		"-p", "127.0.0.1:8080:8080",
		"ghcr.io/opencharly/fedora-test:latest",
		"supervisord", "-n", "-c", "/etc/supervisord.conf",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("BuildStartArgs() =\n  %v\nwant\n  %v", args, want)
	}
}

func TestBuildStartArgsWithVolumes(t *testing.T) {
	volumes := []spec.VolumeMount{
		{VolumeName: "charly-ollama-models", ContainerPath: "/home/user/.ollama/models"},
	}
	args := BuildStartArgs("docker", "ghcr.io/opencharly/ollama:latest", 1000, 1000, nil, "charly-ollama", volumes, nil, false, "127.0.0.1", nil, spec.SecurityConfig{}, []string{"supervisord", "-n", "-c", "/etc/supervisord.conf"}, "/workspace")
	want := []string{
		"docker", "run", "-d", "--rm",
		"--name", "charly-ollama",
		"-w", "/workspace",
		"-v", "charly-ollama-models:/home/user/.ollama/models",
		"ghcr.io/opencharly/ollama:latest",
		"supervisord", "-n", "-c", "/etc/supervisord.conf",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("BuildStartArgs() =\n  %v\nwant\n  %v", args, want)
	}
}

func TestBuildStartArgsWithGPU(t *testing.T) {
	args := BuildStartArgs("docker", "ghcr.io/opencharly/ollama:latest", 1000, 1000, nil, "charly-ollama", nil, nil, true, "127.0.0.1", nil, spec.SecurityConfig{}, []string{"supervisord", "-n", "-c", "/etc/supervisord.conf"}, "/workspace")
	want := []string{
		"docker", "run", "-d", "--rm",
		"--name", "charly-ollama",
		"-w", "/workspace",
		"--gpus", "all",
		"ghcr.io/opencharly/ollama:latest",
		"supervisord", "-n", "-c", "/etc/supervisord.conf",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("BuildStartArgs(gpu=true) =\n  %v\nwant\n  %v", args, want)
	}
}

func TestBuildStartArgsWithGPUPodman(t *testing.T) {
	args := BuildStartArgs("podman", "ghcr.io/opencharly/ollama:latest", 1000, 1000, nil, "charly-ollama", nil, nil, true, "127.0.0.1", nil, spec.SecurityConfig{}, []string{"supervisord", "-n", "-c", "/etc/supervisord.conf"}, "/workspace")
	want := []string{
		"podman", "run", "-d", "--rm",
		"--name", "charly-ollama",
		"-w", "/workspace",
		"--device", "nvidia.com/gpu=all",
		"ghcr.io/opencharly/ollama:latest",
		"supervisord", "-n", "-c", "/etc/supervisord.conf",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("BuildStartArgs(podman+gpu) =\n  %v\nwant\n  %v", args, want)
	}
}

func TestBuildStartArgsWithEnvVars(t *testing.T) {
	envVars := []string{"FOO=bar", "TOKEN=secret"}
	args := BuildStartArgs("docker", "ghcr.io/opencharly/fedora:latest", 1000, 1000, nil, "charly-fedora", nil, nil, false, "127.0.0.1", envVars, spec.SecurityConfig{}, []string{"supervisord", "-n", "-c", "/etc/supervisord.conf"}, "/workspace")
	want := []string{
		"docker", "run", "-d", "--rm",
		"--name", "charly-fedora",
		"-w", "/workspace",
		"-e", "FOO=bar",
		"-e", "TOKEN=secret",
		"ghcr.io/opencharly/fedora:latest",
		"supervisord", "-n", "-c", "/etc/supervisord.conf",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("BuildStartArgs(envVars) =\n  %v\nwant\n  %v", args, want)
	}
}

func TestBuildStartArgsNoSupervisord(t *testing.T) {
	args := BuildStartArgs("podman", "ghcr.io/opencharly/charly-fedora:latest", 0, 0, nil, "charly-charly-fedora", nil, nil, false, "127.0.0.1", nil, spec.SecurityConfig{}, []string{"sleep", "infinity"}, "/workspace")
	want := []string{
		"podman", "run", "-d", "--rm",
		"--name", "charly-charly-fedora",
		"-w", "/workspace",
		"ghcr.io/opencharly/charly-fedora:latest",
		"sleep", "infinity",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("BuildStartArgs(noSupervisord) =\n  %v\nwant\n  %v", args, want)
	}
}

func TestBuildStartArgsWithPrivileged(t *testing.T) {
	sec := spec.SecurityConfig{Privileged: true}
	args := BuildStartArgs("docker", "myimage:latest", 0, 0, nil, "charly-myimage", nil, nil, false, "127.0.0.1", nil, sec, []string{"supervisord", "-n", "-c", "/etc/supervisord.conf"}, "/workspace")
	found := slices.Contains(args, "--privileged")
	if !found {
		t.Errorf("expected --privileged in args: %v", args)
	}
}

func TestBuildStartArgsWithBindMounts(t *testing.T) {
	bindMounts := []ResolvedBindMount{
		{Name: "secrets", HostPath: "/enc/plain", ContPath: "/home/user/.secrets", Encrypted: true},
	}
	args := BuildStartArgs("docker", "myapp:latest", 1000, 1000, nil, "charly-myapp", nil, bindMounts, false, "127.0.0.1", nil, spec.SecurityConfig{}, []string{"supervisord", "-n", "-c", "/etc/supervisord.conf"}, "/workspace")

	found := false
	for i, arg := range args {
		if arg == "-v" && i+1 < len(args) && args[i+1] == "/enc/plain:/home/user/.secrets" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected -v /enc/plain:/home/user/.secrets in args, got: %v", args)
	}
	// Docker should NOT have --userns
	for _, arg := range args {
		if arg == "--userns=keep-id:uid=1000,gid=1000" {
			t.Error("docker should not have --userns=keep-id")
		}
	}
}

func TestBuildStartArgsWithBindMountsPodman(t *testing.T) {
	bindMounts := []ResolvedBindMount{
		{Name: "secrets", HostPath: "/enc/plain", ContPath: "/home/user/.secrets", Encrypted: true},
	}
	args := BuildStartArgs("podman", "myapp:latest", 1000, 1000, nil, "charly-myapp", nil, bindMounts, false, "127.0.0.1", nil, spec.SecurityConfig{}, []string{"supervisord", "-n", "-c", "/etc/supervisord.conf"}, "/workspace")

	found := slices.Contains(args, "--userns=keep-id:uid=1000,gid=1000")
	if !found {
		t.Errorf("expected --userns=keep-id:uid=1000,gid=1000 in podman args, got: %v", args)
	}
}
