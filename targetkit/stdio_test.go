package targetkit

import (
	"context"
	"io"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc"

	pb "github.com/opencharly/sdk/proto"
	"github.com/opencharly/sdk/spec"
	"github.com/opencharly/sdk/testkit"
)

type echoProvider struct{ pb.UnimplementedProviderServer }

func (echoProvider) Invoke(_ context.Context, req *pb.InvokeRequest) (*pb.InvokeReply, error) {
	return &pb.InvokeReply{ResultJson: append([]byte("echo:"), req.GetParamsJson()...)}, nil
}

func TestTargetkitHelperProcess(t *testing.T) {
	if os.Getenv("CHARLY_TARGETKIT_HELPER") != "1" {
		return
	}
	err := ServeStdio(os.Stdin, os.Stdout, func(server *grpc.Server) {
		pb.RegisterProviderServer(server, echoProvider{})
	})
	if err != nil {
		_, _ = io.WriteString(os.Stderr, err.Error())
		os.Exit(2)
	}
	os.Exit(0)
}

// TestGRPCOverProcessTransports exercises real HTTP/2 gRPC frames over both
// supported process transports. The SSH case replaces only the ssh executable
// with the test process; StdioCommand still has to construct and pass the exact
// fixed remote endpoint argv. This catches regressions hidden by argv-only unit
// tests.
func TestGRPCOverProcessTransports(t *testing.T) {
	cases := []struct {
		name   string
		target spec.TargetSpec
		check  func(*testing.T, string, []string)
	}{
		{
			name:   "exec",
			target: spec.TargetSpec{Hops: []spec.TargetHop{{Transport: "exec"}, {Transport: "grpc"}}},
			check: func(t *testing.T, name string, args []string) {
				if name != "charly" || !reflect.DeepEqual(args, []string{"__agent-target", "serve", "--stdio"}) {
					t.Fatalf("exec endpoint = %q %q", name, args)
				}
			},
		},
		{
			name:   "ssh",
			target: spec.TargetSpec{Hops: []spec.TargetHop{{Transport: "ssh", Address: "box", User: "agent"}, {Transport: "grpc"}, {Transport: "tmux"}}},
			check: func(t *testing.T, name string, args []string) {
				want := []string{"-T", "agent@box", "'charly' '__agent-target' 'serve' '--stdio'"}
				if name != "ssh" || !reflect.DeepEqual(args, want) {
					t.Fatalf("ssh endpoint = %q %q, want ssh %q", name, args, want)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			conn, client, err := DialProvider(ctx, tc.target, DialOptions{
				Command: func(ctx context.Context, name string, args ...string) *exec.Cmd {
					tc.check(t, name, args)
					cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestTargetkitHelperProcess$")
					cmd.Env = append(os.Environ(), "CHARLY_TARGETKIT_HELPER=1")
					return cmd
				},
			})
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			t.Cleanup(func() {
				if err := conn.Close(); err != nil {
					t.Errorf("close target connection: %v", err)
				}
			})
			reply, err := client.Invoke(ctx, &pb.InvokeRequest{Class: "fixture", Reserved: "echo", ParamsJson: []byte("payload")})
			if err != nil {
				t.Fatalf("invoke: %v", err)
			}
			if got, want := string(reply.GetResultJson()), "echo:payload"; got != want {
				t.Fatalf("reply = %q, want %q", got, want)
			}
		})
	}
}

func TestGRPCOverRealOpenSSH(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("OpenSSH client unavailable")
	}
	server := testkit.StartSSHProcessServer(t, func() *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestTargetkitHelperProcess$")
		cmd.Env = append(os.Environ(), "CHARLY_TARGETKIT_HELPER=1")
		return cmd
	})
	t.Setenv("HOME", server.Home)
	target := spec.TargetSpec{Hops: []spec.TargetHop{
		{Transport: "ssh", Address: server.Address, User: "agent", Port: server.Port, IdentityFile: server.IdentityFile, Options: spec.StrMap{
			"IdentitiesOnly": "yes", "LogLevel": "ERROR", "StrictHostKeyChecking": "no", "UserKnownHostsFile": "/dev/null",
		}},
		{Transport: "grpc"},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, client, err := DialProvider(ctx, target, DialOptions{Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close SSH target connection: %v", err)
		}
	})
	reply, err := client.Invoke(ctx, &pb.InvokeRequest{Class: "fixture", Reserved: "echo", ParamsJson: []byte("through-openssh")})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(reply.GetResultJson()), "echo:through-openssh"; got != want {
		t.Fatalf("real SSH gRPC response = %q, want %q", got, want)
	}
}

func TestSSHGRPCCommandIsFixedArgv(t *testing.T) {
	target := spec.TargetSpec{Hops: []spec.TargetHop{
		{Transport: "ssh", Address: "box.example", User: "agent", Port: 2222, IdentityFile: "/keys/id"},
		{Transport: "grpc"},
		{Transport: "tmux"},
	}}
	got, err := StdioCommand(target, DialOptions{CharlyBinary: "/usr/bin/charly"})
	if err != nil {
		t.Fatal(err)
	}
	// CharlyBinary is a controller-local path and must never be sent to the
	// remote box. The remote endpoint resolves its own installation via PATH.
	want := []string{"ssh", "-T", "-p", "2222", "-i", "/keys/id", "agent@box.example", "'charly' '__agent-target' 'serve' '--stdio'"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v, want %#v", got, want)
	}
}

func TestSSHGRPCCommandUsesReplicatedRemoteEndpoint(t *testing.T) {
	target := spec.TargetSpec{Hops: []spec.TargetHop{
		{Transport: "ssh", Address: "box.example", User: "agent"},
		{Transport: "grpc"},
		{Transport: "tmux"},
	}}
	got, err := StdioCommand(target, DialOptions{
		CharlyBinary:       "/controller/worktree/bin/charly",
		RemoteCharlyBinary: "/tmp/charly-2026.200.1200",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ssh", "-T", "agent@box.example", "'/tmp/charly-2026.200.1200' '__agent-target' 'serve' '--stdio'"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v, want %#v", got, want)
	}
}

func TestSSHCommandPreservesWorkingDirectoryAndEnvironment(t *testing.T) {
	target := spec.TargetSpec{WorkingDir: "/work dir", Hops: []spec.TargetHop{{Transport: "ssh", Address: "box", Env: spec.StrMap{"TOKEN": "a b'$"}}, {Transport: "grpc"}}}
	got, err := StdioCommand(target, DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	wantRemote := "cd '/work dir' && env 'TOKEN=a b'\\''$' 'charly' '__agent-target' 'serve' '--stdio'"
	if got[len(got)-1] != wantRemote {
		t.Fatalf("remote command = %q, want %q", got[len(got)-1], wantRemote)
	}
}

func TestGenericRouteCrossProduct(t *testing.T) {
	routes := []spec.TargetSpec{
		{Hops: []spec.TargetHop{{Transport: "exec"}, {Transport: "grpc"}}},
		{Hops: []spec.TargetHop{{Transport: "exec"}, {Transport: "grpc"}, {Transport: "tmux"}}},
		{Hops: []spec.TargetHop{{Transport: "ssh", Address: "box"}, {Transport: "grpc"}}},
		{Hops: []spec.TargetHop{{Transport: "ssh", Address: "box"}, {Transport: "grpc"}, {Transport: "exec"}, {Transport: "grpc"}, {Transport: "tmux"}}},
	}
	for _, route := range routes {
		if _, err := StdioCommand(route, DialOptions{}); err != nil {
			t.Fatalf("route %#v: %v", route.Hops, err)
		}
	}
}

func TestRouteRequiresGRPC(t *testing.T) {
	_, err := StdioCommand(spec.TargetSpec{Hops: []spec.TargetHop{{Transport: "ssh", Address: "box"}, {Transport: "tmux"}}}, DialOptions{})
	if err == nil {
		t.Fatal("route without grpc accepted")
	}
}
