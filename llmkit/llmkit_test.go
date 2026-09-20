package llmkit

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/opencharly/spec/spec"
)

// llmkit_test.go — the shared client's conformance suite. The mock speaks the SSE
// shape the openai-go SDK decoder expects; the captured request body is what the
// config tests assert on (the authored params are proven by what ARRIVED).

// sseChunk builds one SDK-shaped chat-completion chunk carrying a delta.
func sseChunk(delta map[string]any) map[string]any {
	return map[string]any{
		"id":      "chatcmpl-test",
		"object":  "chat.completion.chunk",
		"created": 1700000000,
		"model":   "test-model",
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}},
	}
}

func writeContent(rw http.ResponseWriter, content string) {
	rw.Header().Set("Content-Type", "text/event-stream")
	rw.WriteHeader(http.StatusOK)
	b, _ := json.Marshal(sseChunk(map[string]any{"role": "assistant", "content": content}))
	_, _ = rw.Write([]byte("data: " + string(b) + "\n\n"))
	_, _ = rw.Write([]byte("data: [DONE]\n\n"))
}

func writeReasoningOnly(rw http.ResponseWriter, reasoning string) {
	rw.Header().Set("Content-Type", "text/event-stream")
	rw.WriteHeader(http.StatusOK)
	b, _ := json.Marshal(sseChunk(map[string]any{"role": "assistant", "reasoning": reasoning}))
	_, _ = rw.Write([]byte("data: " + string(b) + "\n\n"))
	_, _ = rw.Write([]byte("data: [DONE]\n\n"))
}

func writeEmpty(rw http.ResponseWriter) {
	rw.Header().Set("Content-Type", "text/event-stream")
	rw.WriteHeader(http.StatusOK)
	b, _ := json.Marshal(sseChunk(map[string]any{"role": "assistant"}))
	_, _ = rw.Write([]byte("data: " + string(b) + "\n\n"))
	_, _ = rw.Write([]byte("data: [DONE]\n\n"))
}

// mock starts an SSE endpoint, decoding the request body for assertions.
func mock(t *testing.T, reply func(http.ResponseWriter), assert func(t *testing.T, body map[string]any)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		raw, _ := io.ReadAll(req.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("request body is not JSON: %v (%s)", err, raw)
		}
		if assert != nil {
			assert(t, body)
		}
		reply(rw)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestChat_TextStreaming: a plain streaming completion round-trips and the
// request is streaming (the idle bound depends on SSE).
func TestChat_TextStreaming(t *testing.T) {
	srv := mock(t, func(rw http.ResponseWriter) { writeContent(rw, "hello") },
		func(t *testing.T, body map[string]any) {
			if body["stream"] != true {
				t.Errorf("stream must be true, got %v", body["stream"])
			}
		})
	cfg := Default()
	cfg.BaseURL = srv.URL
	msg, err := Chat(t.Context(), cfg, []openai.ChatCompletionMessageParamUnion{Text("hi")}, nil)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if msg.Content == nil || *msg.Content != "hello" {
		t.Fatalf("content = %v, want hello", msg.Content)
	}
}

// TestChat_ParamsReachTheWire: every set #LLMParams field arrives under its exact
// wire name; an unset field is NOT sent.
func TestChat_ParamsReachTheWire(t *testing.T) {
	temp, topP := 0.3, 0.8
	maxTok, maxComp, seed, topLog := int64(1234), int64(5678), int64(42), int64(3)
	freq, pres := 0.1, 0.2
	parallel, logprobs := true, false
	cfg := Default()
	cfg.Params = spec.LLMParams{
		Temperature:           &temp,
		Top_p:                 &topP,
		Max_tokens:            &maxTok,
		Max_completion_tokens: &maxComp,
		Frequency_penalty:     &freq,
		Presence_penalty:      &pres,
		Seed:                  &seed,
		Stop:                  []any{"A", "B"},
		Reasoning_effort:      "high",
		Stream_options:        spec.LLMStreamOptions{Include_usage: boolPtr(true)},
		Parallel_tool_calls:   &parallel,
		Logprobs:              &logprobs,
		Top_logprobs:          &topLog,
		User:                  "u-1",
		Metadata:              map[string]string{"k": "v"},
		Logit_bias:            map[string]int64{"5": 1},
		Response_format:       spec.LLMResponseFormat{Type: "json_object"},
		Tool_choice:           "required",
		Extra:                 map[string]any{"custom_knob": 7},
	}
	srv := mock(t, func(rw http.ResponseWriter) { writeContent(rw, "ok") },
		func(t *testing.T, body map[string]any) {
			want := map[string]any{
				"temperature": 0.3, "top_p": 0.8,
				"max_tokens": float64(1234), "max_completion_tokens": float64(5678),
				"frequency_penalty": 0.1, "presence_penalty": 0.2,
				"seed": float64(42), "reasoning_effort": "high",
				"parallel_tool_calls": true, "logprobs": false,
				"top_logprobs": float64(3), "user": "u-1",
				"custom_knob": float64(7),
			}
			for k, v := range want {
				if body[k] != v {
					t.Errorf("%s: got %#v, want %#v", k, body[k], v)
				}
			}
			if rf, _ := body["response_format"].(map[string]any); rf["type"] != "json_object" {
				t.Errorf("response_format.type: got %v", rf)
			}
			if body["tool_choice"] != "required" {
				t.Errorf("tool_choice: got %v", body["tool_choice"])
			}
			if so, _ := body["stream_options"].(map[string]any); so["include_usage"] != true {
				t.Errorf("stream_options: got %v", body["stream_options"])
			}
			if stops, _ := body["stop"].([]any); len(stops) != 2 {
				t.Errorf("stop: got %v", body["stop"])
			}
			if md, _ := body["metadata"].(map[string]any); md["k"] != "v" {
				t.Errorf("metadata: got %v", body["metadata"])
			}
			if lb, _ := body["logit_bias"].(map[string]any); lb["5"] != float64(1) {
				t.Errorf("logit_bias: got %v", body["logit_bias"])
			}
		})
	cfg.BaseURL = srv.URL
	if _, err := Chat(t.Context(), cfg, []openai.ChatCompletionMessageParamUnion{Text("hi")}, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

// TestChat_UnsetParamsAreNotSent: an omitted knob is not sent (the server default
// applies) — the engine never injects a value the author did not ask for.
func TestChat_UnsetParamsAreNotSent(t *testing.T) {
	srv := mock(t, func(rw http.ResponseWriter) { writeContent(rw, "ok") },
		func(t *testing.T, body map[string]any) {
			for _, k := range []string{"temperature", "top_p", "max_tokens", "seed", "response_format", "user"} {
				if _, ok := body[k]; ok {
					t.Errorf("%s must NOT be sent when unset (got %v)", k, body[k])
				}
			}
		})
	cfg := Default()
	cfg.BaseURL = srv.URL
	if _, err := Chat(t.Context(), cfg, []openai.ChatCompletionMessageParamUnion{Text("hi")}, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

// TestChat_JsonSchemaResponseFormat: a json_schema response_format is rendered as
// the OpenAI json_schema object.
func TestChat_JsonSchemaResponseFormat(t *testing.T) {
	cfg := Default()
	rf := spec.LLMResponseFormat{Type: "json_schema"}
	rf.Json_schema.Name = "verdict"
	rf.Json_schema.Description = "d"
	rf.Json_schema.Schema = map[string]any{"type": "object"}
	rf.Json_schema.Strict = true
	cfg.Params = spec.LLMParams{Response_format: rf}
	srv := mock(t, func(rw http.ResponseWriter) { writeContent(rw, "{}") },
		func(t *testing.T, body map[string]any) {
			rf, _ := body["response_format"].(map[string]any)
			if rf["type"] != "json_schema" {
				t.Fatalf("type = %v", rf["type"])
			}
			js, _ := rf["json_schema"].(map[string]any)
			if js["name"] != "verdict" || js["strict"] != true {
				t.Errorf("json_schema block = %v", js)
			}
		})
	cfg.BaseURL = srv.URL
	if _, err := Chat(t.Context(), cfg, []openai.ChatCompletionMessageParamUnion{Text("hi")}, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

// TestChat_VisionContentParts: a vision message arrives as a content-parts array
// with an image_url data URL.
func TestChat_VisionContentParts(t *testing.T) {
	srv := mock(t, func(rw http.ResponseWriter) { writeContent(rw, "seen") },
		func(t *testing.T, body map[string]any) {
			msgs, _ := body["messages"].([]any)
			if len(msgs) == 0 {
				t.Fatal("no messages")
			}
			user, _ := msgs[0].(map[string]any)
			parts, ok := user["content"].([]any)
			if !ok {
				t.Fatalf("content must be a parts array, got %T", user["content"])
			}
			var sawText, sawImage bool
			for _, p := range parts {
				pm, _ := p.(map[string]any)
				switch pm["type"] {
				case "text":
					sawText = true
				case "image_url":
					sawImage = true
					iu, _ := pm["image_url"].(map[string]any)
					if url, _ := iu["url"].(string); !strings.HasPrefix(url, "data:image/png;base64,") {
						t.Errorf("image must be a base64 data URL, got %q", url)
					}
				}
			}
			if !sawText || !sawImage {
				t.Errorf("parts must carry text AND image_url (text=%v image=%v)", sawText, sawImage)
			}
		})
	cfg := Default()
	cfg.BaseURL = srv.URL
	img := ImageDataURL("image/png", []byte("PNGDATA"))
	got, err := ChatVision(t.Context(), cfg, "what is this?", []string{img})
	if err != nil {
		t.Fatalf("ChatVision: %v", err)
	}
	if got != "seen" {
		t.Fatalf("got %q", got)
	}
}

// TestImageDataURL: the encoder renders `data:<mime>;base64,<...>` and defaults
// the mime to image/png.
func TestImageDataURL(t *testing.T) {
	if got := ImageDataURL("", []byte{0x00}); !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Errorf("empty mime must default to image/png, got %q", got)
	}
	if got := ImageDataURL("image/jpeg", []byte{0xff}); !strings.HasPrefix(got, "data:image/jpeg;base64,") {
		t.Errorf("mime not honoured: %q", got)
	}
}

// TestChat_ReasoningOnlyIsNamedEmpty: a 200 whose only delta is ollama's
// non-standard `reasoning` field fails with the NAMED reasoning diagnostic.
func TestChat_ReasoningOnlyIsNamedEmpty(t *testing.T) {
	srv := mock(t, func(rw http.ResponseWriter) { writeReasoningOnly(rw, strings.Repeat("think ", 20)) }, nil)
	cfg := Default()
	cfg.BaseURL = srv.URL
	_, err := Chat(t.Context(), cfg, []openai.ChatCompletionMessageParamUnion{Text("hi")}, nil)
	if err == nil {
		t.Fatal("a reasoning-only 200 must fail")
	}
	if !strings.Contains(err.Error(), "reasoning") || !strings.Contains(err.Error(), "only") {
		t.Fatalf("the diagnostic must NAME the reasoning output, got: %v", err)
	}
}

// TestChat_EmptyCompletionErrors: a 200 with neither content nor tool calls fails.
func TestChat_EmptyCompletionErrors(t *testing.T) {
	srv := mock(t, func(rw http.ResponseWriter) { writeEmpty(rw) }, nil)
	cfg := Default()
	cfg.BaseURL = srv.URL
	_, err := Chat(t.Context(), cfg, []openai.ChatCompletionMessageParamUnion{Text("hi")}, nil)
	if err == nil || !strings.Contains(err.Error(), "empty completion") {
		t.Fatalf("want the empty-completion error, got: %v", err)
	}
}

// TestChat_IdleWatchdogBoundsAStall: a provider that streams NO chunks past
// IdleTimeout fails in bounded time with the stall error. Mutation-verified: an
// inert watchdog makes this HANG.
func TestChat_IdleWatchdogBoundsAStall(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "text/event-stream")
		rw.WriteHeader(http.StatusOK)
		if f, ok := rw.(http.Flusher); ok {
			f.Flush()
		}
		<-release
	}))
	defer srv.Close()
	defer close(release)
	cfg := Default()
	cfg.BaseURL = srv.URL
	cfg.IdleTimeout = 150 * time.Millisecond
	start := time.Now()
	_, err := Chat(t.Context(), cfg, []openai.ChatCompletionMessageParamUnion{Text("hi")}, nil)
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "stream stalled") {
		t.Fatalf("want the stall error, got: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("the idle watchdog did not bound the stall: %s", elapsed)
	}
}

// TestChat_NoOpenAIEnvBleed: a stray operator OPENAI_API_KEY / OPENAI_BASE_URL
// must NOT hijack a call. The client is NewChatCompletionService (given options
// only), never NewClient (which prepends the OPENAI_*-reading defaults).
func TestChat_NoOpenAIEnvBleed(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "polluted-key")
	t.Setenv("OPENAI_BASE_URL", "http://polluted.invalid/v1")
	var gotAuth, gotHost string
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		gotAuth = req.Header.Get("Authorization")
		gotHost = req.Host
		writeContent(rw, "clean")
	}))
	defer srv.Close()
	cfg := Default()
	cfg.BaseURL = srv.URL // no api_key -> NO Authorization header
	msg, err := Chat(t.Context(), cfg, []openai.ChatCompletionMessageParamUnion{Text("hi")}, nil)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if msg.Content == nil || *msg.Content != "clean" {
		t.Fatalf("content = %v", msg.Content)
	}
	if gotAuth != "" {
		t.Errorf("OPENAI_API_KEY leaked: Authorization=%q", gotAuth)
	}
	if strings.Contains(gotHost, "polluted") {
		t.Errorf("OPENAI_BASE_URL leaked: host=%q", gotHost)
	}
}

// TestConfig_PrecedenceAndMerge: Apply is field-wise and params MERGE; FromEnv
// wins over both.
func TestConfig_PrecedenceAndMerge(t *testing.T) {
	temp, topP, stageTemp := 0.9, 0.5, 0.1
	cfg := Default().Apply(spec.LLMSpec{Model: "entity-model", Params: spec.LLMParams{Temperature: &temp, Top_p: &topP}})
	cfg2 := cfg.Apply(spec.LLMSpec{Model: "stage-model", Params: spec.LLMParams{Temperature: &stageTemp}})
	if cfg2.Model != "stage-model" {
		t.Errorf("model: got %q", cfg2.Model)
	}
	if cfg2.Params.Temperature == nil || *cfg2.Params.Temperature != 0.1 {
		t.Errorf("temperature: got %v", cfg2.Params.Temperature)
	}
	if cfg2.Params.Top_p == nil || *cfg2.Params.Top_p != 0.5 {
		t.Errorf("top_p must survive a temperature-only override, got %v", cfg2.Params.Top_p)
	}
	t.Setenv("EVAL_LLM_MODEL", "env-model")
	if got := cfg2.FromEnv().Model; got != "env-model" {
		t.Errorf("env must win, got %q", got)
	}
}

// TestConfig_AuthAbsentWhenKeyEmpty: `auth=false` when no key is resolved.
func TestConfig_AuthAbsentWhenKeyEmpty(t *testing.T) {
	if got := Default().APIKey; got != "" {
		t.Fatalf("the default must have NO key, got %q", got)
	}
}

func boolPtr(b bool) *bool { return &b }

// TestChat_SurfacesFinishReasonAndUsage: the accumulator's stop reason and the
// provider's token accounting are SURFACED on Message, not dropped. This is the
// RCA-critical data — "finish_reason: length" (truncated) vs "stop", and the
// reasoning-token count that makes an unbounded generation measurable rather
// than inferred. Without it a caller cannot tell WHY a turn was slow or empty.
func TestChat_SurfacesFinishReasonAndUsage(t *testing.T) {
	srv := mock(t, func(rw http.ResponseWriter) {
		rw.Header().Set("Content-Type", "text/event-stream")
		rw.WriteHeader(http.StatusOK)
		// A content delta, a finish_reason on the choice, and a usage chunk.
		c1 := sseChunk(map[string]any{"role": "assistant", "content": "hi"})
		c1["choices"] = []any{map[string]any{"index": 0, "delta": map[string]any{"content": "hi"}, "finish_reason": "length"}}
		b1, _ := json.Marshal(c1)
		_, _ = rw.Write([]byte("data: " + string(b1) + "\n\n"))
		usage := map[string]any{
			"id": "chatcmpl-test", "object": "chat.completion.chunk", "created": 1700000000, "model": "m",
			"choices": []any{},
			"usage": map[string]any{
				"prompt_tokens": 123, "completion_tokens": 456, "total_tokens": 579,
				"completion_tokens_details": map[string]any{"reasoning_tokens": 400},
			},
		}
		b2, _ := json.Marshal(usage)
		_, _ = rw.Write([]byte("data: " + string(b2) + "\n\n"))
		_, _ = rw.Write([]byte("data: [DONE]\n\n"))
	}, nil)
	cfg := Default()
	cfg.BaseURL = srv.URL
	msg, err := Chat(t.Context(), cfg, []openai.ChatCompletionMessageParamUnion{Text("hi")}, nil)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if msg.FinishReason != "length" {
		t.Fatalf("FinishReason = %q, want length", msg.FinishReason)
	}
	if msg.Usage == nil {
		t.Fatal("Usage must be surfaced when the provider reports it")
	}
	if msg.Usage.PromptTokens != 123 || msg.Usage.CompletionTokens != 456 || msg.Usage.TotalTokens != 579 || msg.Usage.ReasoningTokens != 400 {
		t.Fatalf("usage = %+v, want prompt=123 completion=456 total=579 reasoning=400", *msg.Usage)
	}
}

// TestChat_NoUsageReportedLeavesItNil: a provider that does not honour
// include_usage must leave Usage nil (not a zero struct) so a caller can tell
// "reported zero" from "not reported".
func TestChat_NoUsageReportedLeavesItNil(t *testing.T) {
	srv := mock(t, func(rw http.ResponseWriter) { writeContent(rw, "hello") }, nil)
	cfg := Default()
	cfg.BaseURL = srv.URL
	msg, err := Chat(t.Context(), cfg, []openai.ChatCompletionMessageParamUnion{Text("hi")}, nil)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if msg.Usage != nil {
		t.Fatalf("Usage = %+v, want nil when unreported", *msg.Usage)
	}
}

// TestChat_EmptyCompletionIsTypedAndNamesFinishReason: an empty completion is a
// DETERMINISTIC, typed failure carrying the provider's stop reason and the
// reasoning byte count — so a caller can fail hard instead of blind-retrying a
// doomed `finish_reason: length` request. A reasoning-only turn is the
// measured real-world shape of this failure.
func TestChat_EmptyCompletionIsTypedAndNamesFinishReason(t *testing.T) {
	srv := mock(t, func(rw http.ResponseWriter) {
		rw.Header().Set("Content-Type", "text/event-stream")
		rw.WriteHeader(http.StatusOK)
		// reasoning-only delta, finish_reason=length, no content.
		c := sseChunk(map[string]any{"role": "assistant", "reasoning": "thinking hard"})
		c["choices"] = []any{map[string]any{"index": 0, "delta": map[string]any{"reasoning": "thinking hard"}, "finish_reason": "length"}}
		b, _ := json.Marshal(c)
		_, _ = rw.Write([]byte("data: " + string(b) + "\n\n"))
		_, _ = rw.Write([]byte("data: [DONE]\n\n"))
	}, nil)
	cfg := Default()
	cfg.BaseURL = srv.URL
	_, err := Chat(t.Context(), cfg, []openai.ChatCompletionMessageParamUnion{Text("hi")}, nil)
	if err == nil {
		t.Fatal("an empty completion must be an error")
	}
	var ece *EmptyCompletionError
	if !errors.As(err, &ece) {
		t.Fatalf("error must be *EmptyCompletionError, got %T: %v", err, err)
	}
	if ece.FinishReason != "length" {
		t.Fatalf("FinishReason = %q, want length", ece.FinishReason)
	}
	if ece.ReasoningBytes == 0 {
		t.Fatal("ReasoningBytes must be surfaced so the RCA can name the cause")
	}
	if !strings.Contains(err.Error(), "budget") {
		t.Fatalf("a length-truncated empty completion must say the budget ran out: %v", err)
	}
}

// TestDefaultEnablesUsage: the built-in layer requests token accounting so the
// prompt/completion/reasoning split is available on every call.
func TestDefaultEnablesUsage(t *testing.T) {
	d := Default()
	if d.Params.Stream_options.Include_usage == nil || !*d.Params.Stream_options.Include_usage {
		t.Fatal("Default() must request stream_options.include_usage")
	}
}
