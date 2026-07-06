package kit

import "github.com/opencharly/sdk/spec"

// descent.go — the venue-hop descent-descriptor stamper (the descent de-type,
// Cutover H). This is the SINGLE source of the substrate-word → nesting-transport
// mapping (R3): candy/plugin-substrate and candy/plugin-group call StampDescent in
// their OpLoad echo so the kernel's deploy chain (appendHopForFlatPath) descends
// generically BY TRANSPORT, never by switching on the substrate kind word. The
// transport SET is the kernel's closed nesting-boundary vocabulary (JumpKind); the
// word→transport MAPPING lives here, out of the kernel.

// StampDescent stamps node.Descent from node.Target and recurses into the whole
// nested (Children) + peer (Members) subtree, so every node the deploy chain can
// descend into carries a descriptor. Idempotent — re-stamping writes the same value.
func StampDescent(node *spec.Deploy) {
	if node == nil {
		return
	}
	node.Descent = descentForTarget(node.Target)
	for _, child := range node.Children {
		StampDescent(child)
	}
	for _, member := range node.Members {
		StampDescent(member)
	}
}

// descentForTarget maps a substrate word to its nesting transport.
func descentForTarget(target string) *spec.DescentDescriptor {
	switch target {
	case "vm":
		return &spec.DescentDescriptor{Transport: "ssh"}
	case "k8s":
		return &spec.DescentDescriptor{Transport: "reject"}
	case "local":
		// A local deploy's own ROOT executor runs host-side; it adds no hop.
		return &spec.DescentDescriptor{Transport: "none", HostRooted: true}
	case "android":
		// Reached via adb over the parent pod's published port — no chain hop.
		return &spec.DescentDescriptor{Transport: "none"}
	default:
		// pod (the default substrate) enters its container by name. A targetless
		// group venue (Target=="") also lands here — its members are peers, never
		// chain-descended, and the kernel's empty→pod default historically treated
		// it as pod, so container-exec is the behavior-preserving default.
		return &spec.DescentDescriptor{Transport: "container-exec"}
	}
}
