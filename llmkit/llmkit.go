// Package llmkit — the SHARED OpenAI-compatible LLM client every consumer that
// talks to an OpenAI-style endpoint uses: the generic pipeline engine's agent
// stages (candy/plugin-pipeline) and the `vision:` check verb
// (candy/plugin-vision), which validates a screenshot by sending it to the same
// endpoint.
//
// ONE home (R3 + the boundary law): the client was plugin-local in
// plugin-pipeline; it belongs in the SDK so any plugin speaks the endpoint
// identically — the same streaming/accumulator/idle-bound/vision machinery, the
// same parameter mapping, the same no-env-bleed guarantee. The CUE vocabulary it
// maps (spec.LLMSpec/spec.LLMParams) lives in the contract module.
//
// Built on the OFFICIAL github.com/openai/openai-go/v3 SDK. The SDK owns the wire
// format, the SSE decoder, the chunk accumulator, tool-call assembly and the
// typed request params; this package owns the things the SDK does not:
//
//   - the IDLE bound (a per-chunk watchdog that cancels the request) — NOT a
//     whole-generation deadline, so a slow-but-progressing model is never cut
//     off while a silent provider fails in bounded time;
//   - ollama's NON-STANDARD `reasoning` delta, which the SDK accumulator drops,
//     read per-chunk from the raw JSON so a reasoning-only 200 is reported as a
//     NAMED empty completion rather than a bare one;
//   - the empty-completion guard (a 200 with neither content nor tool calls is
//     an ERROR, never a silent empty turn);
//   - the no-env-bleed guarantee: the client is built from the given options
//     ONLY, never via openai.NewClient (which reads OPENAI_API_KEY/
//     OPENAI_BASE_URL/OPENAI_CUSTOM_HEADERS and would let a stray operator env
//     hijack a lane).
package llmkit

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
	"github.com/opencharly/spec/spec"
)

// Defaults keep a consumer runnable with no authored config — the local ollama
// server is the built-in default layer.
const (
	DefaultBaseURL     = "http://localhost:11434/v1"
	DefaultModel       = "deepseek-v4.1-flash:cloud"
	DefaultIdleTimeout = 3 * time.Minute
)

// Config is the RESOLVED endpoint + request config for one call. Build it with
// Default(), overlay authored blocks with Apply, then overlay the operator env
// with FromEnv — the same field-wise precedence the pipeline documents
// (env > step > entity > default).
type Config struct {
	BaseURL      string
	Model        string
	APIKey       string
	Organization string
	Project      string
	Timeout      time.Duration
	IdleTimeout  time.Duration
	MaxRetries   int
	Headers      map[string]string
	Params       spec.LLMParams
}

// Default returns the built-in config layer.
func Default() Config {
	return Config{
		BaseURL:     DefaultBaseURL,
		Model:       DefaultModel,
		IdleTimeout: DefaultIdleTimeout,
		MaxRetries:  2,
	}
}

// Apply overlays an authored #LLMSpec FIELD-WISE: a layer fills only what the
// higher layer left unset, and `params`/`headers`/`extra` merge rather than
// replace, so a later layer can tighten one knob without wiping its siblings.
func (c Config) Apply(s spec.LLMSpec) Config {
	if s.Base_url != "" {
		c.BaseURL = s.Base_url
	}
	if s.Model != "" {
		c.Model = s.Model
	}
	if s.Api_key != "" {
		c.APIKey = s.Api_key
	}
	if s.Organization != "" {
		c.Organization = s.Organization
	}
	if s.Project != "" {
		c.Project = s.Project
	}
	if d, ok := ParseDuration(s.Timeout); ok {
		c.Timeout = d
	}
	if d, ok := ParseDuration(s.Idle_timeout); ok {
		c.IdleTimeout = d
	}
	if s.Max_retries != nil {
		c.MaxRetries = int(*s.Max_retries)
	}
	if len(s.Headers) > 0 {
		if c.Headers == nil {
			c.Headers = map[string]string{}
		}
		for k, v := range s.Headers {
			c.Headers[k] = v
		}
	}
	c.Params = MergeParams(c.Params, s.Params)
	return c
}

// FromEnv overlays the operator env layer (the highest precedence). The names are
// the EVAL_LLM_* family the pipeline established, so one operator convention
// configures every consumer.
func (c Config) FromEnv() Config {
	if v := os.Getenv("EVAL_LLM_BASE_URL"); v != "" {
		c.BaseURL = v
	}
	if v := os.Getenv("EVAL_LLM_MODEL"); v != "" {
		c.Model = v
	}
	if v := os.Getenv("EVAL_LLM_API_KEY"); v != "" {
		c.APIKey = v
	}
	if d, ok := ParseDuration(os.Getenv("EVAL_LLM_IDLE_TIMEOUT")); ok {
		c.IdleTimeout = d
	}
	if d, ok := ParseDuration(os.Getenv("EVAL_LLM_TIMEOUT")); ok {
		c.Timeout = d
	}
	if v := os.Getenv("EVAL_LLM_MAX_RETRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			c.MaxRetries = n
		}
	}
	return c.normalize()
}

// normalize trims/clamps the resolved values so a caller can use the Config
// directly without re-checking bounds.
func (c Config) normalize() Config {
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	if c.Timeout < 0 {
		c.Timeout = 0
	}
	if c.IdleTimeout <= 0 {
		c.IdleTimeout = DefaultIdleTimeout
	}
	return c
}

// Normalize exposes the resolved-config normalization (a consumer that builds a
// Config by hand should call it before use).
func (c Config) Normalize() Config { return c.normalize() }

// MergeParams overlays `over` onto `base` FIELD-WISE (over wins per set field),
// merging the open maps. This is what makes a step-level override change one knob
// without discarding the entity's other parameters.
func MergeParams(base, over spec.LLMParams) spec.LLMParams {
	if over.Temperature != nil {
		base.Temperature = over.Temperature
	}
	if over.Top_p != nil {
		base.Top_p = over.Top_p
	}
	if over.Max_tokens != nil {
		base.Max_tokens = over.Max_tokens
	}
	if over.Max_completion_tokens != nil {
		base.Max_completion_tokens = over.Max_completion_tokens
	}
	if over.Frequency_penalty != nil {
		base.Frequency_penalty = over.Frequency_penalty
	}
	if over.Presence_penalty != nil {
		base.Presence_penalty = over.Presence_penalty
	}
	if over.Seed != nil {
		base.Seed = over.Seed
	}
	if over.Stop != nil {
		base.Stop = over.Stop
	}
	if over.Reasoning_effort != "" {
		base.Reasoning_effort = over.Reasoning_effort
	}
	if over.Reasoning.Effort != "" {
		base.Reasoning.Effort = over.Reasoning.Effort
	}
	if over.Response_format.Type != "" {
		base.Response_format = over.Response_format
	}
	if over.Stream_options.Include_usage != nil {
		base.Stream_options.Include_usage = over.Stream_options.Include_usage
	}
	if over.Parallel_tool_calls != nil {
		base.Parallel_tool_calls = over.Parallel_tool_calls
	}
	if over.Tool_choice != nil {
		base.Tool_choice = over.Tool_choice
	}
	if over.Logprobs != nil {
		base.Logprobs = over.Logprobs
	}
	if over.Top_logprobs != nil {
		base.Top_logprobs = over.Top_logprobs
	}
	if over.User != "" {
		base.User = over.User
	}
	if len(over.Metadata) > 0 {
		if base.Metadata == nil {
			base.Metadata = map[string]string{}
		}
		for k, v := range over.Metadata {
			base.Metadata[k] = v
		}
	}
	if len(over.Logit_bias) > 0 {
		if base.Logit_bias == nil {
			base.Logit_bias = map[string]int64{}
		}
		for k, v := range over.Logit_bias {
			base.Logit_bias[k] = v
		}
	}
	if len(over.Extra) > 0 {
		if base.Extra == nil {
			base.Extra = map[string]any{}
		}
		for k, v := range over.Extra {
			base.Extra[k] = v
		}
	}
	return base
}

// ParseDuration parses a non-empty, positive Go duration; false otherwise.
func ParseDuration(s string) (time.Duration, bool) {
	if s == "" {
		return 0, false
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
}

// ── the wire message model ───────────────────────────────────────────────────

// Message is the caller's transcript row. It is deliberately NOT a wire type (the
// SDK owns the wire): Content is a *string so "no content" (a pure tool-call
// turn) is distinguishable from "".
type Message struct {
	Role       string
	Content    *string
	ToolCalls  []ToolCall
	ToolCallID string
}

// ToolCall is one assembled tool call.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// Strptr is the ergonomic constructor for a Message content pointer.
func Strptr(s string) *string { return &s }

// Text builds a user text message.
func Text(s string) openai.ChatCompletionMessageParamUnion { return openai.UserMessage(s) }

// System builds a system message.
func System(s string) openai.ChatCompletionMessageParamUnion { return openai.SystemMessage(s) }

// ToSDKMessages converts a caller transcript to the SDK message union.
func ToSDKMessages(msgs []Message) []openai.ChatCompletionMessageParamUnion {
	out := make([]openai.ChatCompletionMessageParamUnion, 0, len(msgs))
	for _, m := range msgs {
		content := ""
		if m.Content != nil {
			content = *m.Content
		}
		switch m.Role {
		case "system":
			out = append(out, openai.SystemMessage(content))
		case "assistant":
			if len(m.ToolCalls) > 0 {
				ap := openai.ChatCompletionAssistantMessageParam{
					ToolCalls: ToSDKToolCalls(m.ToolCalls),
				}
				if content != "" {
					ap.Content = openai.ChatCompletionAssistantMessageParamContentUnion{OfString: openai.String(content)}
				}
				out = append(out, openai.ChatCompletionMessageParamUnion{OfAssistant: &ap})
				continue
			}
			out = append(out, openai.AssistantMessage(content))
		case "tool":
			out = append(out, openai.ToolMessage(content, m.ToolCallID))
		default:
			out = append(out, openai.UserMessage(content))
		}
	}
	return out
}

// ToSDKToolCalls converts caller tool-call rows to the SDK param union.
func ToSDKToolCalls(calls []ToolCall) []openai.ChatCompletionMessageToolCallUnionParam {
	out := make([]openai.ChatCompletionMessageToolCallUnionParam, 0, len(calls))
	for _, c := range calls {
		out = append(out, openai.ChatCompletionMessageToolCallUnionParam{
			OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
				ID: c.ID,
				Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
					Name:      c.Name,
					Arguments: c.Arguments,
				},
			},
		})
	}
	return out
}

// FromSDKTools converts assembled SDK tool calls into caller rows.
func FromSDKTools(calls []openai.ChatCompletionMessageToolCallUnion) []ToolCall {
	out := make([]ToolCall, 0, len(calls))
	for _, c := range calls {
		out = append(out, ToolCall{ID: c.ID, Name: c.Function.Name, Arguments: c.Function.Arguments})
	}
	return out
}

// FunctionTool renders one function tool from a name/description/JSON-schema
// parameters object. An empty parameters map yields a tool with no argument
// schema.
func FunctionTool(name, description string, parameters map[string]any) openai.ChatCompletionToolUnionParam {
	def := shared.FunctionDefinitionParam{Name: name}
	if description != "" {
		def.Description = openai.String(description)
	}
	if len(parameters) > 0 {
		def.Parameters = shared.FunctionParameters(parameters)
	}
	return openai.ChatCompletionFunctionTool(def)
}

// ── client construction ──────────────────────────────────────────────────────

// clientOptions builds the SDK request options. It uses ONLY these options — the
// client is not openai.NewClient, so a stray operator OPENAI_API_KEY /
// OPENAI_BASE_URL / OPENAI_CUSTOM_HEADERS can never hijack a call.
func (c Config) clientOptions() []option.RequestOption {
	opts := []option.RequestOption{
		option.WithBaseURL(c.BaseURL),
		option.WithMaxRetries(c.MaxRetries),
	}
	// An empty key means ABSENT — no Authorization header at all (the keyless
	// local ollama needs none).
	if c.APIKey != "" {
		opts = append(opts, option.WithAPIKey(c.APIKey))
	}
	if c.Organization != "" {
		opts = append(opts, option.WithOrganization(c.Organization))
	}
	if c.Project != "" {
		opts = append(opts, option.WithProject(c.Project))
	}
	if c.Timeout > 0 {
		opts = append(opts, option.WithRequestTimeout(c.Timeout))
	}
	// deterministic header order (a map walk is not)
	keys := make([]string, 0, len(c.Headers))
	for k := range c.Headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		opts = append(opts, option.WithHeader(k, c.Headers[k]))
	}
	return opts
}

// extraOptions renders the `extra` escape hatch as sjson-set request options —
// the ONE legal place for an undocumented key.
func (c Config) extraOptions() []option.RequestOption {
	if len(c.Params.Extra) == 0 {
		return nil
	}
	keys := make([]string, 0, len(c.Params.Extra))
	for k := range c.Params.Extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	opts := make([]option.RequestOption, 0, len(keys))
	for _, k := range keys {
		opts = append(opts, option.WithJSONSet(k, c.Params.Extra[k]))
	}
	return opts
}

// buildParams maps the authored #LLMParams onto the SDK request. EVERY field is
// applied only when set, so an omitted knob is not sent at all.
func (c Config) buildParams(msgs []openai.ChatCompletionMessageParamUnion, tools []openai.ChatCompletionToolUnionParam) openai.ChatCompletionNewParams {
	p := openai.ChatCompletionNewParams{
		Model:    c.Model,
		Messages: msgs,
	}
	if len(tools) > 0 {
		p.Tools = tools
	}
	if v := c.Params.Temperature; v != nil {
		p.Temperature = openai.Float(*v)
	}
	if v := c.Params.Top_p; v != nil {
		p.TopP = openai.Float(*v)
	}
	if v := c.Params.Max_tokens; v != nil {
		p.MaxTokens = openai.Int(*v)
	}
	if v := c.Params.Max_completion_tokens; v != nil {
		p.MaxCompletionTokens = openai.Int(*v)
	}
	if v := c.Params.Frequency_penalty; v != nil {
		p.FrequencyPenalty = openai.Float(*v)
	}
	if v := c.Params.Presence_penalty; v != nil {
		p.PresencePenalty = openai.Float(*v)
	}
	if v := c.Params.Seed; v != nil {
		p.Seed = openai.Int(*v)
	}
	if s := c.Params.Stop; s != nil {
		p.Stop = stopUnion(s)
	}
	if v := c.Params.Reasoning_effort; v != "" {
		p.ReasoningEffort = shared.ReasoningEffort(v)
	}
	if c.Params.Reasoning.Effort != "" {
		p.ReasoningEffort = shared.ReasoningEffort(c.Params.Reasoning.Effort)
	}
	if t := c.Params.Response_format.Type; t != "" {
		p.ResponseFormat = responseFormatUnion(c.Params.Response_format)
	}
	if v := c.Params.Stream_options.Include_usage; v != nil {
		p.StreamOptions.IncludeUsage = openai.Bool(*v)
	}
	if v := c.Params.Parallel_tool_calls; v != nil {
		p.ParallelToolCalls = openai.Bool(*v)
	}
	if tc := c.Params.Tool_choice; tc != nil {
		p.ToolChoice = toolChoiceUnion(tc)
	}
	if v := c.Params.Logprobs; v != nil {
		p.Logprobs = openai.Bool(*v)
	}
	if v := c.Params.Top_logprobs; v != nil {
		p.TopLogprobs = openai.Int(*v)
	}
	if c.Params.User != "" {
		p.User = openai.String(c.Params.User)
	}
	if len(c.Params.Metadata) > 0 {
		md := shared.Metadata{}
		for k, v := range c.Params.Metadata {
			md[k] = v
		}
		p.Metadata = md
	}
	if len(c.Params.Logit_bias) > 0 {
		p.LogitBias = c.Params.Logit_bias
	}
	return p
}

// stopUnion renders #LLMParams.stop (`string | [...string]`, generated as `any`).
func stopUnion(v any) openai.ChatCompletionNewParamsStopUnion {
	switch t := v.(type) {
	case string:
		return openai.ChatCompletionNewParamsStopUnion{OfString: openai.String(t)}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return openai.ChatCompletionNewParamsStopUnion{OfStringArray: out}
	case []string:
		return openai.ChatCompletionNewParamsStopUnion{OfStringArray: t}
	}
	return openai.ChatCompletionNewParamsStopUnion{}
}

// responseFormatUnion maps the authored #LLMResponseFormat onto the SDK union.
func responseFormatUnion(rf spec.LLMResponseFormat) openai.ChatCompletionNewParamsResponseFormatUnion {
	switch rf.Type {
	case "json_object":
		return openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
		}
	case "json_schema":
		js := rf.Json_schema
		param := shared.ResponseFormatJSONSchemaParam{
			JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{
				Name:   js.Name,
				Schema: js.Schema,
			},
		}
		if js.Description != "" {
			param.JSONSchema.Description = openai.String(js.Description)
		}
		if js.Strict {
			param.JSONSchema.Strict = openai.Bool(true)
		}
		return openai.ChatCompletionNewParamsResponseFormatUnion{OfJSONSchema: &param}
	}
	// "text" (and the zero value): no constraint is sent.
	return openai.ChatCompletionNewParamsResponseFormatUnion{}
}

// toolChoiceUnion maps the authored tool_choice onto the SDK union.
func toolChoiceUnion(v any) openai.ChatCompletionToolChoiceOptionUnionParam {
	if s, ok := v.(string); ok {
		switch s {
		case "none", "required", "auto":
			return openai.ChatCompletionToolChoiceOptionUnionParam{OfAuto: openai.String(s)}
		}
		return openai.ChatCompletionToolChoiceOptionUnionParam{}
	}
	if m, ok := v.(map[string]any); ok {
		if fn, ok := m["function"].(map[string]any); ok {
			if name, ok := fn["name"].(string); ok && name != "" {
				return openai.ChatCompletionToolChoiceOptionUnionParam{
					OfFunctionToolChoice: &openai.ChatCompletionNamedToolChoiceParam{
						Function: openai.ChatCompletionNamedToolChoiceFunctionParam{Name: name},
					},
				}
			}
		}
	}
	return openai.ChatCompletionToolChoiceOptionUnionParam{}
}

// ── the streaming call ───────────────────────────────────────────────────────

// Chat issues ONE streaming chat-completions call and returns the assembled
// assistant turn.
//
// IDLE BOUND, NOT A WHOLE-GENERATION DEADLINE. Streaming is load-bearing: the
// request rides a cancellable child of the caller's ctx and a watchdog CANCELS
// that child when no chunk arrives for IdleTimeout. Cancelling the ctx makes the
// transport tear the connection down, which unblocks the in-flight body read with
// an error — so a silent provider fails in bounded time while a
// slow-but-progressing one is never cut off. (resp.Body.Close() does NOT work
// here: net/http holds a mutex across the blocking read, so a Close from the
// watchdog merely queues behind it.)
//
// An empty completion (no content, no tool calls) is an ERROR, never a silent
// empty turn — and the diagnostic NAMES ollama's non-standard `reasoning` field
// when the model produced ONLY thinking, which is the real-world shape of this
// failure.
func Chat(ctx context.Context, cfg Config, msgs []openai.ChatCompletionMessageParamUnion, tools []openai.ChatCompletionToolUnionParam) (Message, error) {
	cfg = cfg.normalize()

	idle := cfg.IdleTimeout
	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()
	var idleTripped atomic.Bool
	watchdog := time.AfterFunc(idle, func() {
		idleTripped.Store(true)
		cancelRead()
	})
	defer watchdog.Stop()

	svc := openai.NewChatCompletionService(cfg.clientOptions()...)
	stream := svc.NewStreaming(readCtx, cfg.buildParams(msgs, tools), cfg.extraOptions()...)
	defer stream.Close()

	var acc openai.ChatCompletionAccumulator
	var sawChunk bool
	// reasoning accumulates ollama's NON-STANDARD `reasoning` delta field. The SDK
	// accumulator has no typed slot for it (it is not part of OpenAI's schema), so
	// it is read per-delta from the raw JSON. NOTE: an UNKNOWN field is recorded
	// with Valid()==false even when present, so presence is tested via a non-empty
	// Raw() — NOT Valid().
	var reasoningSB strings.Builder
	for stream.Next() {
		watchdog.Reset(idle) // progress
		sawChunk = true
		chunk := stream.Current()
		for _, ch := range chunk.Choices {
			if f, ok := ch.Delta.JSON.ExtraFields["reasoning"]; ok {
				if raw := f.Raw(); raw != "" && raw != "null" {
					var s string
					if json.Unmarshal([]byte(raw), &s) == nil {
						reasoningSB.WriteString(s)
					}
				}
			}
		}
		if !acc.AddChunk(chunk) {
			return Message{}, fmt.Errorf("LLM: stream chunk rejected by the accumulator (malformed tool-call/content index sequence)")
		}
	}
	if idleTripped.Load() {
		content := 0
		if len(acc.Choices) > 0 {
			content = len(acc.Choices[0].Message.Content)
		}
		return Message{}, fmt.Errorf("LLM stream stalled: no chunk for %s after %d byte(s) of content, %d byte(s) of reasoning and %d tool call(s) (provider stopped streaming); raise the llm idle_timeout (or EVAL_LLM_IDLE_TIMEOUT) for an unusually slow endpoint",
			idle, content, reasoningSB.Len(), len(acc.Choices))
	}
	if err := stream.Err(); err != nil {
		return Message{}, fmt.Errorf("LLM: %w", err)
	}
	if !sawChunk {
		return Message{}, fmt.Errorf("LLM: empty completion (the provider returned no stream chunks)")
	}

	out := Message{Role: "assistant"}
	if len(acc.Choices) > 0 {
		m := acc.Choices[0].Message
		if m.Content != "" {
			s := m.Content
			out.Content = &s
		}
		out.ToolCalls = FromSDKTools(m.ToolCalls)
	}
	reasoning := reasoningSB.String()
	if out.Content == nil && len(out.ToolCalls) == 0 {
		if reasoning != "" {
			return Message{}, fmt.Errorf("LLM: empty completion (no content, no tool calls — the model emitted only %d byte(s) of reasoning; set reasoning_effort: none or raise max_tokens)", len(reasoning))
		}
		return Message{}, fmt.Errorf("LLM: empty completion (no content, no tool calls)")
	}
	return out, nil
}

// ── vision ───────────────────────────────────────────────────────────────────

// ImageDataURL renders raw image bytes as an OpenAI image_url data URL. Ollama's
// OpenAI layer accepts a base64 data URL ONLY (remote image URLs are explicitly
// unsupported), so this is the canonical way to hand an image to the endpoint.
func ImageDataURL(mime string, data []byte) string {
	if mime == "" {
		mime = "image/png"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// VisionParts builds a multimodal user message: the prompt text plus one image
// per URL (a data URL from ImageDataURL, or a remote URL which a full OpenAI
// endpoint accepts even though ollama's compat layer does not).
func VisionParts(prompt string, images []string) []openai.ChatCompletionContentPartUnionParam {
	parts := []openai.ChatCompletionContentPartUnionParam{openai.TextContentPart(prompt)}
	for _, img := range images {
		if img == "" {
			continue
		}
		parts = append(parts, openai.ImageContentPart(
			openai.ChatCompletionContentPartImageImageURLParam{URL: img},
		))
	}
	return parts
}

// ChatVision sends one multimodal completion: `prompt` plus the given images
// (base64 data URLs), returning the assistant's text. This is the primitive the
// `vision:` check verb drives — the same idle bound, accumulator and
// empty-completion guard as Chat.
//
// A successful Chat with no tools always carries content (the empty-completion
// guard rejects the alternative), so the deref below cannot silently turn a
// failure into "".
func ChatVision(ctx context.Context, cfg Config, prompt string, images []string) (string, error) {
	msgs := []openai.ChatCompletionMessageParamUnion{openai.UserMessage(VisionParts(prompt, images))}
	msg, err := Chat(ctx, cfg, msgs, nil)
	if err != nil {
		return "", err
	}
	if msg.Content == nil {
		return "", fmt.Errorf("llmkit: vision completion returned no content (the empty-completion guard should have caught this)")
	}
	return *msg.Content, nil
}
