// k8sgen.cue — the Kustomize-GENERATOR wire types shared between charly's core
// and the compiled-in candy/plugin-k8sgen (C8/M13; SDD conversion, per the
// standing operator directive: a hand-written wire struct not yet CUE-sourced is
// conversion-in-progress, never a sanctioned exception). These types live in
// package spec — the ONE importable home — because BOTH the host (the in-core
// GenerateK8sKustomize shim, k8s_generate.go) AND the plugin (candy/plugin-k8sgen)
// construct and exchange them across the OpEmit Invoke boundary. The host builds
// a K8sGenInput from K8sGenerateOpts, the plugin runs the pure generator
// (GenerateTree) and returns a K8sGenReply of RELATIVE-pathed manifest docs, and
// the host does the disk I/O + the host-side egress gate (ValidateEgressValue)
// before the bytes hit disk. Plain structs — gengotypes generates them
// faithfully, no disjunction needed.

// #K8sGenInput is the pure-generation input the host ships to plugin-k8sgen
// over OpEmit. Deploy is the deployment node (the former BundleNode =
// spec.Deploy); Cluster is the kind:k8s cluster template (the former K8sSpec =
// spec.K8s); Ports / UID / GID are lifted from the image's OCI-label
// Capabilities host-side so the plugin needs no access to the package-main
// BoxMetadata type.
#K8sGenInput: {
	deployment_name!: string @go(DeploymentName)
	instance!:        string @go(Instance)
	image_ref!:       string @go(ImageRef)
	deploy!:          #Deploy @go(Deploy) // = the former BundleNode
	// cluster is the decoded kind:k8s cluster template. After the k8s
	// substrate-value de-type (Cutover K) the KERNEL no longer sets it — it
	// ships the opaque body in ClusterRaw and the plugin decodes ClusterRaw
	// into Cluster before generating, so the kernel never types spec.K8s.
	cluster?:     #K8s @go(Cluster)
	cluster_raw?: bytes @go(ClusterRaw,type=RawBody) // opaque k8s cluster body (Cutover K)
	ports!: [...string] @go(Ports) // from BoxMetadata.Port
	uid!:        int    @go(UID,type=int) // from BoxMetadata.UID
	gid!:        int    @go(GID,type=int) // from BoxMetadata.GID
	output_dir!: string @go(OutputDir)    // provenance; the host owns disk paths
}

// #K8sGenFile is one generated manifest the plugin returns: its RELATIVE path
// (under OutputDir/DeploymentName, e.g. "base/deployment.yaml"), the manifest
// as JSON (the host unmarshals it back to a value, egress-validates, and
// writes it as YAML), and the egress kind that gates it ("k8s_object" or
// "kustomization").
#K8sGenFile: {
	rel_path!:    string @go(RelPath)
	doc!:         bytes  @go(Doc,type=RawBody)
	egress_kind!: string @go(EgressKind)
}

// #K8sGenReply is the pure-generation output: the RELATIVE overlay path the
// host joins onto OutputDir/DeploymentName to form the `kubectl apply -k`
// argument, and the collected manifest files (base resources + base/overlay
// kustomizations).
#K8sGenReply: {
	overlay_rel_path!: string @go(OverlayRelPath)
	files!: [...#K8sGenFile] @go(Files)
}
