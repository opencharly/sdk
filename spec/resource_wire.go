package spec

// resource_wire.go — the OpResolve envelope for the resource de-type (Cutover G).
// candy/plugin-resource resolves an authored `resource:` (an exclusive-host-resource
// claim, e.g. an nvidia-gpu vendor selector) into a ResolvedResource the kernel's
// GPU arbiter consumes without importing the concrete spec.Resource.

// ResolvedGpuSelector is the resolved GPU-vendor selector (the exclusive-resource
// arbiter matches host GPUs by vendor).
type ResolvedGpuSelector struct {
	Vendor string `json:"vendor,omitempty"`
}

// ResolvedResource is the resolve-to-envelope form of a `resource:` entity — the
// arbiter reads it (a claim-token → selector), never spec.Resource.
type ResolvedResource struct {
	Gpu *ResolvedGpuSelector `json:"gpu,omitempty"`
}

// ResourceResolveInput is candy/plugin-resource's OpResolve input: the opaque
// resource body to project into a ResolvedResource.
type ResourceResolveInput struct {
	Resource RawBody `json:"resource"`
}

// ResourceResolveReply wraps the resolved resource.
type ResourceResolveReply struct {
	Resolved *ResolvedResource `json:"resolved,omitempty"`
}
