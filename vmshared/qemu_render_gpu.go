package vmshared

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// qemu_render_gpu.go — the direct-QEMU backend's video/display rendering.
//
// Before this, RenderQemuArgv emitted a flat `-display none` and NO video device at all,
// so every `libvirt.devices.video` / `graphics` / `qemu_override` field silently did
// nothing on `--backend qemu`. `-display none` is correct only when no video is declared;
// with one declared it is an unrequested headless VM that looks configured.

// qemuVideoDevices maps libvirt's <video><model device='…'/> vocabulary to the QEMU device
// names. The two are NOT the same string: libvirt says `virtio-gpu`, QEMU wants
// `virtio-gpu-pci` (the VGA-flavoured variants keep their names because `virtio-vga`
// already IS the device name).
var qemuVideoDevices = map[string]string{
	"virtio-vga":     "virtio-vga",
	"virtio-vga-gl":  "virtio-vga-gl",
	"virtio-gpu":     "virtio-gpu-pci",
	"virtio-gpu-gl":  "virtio-gpu-gl-pci",
	"vhost-user-vga": "vhost-user-vga",
	"vhost-user-gpu": "vhost-user-gpu-pci",
}

// qemuVideoModels is the fallback when no explicit device= is set: libvirt's <video
// model type='…'> vocabulary. `virtio` yields plain virtio-vga, which has NO GL — which is
// exactly why device= exists and why a blob/native-context guest must set it.
var qemuVideoModels = map[string]string{
	"virtio":  "virtio-vga",
	"qxl":     "qxl-vga",
	"cirrus":  "cirrus-vga",
	"vga":     "VGA",
	"vmvga":   "vmware-svga",
	"bochs":   "bochs-display",
	"ramfb":   "ramfb",
	"virtio2": "virtio-gpu-pci",
}

// qemuVideoDevice resolves one video entry to its QEMU device name, or "" when the entry
// asks for no device at all (model: none).
func qemuVideoDevice(v LibvirtVideo) string {
	if v.Device != "" {
		if dev, ok := qemuVideoDevices[v.Device]; ok {
			return dev
		}
		// An unmapped device= is passed through verbatim rather than dropped: libvirt's
		// vocabulary grows, and a name QEMU rejects is a loud startup failure, where a
		// silently-omitted device is a VM that boots with the wrong hardware.
		return v.Device
	}
	if v.Model == "none" || v.Model == "" {
		return ""
	}
	if dev, ok := qemuVideoModels[v.Model]; ok {
		return dev
	}
	return v.Model
}

// qemuVideoArgs renders `-device <dev>,<props>` for every declared video device, folding in
// that device's libvirt.qemu_override properties.
//
// The override is the ONLY path for QEMU device properties libvirt models no element for —
// drm_native_context, hostmem, max_hostmem, venus — so on this backend it is not an escape
// hatch, it is the main road.
func qemuVideoArgs(lv *LibvirtDomain) []string {
	if lv == nil || lv.Devices == nil {
		return nil
	}
	var args []string
	for _, v := range lv.Devices.Video {
		dev := qemuVideoDevice(v)
		if dev == "" {
			continue
		}
		props := []string{dev}
		if v.Blob != nil && *v.Blob {
			props = append(props, "blob=on")
		}
		// libvirt's heads is QEMU's max_outputs on the virtio-gpu family.
		if v.Heads > 0 && strings.HasPrefix(dev, "virtio-") {
			props = append(props, "max_outputs="+strconv.Itoa(v.Heads))
		}
		if v.Alias != "" {
			props = append(props, "id="+v.Alias)
			props = append(props, qemuOverrideProps(lv, v.Alias)...)
		}
		args = append(args, "-device", strings.Join(props, ","))
	}
	return args
}

// qemuOverrideProps renders one device's overrides as sorted `prop=value` pairs. Sorted
// because Go map iteration is random and an argv that differs run to run is untestable and
// unreadable in a process listing.
func qemuOverrideProps(lv *LibvirtDomain, alias string) []string {
	props := lv.QemuOverride[alias]
	if len(props) == 0 {
		return nil
	}
	names := make([]string, 0, len(props))
	for n := range props {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, n+"="+qemuPropScalar(props[n]))
	}
	return out
}

// qemuPropScalar renders a property value the way the QEMU command line spells it. Note
// bools are on/off here, not the true/false that libvirt's <qemu:property type='bool'/>
// uses — the same declared value, two spellings, which is precisely why this is rendered
// per backend rather than stringified once at decode.
func qemuPropScalar(v any) string {
	switch t := v.(type) {
	case bool:
		if t {
			return "on"
		}
		return "off"
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

// qemuDisplayArg picks the `-display` value from the declared graphics. Returns "none" when
// nothing is declared, preserving today's behaviour exactly for every VM that declares no
// graphics.
//
// egl-headless is the one that matters for a GPU guest: it is what gives the guest's
// virtio-gpu a host GL context, and `rendernode=` is what pins it to a specific DRM node.
func qemuDisplayArg(lv *LibvirtDomain) string {
	if lv == nil || lv.Devices == nil || len(lv.Devices.Graphics) == 0 {
		return "none"
	}
	for _, g := range lv.Devices.Graphics {
		switch g.Type {
		case "egl-headless":
			if g.GL != nil && g.GL.RenderNode != "" {
				return "egl-headless,rendernode=" + g.GL.RenderNode
			}
			return "egl-headless"
		case "sdl":
			return "sdl"
		}
	}
	// vnc/spice/rdp/dbus are wired by their own QEMU options, not -display; the direct
	// backend does not render those (RenderQemuArgv's documented gap), so the display
	// stays headless rather than pretending otherwise.
	return "none"
}

// qemuSharedMemoryArgs emits the memfd-backed shared memory a blob-resource virtio-gpu
// requires. Same coupling the libvirt renderer auto-pairs via ensureSharedMemoryBacking:
// blob means the guest maps host memory directly, which cannot work with QEMU's default
// anonymous memory backend.
func qemuSharedMemoryArgs(lv *LibvirtDomain, ramMB int) []string {
	if lv == nil || lv.Devices == nil {
		return nil
	}
	blob := false
	for _, v := range lv.Devices.Video {
		if v.Blob != nil && *v.Blob {
			blob = true
			break
		}
	}
	if !blob {
		return nil
	}
	return []string{
		"-object", fmt.Sprintf("memory-backend-memfd,id=mem0,size=%dM,share=on", ramMB),
		"-machine", "memory-backend=mem0",
	}
}
