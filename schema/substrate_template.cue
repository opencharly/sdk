// substrate_template.cue — the OpResolve envelopes for the local + android +
// pod + k8s + vm substrate-template de-type (Cutover I; SDD conversion, per the
// standing operator directive: a hand-written wire struct not yet CUE-sourced is
// conversion-in-progress, never a sanctioned exception). candy/plugin-substrate
// projects an authored local:/android:/pod:/k8s:/vm: TEMPLATE into a Resolved*
// envelope the kernel consumes without importing the concrete spec.Local /
// spec.Android / spec.Pod / spec.K8s / spec.Vm. Written out explicitly (not
// embedding the authoring kind schemas #Local/#Android/#Pod/#K8s, whose
// required/optional field sets differ from the resolved envelope's — e.g.
// #Local's `candy` is REQUIRED, ResolvedLocal's is OPTIONAL) so every field's
// state is independently auditable against the former hand type. Plain
// structs — gengotypes generates them faithfully, no disjunction needed.
// IsEndpoint()/EffectiveSerial() are pure Go METHODS on ResolvedAndroid — CUE
// cannot express them — and stay hand-written in
// spec/substrate_template_methods.go (mirrors Op.Kind() in
// spec/charly_methods.go: a method, not a type).

// #ResolvedLocal is the resolve-to-envelope form of a `local:` template — a
// candy-stack applied to a host. The kernel reads it (candy stack / install
// opts / plan), never spec.Local.
#ResolvedLocal: {
	candy?: [...#CandyRef] @go(Candy)
	install_opts?: #InstallOpts @go(InstallOpts,optional=nillable)
	env?: {[string]: string} @go(Env)
	description?: string @go(Description)
	plan?: [...#Step] @go(Plan)
	// raw is the opaque authored body (the kernel passes it back unread where
	// a verbatim template is needed).
	raw?: bytes @go(Raw,type=RawBody)
}

// #ResolvedAndroid is the resolve-to-envelope form of an `android:` device
// template.
#ResolvedAndroid: {
	serial?:         string @go(Serial)
	device?:         string @go(Device)
	api_level?:      int    @go(ApiLevel,type=int)
	google_account?: #GoogleAccount @go(GoogleAccount,optional=nillable)
	plan?: [...#Step] @go(Plan)
	box?: string @go(Box)
	adb?: #AdbEndpoint @go(Adb,optional=nillable)
	raw?: bytes @go(Raw,type=RawBody)
}

// #ResolvedPod is the resolve-to-envelope form of a `pod:` template — a box +
// sidecar + plan bundle. The kernel reads it (currently the include-spliced
// Plan), never spec.Pod.
#ResolvedPod: {
	box?: #CandyRef @go(Box)
	sidecar?: [...#PodSidecar] @go(Sidecar)
	secret?: [...#DeploySecret] @go(Secret)
	env_default?: {[string]: string} @go(EnvDefaults)
	plan?: [...#Step] @go(Plan)
	raw?: bytes @go(Raw,type=RawBody)
}

// #PodResolveInput carries one opaque pod template body to project.
#PodResolveInput: {
	pod!: bytes @go(Pod,type=RawBody)
}

// #PodResolveReply wraps the resolved pod template.
#PodResolveReply: {
	resolved?: #ResolvedPod @go(Resolved,optional=nillable)
}

// #ResolvedK8s is the resolve-to-envelope form of a `k8s:` cluster template.
// The kernel reads only KubeconfigContext (the deploy preresolver); the full
// cluster model rides opaquely in Raw and is decoded by candy/plugin-k8sgen,
// never the kernel.
#ResolvedK8s: {
	kubeconfig_context?: string @go(KubeconfigContext)
	raw?: bytes @go(Raw,type=RawBody)
}

// #K8sResolveInput carries one opaque k8s cluster template body to project.
#K8sResolveInput: {
	k8s!: bytes @go(K8s,type=RawBody)
}

// #K8sResolveReply wraps the resolved k8s cluster template.
#K8sResolveReply: {
	resolved?: #ResolvedK8s @go(Resolved,optional=nillable)
}

// #LocalResolveInput / #AndroidResolveInput carry one opaque template body to
// project.
#LocalResolveInput: {
	local!: bytes @go(Local,type=RawBody)
}

#AndroidResolveInput: {
	android!: bytes @go(Android,type=RawBody)
}

// #LocalResolveReply / #AndroidResolveReply wrap the resolved template.
#LocalResolveReply: {
	resolved?: #ResolvedLocal @go(Resolved,optional=nillable)
}

#AndroidResolveReply: {
	resolved?: #ResolvedAndroid @go(Resolved,optional=nillable)
}

// #SubstrateTemplateResolveRequest is the discriminated OpResolve request for
// the substrate-template de-type: exactly one of Local / Android / Pod is set.
#SubstrateTemplateResolveRequest: {
	local?:   #LocalResolveInput   @go(Local,optional=nillable)
	android?: #AndroidResolveInput @go(Android,optional=nillable)
	pod?:     #PodResolveInput     @go(Pod,optional=nillable)
	k8s?:     #K8sResolveInput     @go(K8s,optional=nillable)
	vm?:      #VmResolveInput      @go(Vm,optional=nillable)
}

// #VmResolveInput carries one opaque vm template body to project (Cutover L).
#VmResolveInput: {
	vm!: bytes @go(Vm,type=RawBody)
}

// #VmResolveReply wraps the resolved vm value envelope (#ResolvedVm lives in
// vm.cue).
#VmResolveReply: {
	resolved?: #ResolvedVm @go(Resolved,optional=nillable)
}
