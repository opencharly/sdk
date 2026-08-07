package deploykit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/opencharly/sdk"
)

// credential_executor.go — the ONE shared verb:credential-backed CredentialAccess (R3). A plugin
// resolves/persists candy secrets by peer-dispatching verb:credential (candy/plugin-secrets) over
// its own sdk.Executor.InvokeProvider; every consumer needs the SAME two RPC-backed ops bundled into
// a deploykit.CredentialAccess. This is that single abstraction, GENERAL (no per-caller shaping): it
// takes only (ctx, *sdk.Executor) and returns a plain deploykit.CredentialAccess, so any deploy-time
// plugin drops it in as-is.
//
// #55 K4 (this cutover) is its FIRST consumer beyond the source: candy/plugin-fleet's plugin-side
// secret resolution (secrets_artifacts.go) + candy/plugin-deploy-pod (repointed off its own former
// pluginCredentialAccess copy, the source this was extracted from). The three OTHER pre-existing
// copies — candy/plugin-pod/enc_cmd.go, candy/plugin-settings/config.go,
// candy/plugin-substrate/status_android_collect.go — are UNTOUCHED tracked debt, scheduled onto this
// helper by task #86 (R3 dedup batch); they're byte-compatible with this wire, so #86 is a clean
// drop-in with no reshaping here.

// credentialViaExecInput / credentialViaExecReply are the verb:credential wire forms — byte-compatible
// with charly/credential_plugin.go + the per-candy copies the #86 batch will retire.
type credentialViaExecInput struct {
	Method  string `json:"method"`
	Service string `json:"service,omitempty"`
	Key     string `json:"key,omitempty"`
	Value   string `json:"value,omitempty"`
}

type credentialViaExecReply struct {
	Value  string `json:"value,omitempty"`
	Source string `json:"source,omitempty"`
	Error  string `json:"error,omitempty"`
}

// CredentialAccessViaExecutor bundles verb:credential's resolve+set ops into a CredentialAccess,
// driven over the given sdk.Executor's InvokeProvider (peer-dispatch to candy/plugin-secrets). Use
// it anywhere a deploy-time plugin needs to resolve candy secret_requires:/secret_accepts: or an
// enc passphrase (ResolveSecretForCandy / ResolveEncPassphrase / ResolveHookSecretEnv).
func CredentialAccessViaExecutor(ctx context.Context, ex *sdk.Executor) CredentialAccess {
	return CredentialAccess{
		Resolve: func(_ /*envVar*/, service, key, defaultVal string) (value, source string) {
			return credentialResolveViaExec(ctx, ex, service, key, defaultVal)
		},
		Write: func(service, key, value string) error {
			return credentialWriteViaExec(ctx, ex, service, key, value)
		},
	}
}

// credentialResolveViaExec resolves one credential via verb:credential. A nil exec or a transport
// error degrades to (defaultVal, "unavailable"), so the ResolveEncPassphrase / ResolveSecretForCandy
// fallback chain (env → store → generate/prompt) behaves identically regardless of the backing.
func credentialResolveViaExec(ctx context.Context, ex *sdk.Executor, service, key, defaultVal string) (value, source string) {
	if ex == nil {
		return defaultVal, "unavailable"
	}
	inJSON, err := json.Marshal(credentialViaExecInput{Method: "resolve", Service: service, Key: key})
	if err != nil {
		return defaultVal, "unavailable"
	}
	resJSON, err := ex.InvokeProvider(ctx, "verb", "credential", sdk.OpRun, inJSON, nil, sdk.InvokeProviderOpts{})
	if err != nil {
		return defaultVal, "unavailable"
	}
	var reply credentialViaExecReply
	if len(resJSON) > 0 {
		if uerr := json.Unmarshal(resJSON, &reply); uerr != nil {
			return defaultVal, "unavailable"
		}
	}
	if reply.Value != "" {
		return reply.Value, reply.Source
	}
	src := reply.Source
	if src == "" {
		src = "default"
	}
	return defaultVal, src
}

// credentialWriteViaExec persists one credential via verb:credential (the auto-generate-on-first-
// ensure path).
func credentialWriteViaExec(ctx context.Context, ex *sdk.Executor, service, key, value string) error {
	if ex == nil {
		return fmt.Errorf("credential write: no host reverse channel")
	}
	inJSON, err := json.Marshal(credentialViaExecInput{Method: "set", Service: service, Key: key, Value: value})
	if err != nil {
		return err
	}
	resJSON, err := ex.InvokeProvider(ctx, "verb", "credential", sdk.OpRun, inJSON, nil, sdk.InvokeProviderOpts{})
	if err != nil {
		return err
	}
	var reply credentialViaExecReply
	if len(resJSON) > 0 {
		if uerr := json.Unmarshal(resJSON, &reply); uerr != nil {
			return uerr
		}
	}
	if reply.Error != "" {
		return errors.New(reply.Error)
	}
	return nil
}
