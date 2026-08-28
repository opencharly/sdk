package vmshared

import (
	"strings"
	"testing"
)

func boolp(b bool) *bool { return &b }

// The regression that bounds the whole change: a VM that declares no graphics and no video
// renders EXACTLY as it did before the GPU surface existed — one `-display none`, no video
// device, no memory-backend object. Every new emitter is conditional on a declared field, and
// this is what proves it.
func TestRenderQemuArgv_NoVideoDeclaredIsUnchanged(t *testing.T) {
	spec := &VmSpec{Source: VmSource{Kind: "cloud_image", URL: "http://x/y.qcow2"}}
	args := strings.Join(RenderQemuArgv(spec, VmRuntimeParams{RamMB: 4096, Cpus: 2}, QemuRuntimePaths{}), " ")

	if !strings.Contains(args, "-display none") {
		t.Errorf("a VM with no graphics must still render `-display none`; got %q", args)
	}
	for _, forbidden := range []string{"virtio-vga", "virtio-gpu", "memory-backend-memfd", "egl-headless"} {
		if strings.Contains(args, forbidden) {
			t.Errorf("undeclared GPU config leaked into argv (%q): %q", forbidden, args)
		}
	}
}

// The configuration Spike 3 proved works, expressed entirely in charly YAML. Before this
// change every field below rendered to NOTHING on the qemu backend: the argv was
// `-display none` with no video device at all.
func TestRenderQemuArgv_BlobNativeContextRendersFully(t *testing.T) {
	spec := &VmSpec{
		Source: VmSource{Kind: "cloud_image", URL: "http://x/y.qcow2"},
		Libvirt: &LibvirtDomain{
			Devices: &LibvirtDevices{
				Video: []LibvirtVideo{{
					Model: "virtio", Device: "virtio-gpu-gl", Blob: boolp(true),
					Heads: 1, Alias: "ua-gpu",
				}},
				Graphics: []LibvirtGraphics{{
					Type: "egl-headless",
					GL:   &LibvirtGraphicsGL{RenderNode: "/dev/dri/renderD128"},
				}},
			},
			QemuOverride: map[string]map[string]any{
				"ua-gpu": {"drm_native_context": true, "hostmem": "4G", "venus": false},
			},
		},
	}
	args := strings.Join(RenderQemuArgv(spec, VmRuntimeParams{RamMB: 8192, Cpus: 4}, QemuRuntimePaths{}), " ")

	for _, want := range []string{
		// libvirt says virtio-gpu-gl; QEMU wants virtio-gpu-gl-pci. Emitting libvirt's
		// spelling verbatim would fail at startup.
		"-device virtio-gpu-gl-pci,",
		"blob=on",
		"max_outputs=1",
		"id=ua-gpu",
		// The override properties — the only path to these knobs, since libvirt models
		// no element for them.
		"drm_native_context=on",
		"hostmem=4G",
		"venus=off",
		// blob requires shared memory backing; without it QEMU refuses the device.
		"-object memory-backend-memfd,id=mem0,size=8192M,share=on",
		"-machine memory-backend=mem0",
		// The display is what gives the guest's virtio-gpu a host GL context.
		"-display egl-headless,rendernode=/dev/dri/renderD128",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("argv missing %q\n---\n%s", want, args)
		}
	}
	if strings.Contains(args, "-display none") {
		t.Errorf("egl-headless was declared but the argv still says `-display none`:\n%s", args)
	}
}

// A bool is `on`/`off` on the QEMU command line and `true`/`false` in libvirt's
// <qemu:property type='bool'/>. The same authored value, two spellings — which is why the
// rendering is per backend rather than stringified once at decode. QEMU rejects true/false
// for an on/off property, so getting this wrong fails at VM start, not at render.
func TestQemuOverrideProps_BoolsAreOnOffAndOrderIsStable(t *testing.T) {
	lv := &LibvirtDomain{QemuOverride: map[string]map[string]any{
		"ua-gpu": {"venus": true, "blob": false, "hostmem": "4G", "max_hostmem": 8},
	}}
	got := strings.Join(qemuOverrideProps(lv, "ua-gpu"), ",")
	// Sorted by property name — Go map order is random, and an argv that differs between
	// runs is untestable and unreadable in a process listing.
	if want := "blob=off,hostmem=4G,max_hostmem=8,venus=on"; got != want {
		t.Errorf("qemuOverrideProps = %q, want %q", got, want)
	}
}

// device= is what makes a GL-capable device reachable at all: model:virtio alone yields
// plain virtio-vga, which has no GL and therefore cannot do blob or a native context.
func TestQemuVideoDevice_ModelVsDevice(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   LibvirtVideo
		want string
	}{
		{"model virtio alone has no GL", LibvirtVideo{Model: "virtio"}, "virtio-vga"},
		{"device selects the GL variant", LibvirtVideo{Model: "virtio", Device: "virtio-gpu-gl"}, "virtio-gpu-gl-pci"},
		{"libvirt virtio-gpu becomes the pci device", LibvirtVideo{Device: "virtio-gpu"}, "virtio-gpu-pci"},
		{"vga variants keep their name", LibvirtVideo{Device: "virtio-vga-gl"}, "virtio-vga-gl"},
		{"model none emits no device", LibvirtVideo{Model: "none"}, ""},
		{"qxl", LibvirtVideo{Model: "qxl"}, "qxl-vga"},
		// An unmapped device= passes through rather than vanishing: libvirt's vocabulary
		// grows, and a name QEMU rejects is a loud failure where a dropped device is a VM
		// that boots with the wrong hardware.
		{"unknown device passes through", LibvirtVideo{Device: "virtio-gpu-rutabaga"}, "virtio-gpu-rutabaga"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := qemuVideoDevice(tc.in); got != tc.want {
				t.Errorf("qemuVideoDevice = %q, want %q", got, tc.want)
			}
		})
	}
}

// An override targeting a device with no alias cannot be attached to anything on this
// backend either — QEMU matches by id=, which is what the alias becomes. The libvirt bridge
// rejects this case outright; here it must simply not silently attach to the wrong device.
func TestRenderQemuArgv_OverrideNeedsTheAlias(t *testing.T) {
	spec := &VmSpec{
		Source: VmSource{Kind: "cloud_image", URL: "http://x/y.qcow2"},
		Libvirt: &LibvirtDomain{
			Devices:      &LibvirtDevices{Video: []LibvirtVideo{{Model: "virtio", Device: "virtio-gpu-gl"}}},
			QemuOverride: map[string]map[string]any{"ua-gpu": {"drm_native_context": true}},
		},
	}
	args := strings.Join(RenderQemuArgv(spec, VmRuntimeParams{RamMB: 2048, Cpus: 2}, QemuRuntimePaths{}), " ")
	if strings.Contains(args, "drm_native_context") {
		t.Errorf("an override attached to a device that declares no alias:\n%s", args)
	}
}
