package charlylib

import (
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	pb "github.com/opencharly/spec/proto"
)

type fakeProvider struct{ pb.UnimplementedProviderServer }
type fakeMeta struct {
	pb.UnimplementedPluginMetaServer
}

func fakePlugin(tag *string, label string) Plugin {
	return Plugin{
		Provider: func() pb.ProviderServer { *tag = label; return &fakeProvider{} },
		Meta:     func() pb.PluginMetaServer { return &fakeMeta{} },
		CLI:      func([]string) int { *tag = label; return 0 },
	}
}

func TestRegistryNamesSorted(t *testing.T) {
	reg := Registry{"plugin-z": {}, "plugin-a": {}, "plugin-m": {}}
	want := []string{"plugin-a", "plugin-m", "plugin-z"}
	if got := reg.Names(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
}

// TestRunDispatchesByArgv0 is the behaviour that makes the shared host work:
// the binary name (argv[0] base) selects the registered plugin.
func TestRunDispatchesByArgv0(t *testing.T) {
	origArgs, origMain := os.Args, pluginMain
	defer func() { os.Args, pluginMain = origArgs, origMain }()

	var ran string
	reg := Registry{
		"plugin-a": fakePlugin(&ran, "plugin-a"),
		"plugin-b": fakePlugin(&ran, "plugin-b"),
	}
	// transport.Main would start a server; capture what Run selected instead.
	pluginMain = func(p pb.ProviderServer, m pb.PluginMetaServer, cli func([]string) int) {
		if p == nil || m == nil || cli == nil {
			t.Fatal("Run passed a nil provider/meta/cli")
		}
		_ = cli(nil)
	}

	// symlink-style absolute path whose base name is the plugin binary name.
	os.Args = []string{"/usr/lib/charly/plugins/plugin-b"}
	Run(reg)
	if ran != "plugin-b" {
		t.Fatalf("Run dispatched %q, want plugin-b", ran)
	}
}

// TestResolve covers the two loud-failure modes without spawning a process.
func TestResolve(t *testing.T) {
	var tag string
	good := Registry{"plugin-a": fakePlugin(&tag, "plugin-a")}

	if _, err := resolve(good, "plugin-a"); err != nil {
		t.Errorf("resolve(known) = %v, want nil", err)
	}
	if _, err := resolve(good, "plugin-nope"); err == nil || !strings.Contains(err.Error(), "known plugins") {
		t.Errorf("resolve(unknown) = %v, want a known-plugins error", err)
	}
	bad := Registry{"plugin-b": {Provider: func() pb.ProviderServer { return &fakeProvider{} }}}
	if _, err := resolve(bad, "plugin-b"); err == nil || !strings.Contains(err.Error(), "nil") {
		t.Errorf("resolve(nil-field) = %v, want a nil-field error", err)
	}
}

// TestRunUnknownNameExitsLoudly runs Run in a subprocess (it calls os.Exit) and
// asserts the mis-installed-symlink failure is loud + non-zero, never silent.
func TestRunUnknownNameExitsLoudly(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestRunUnknownNameExitsLoudly")
	cmd.Env = append(os.Environ(), "CHARLYLIB_TEST_UNKNOWN=plugin-nope")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("Run with an unknown name exited 0; want a non-zero exit\n%s", out)
	}
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 2 {
		t.Fatalf("exit = %v, want code 2\n%s", err, out)
	}
	if !strings.Contains(string(out), "known plugins") {
		t.Fatalf("stderr missing the known-plugins list:\n%s", out)
	}
}

// TestMain lets the subprocess arm of TestRunUnknownNameExitsLoudly drive Run.
func TestMain(m *testing.M) {
	if name := os.Getenv("CHARLYLIB_TEST_UNKNOWN"); name != "" {
		os.Args = []string{name}
		Run(Registry{})
		return // unreachable: Run exits
	}
	os.Exit(m.Run())
}
