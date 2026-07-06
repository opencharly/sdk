package spec

// substrate_template_wire.go — the OpResolve envelopes for the local + android
// substrate-template de-type (Cutover I). candy/plugin-substrate projects an authored
// local:/android: TEMPLATE into a Resolved* envelope the kernel consumes without
// importing the concrete spec.Local / spec.Android.

// ResolvedLocal is the resolve-to-envelope form of a `local:` template — a candy-stack
// applied to a host. The kernel reads it (candy stack / install opts / plan), never
// spec.Local.
type ResolvedLocal struct {
	Candy       []CandyRef        `json:"candy,omitempty"`
	InstallOpts *InstallOpts      `json:"install_opts,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Description string            `json:"description,omitempty"`
	Plan        []Step            `json:"plan,omitempty"`
	// Raw is the opaque authored body (the kernel passes it back unread where a
	// verbatim template is needed).
	Raw RawBody `json:"raw,omitempty"`
}

// ResolvedAndroid is the resolve-to-envelope form of an `android:` device template.
type ResolvedAndroid struct {
	Serial        string         `json:"serial,omitempty"`
	Device        string         `json:"device,omitempty"`
	ApiLevel      int            `json:"api_level,omitempty"`
	GoogleAccount *GoogleAccount `json:"google_account,omitempty"`
	Plan          []Step         `json:"plan,omitempty"`
	Box           string         `json:"box,omitempty"`
	Adb           *AdbEndpoint   `json:"adb,omitempty"`
	Raw           RawBody        `json:"raw,omitempty"`
}

// IsEndpoint reports whether the resolved device targets a remote adb endpoint.
func (a *ResolvedAndroid) IsEndpoint() bool {
	return a != nil && a.Adb != nil && a.Adb.Host != ""
}

// EffectiveSerial returns the device serial, defaulting to the emulator serial.
func (a *ResolvedAndroid) EffectiveSerial() string {
	if a != nil && a.Serial != "" {
		return a.Serial
	}
	return "emulator-5554"
}

// ResolvedPod is the resolve-to-envelope form of a `pod:` template — a box + sidecar
// + plan bundle. The kernel reads it (currently the include-spliced Plan), never spec.Pod.
type ResolvedPod struct {
	Box         CandyRef          `json:"box,omitempty"`
	Sidecar     []PodSidecar      `json:"sidecar,omitempty"`
	Secret      []DeploySecret    `json:"secret,omitempty"`
	EnvDefaults map[string]string `json:"env_default,omitempty"`
	Plan        []Step            `json:"plan,omitempty"`
	Raw         RawBody           `json:"raw,omitempty"`
}

// PodResolveInput carries one opaque pod template body to project.
type PodResolveInput struct {
	Pod RawBody `json:"pod"`
}

// PodResolveReply wraps the resolved pod template.
type PodResolveReply struct {
	Resolved *ResolvedPod `json:"resolved,omitempty"`
}

// LocalResolveInput / AndroidResolveInput carry one opaque template body to project.
type LocalResolveInput struct {
	Local RawBody `json:"local"`
}

type AndroidResolveInput struct {
	Android RawBody `json:"android"`
}

// LocalResolveReply / AndroidResolveReply wrap the resolved template.
type LocalResolveReply struct {
	Resolved *ResolvedLocal `json:"resolved,omitempty"`
}

type AndroidResolveReply struct {
	Resolved *ResolvedAndroid `json:"resolved,omitempty"`
}

// SubstrateTemplateResolveRequest is the discriminated OpResolve request for the
// substrate-template de-type: exactly one of Local / Android / Pod is set.
type SubstrateTemplateResolveRequest struct {
	Local   *LocalResolveInput   `json:"local,omitempty"`
	Android *AndroidResolveInput `json:"android,omitempty"`
	Pod     *PodResolveInput     `json:"pod,omitempty"`
}
