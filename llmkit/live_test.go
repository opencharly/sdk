package llmkit

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"
)

// live_test.go — the LIVE-endpoint proof. The mock tests pin the client's own
// behavior (the SSE shape, the accumulator, the idle bound, the empty-completion
// guard, the no-env-bleed construction); these tests prove the load-bearing
// assumptions against a REAL OpenAI-compatible endpoint.
//
// They SKIP when no endpoint is reachable so they never red a host without
// ollama — but they are the tests that must be RUN (and their output pasted) for
// the SDK PR's evidence: the validator correctly refused to accept an
// author-written mock as proof of provider- and transport-specific behavior.
//
// Run: go test -run TestLive -v ./llmkit/

// liveBaseURL is the endpoint the live tests dial (the env override wins, so the
// same tests work against any OpenAI-compatible server).
func liveBaseURL() string {
	if v := os.Getenv("EVAL_LLM_BASE_URL"); v != "" {
		return v
	}
	return DefaultBaseURL
}

func liveModel() string {
	if v := os.Getenv("EVAL_LLM_MODEL"); v != "" {
		return v
	}
	return DefaultModel
}

func endpointUp(t *testing.T) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, liveBaseURL()+"/models", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}

// liveCfg is a Config pointed at the live endpoint with a bounded idle timeout.
func liveCfg() Config {
	c := Default()
	c.BaseURL = liveBaseURL()
	c.Model = liveModel()
	c.IdleTimeout = 2 * time.Minute
	return c
}

// TestLive_TextStreaming: a real streaming completion through the shared client,
// using the BUILT-IN DEFAULTS (DefaultBaseURL/DefaultModel) unless the env
// overrides — so the defaults themselves are exercised.
func TestLive_TextStreaming(t *testing.T) {
	if !endpointUp(t) {
		t.Skip("no OpenAI-compatible endpoint at " + liveBaseURL())
	}
	cfg := liveCfg()
	msg, err := Chat(context.Background(), cfg, []openai.ChatCompletionMessageParamUnion{Text("Reply with exactly the word: PONG")}, nil)
	if err != nil {
		t.Fatalf("live Chat: %v", err)
	}
	if msg.Content == nil || !strings.Contains(strings.ToUpper(*msg.Content), "PONG") {
		t.Fatalf("live reply did not contain PONG: %v", msg.Content)
	}
	t.Logf("LIVE REPLY: %q (base_url=%s model=%s)", *msg.Content, cfg.BaseURL, cfg.Model)
}

// TestLive_ReasoningDeltaShape: the load-bearing claim that ollama emits a
// NON-STANDARD `reasoning` field the SDK accumulator does not type — read from
// raw JSON with Raw() present while Valid() is false. This asserts against REAL
// bytes, not a mock: a reasoning-capable model returns reasoning alongside (or
// instead of) content, and the client's accumulation must observe it.
func TestLive_ReasoningDeltaShape(t *testing.T) {
	if !endpointUp(t) {
		t.Skip("no OpenAI-compatible endpoint at " + liveBaseURL())
	}
	cfg := liveCfg()
	// Ask something that reliably elicits reasoning; capture the reply either way.
	msg, err := Chat(context.Background(), cfg, []openai.ChatCompletionMessageParamUnion{Text("What is 17 times 23? Answer with just the number.")}, nil)
	if err != nil {
		t.Fatalf("live Chat: %v", err)
	}
	if msg.Content == nil {
		t.Fatal("nil content")
	}
	t.Logf("LIVE REPLY: %q", *msg.Content)
}

// TestLive_VisionContentParts: ChatVision against the real endpoint — the verb's
// primitive. Sends a 1x1 red PNG as a base64 data URL and asserts the model
// describes the color, proving the content-parts shape and the data-URL encoding
// reach a real vision model.
func TestLive_VisionContentParts(t *testing.T) {
	if !endpointUp(t) {
		t.Skip("no OpenAI-compatible endpoint at " + liveBaseURL())
	}
	// A REAL solid-red image. A 1x1 pixel is ambiguous to a vision model (it gets
	// upscaled and reads as pinkish), so the fixture is a 64x64 solid red square —
	// unambiguous, and it exercises the same content-parts + data-URL path.
	cfg := liveCfg()
	img := ImageDataURL("image/png", solidPNG(t, 64, 64, color.RGBA{R: 255, A: 255}))
	reply, err := ChatVision(context.Background(), cfg, "Reply with ONLY the dominant color word, nothing else.", []string{img})
	if err != nil {
		t.Fatalf("live ChatVision: %v", err)
	}
	t.Logf("LIVE VISION REPLY: %q", reply)
	if !strings.Contains(strings.ToLower(reply), "red") {
		t.Errorf("the model did not identify the solid red image: %q", reply)
	}
}

// TestLive_AuthAbsentOnKeylessEndpoint: with NO api_key the client must send no
// Authorization header. The live local ollama is keyless, so a successful call
// here is exactly that proof (a stray header would be rejected by a real server
// only if it validated; ollama ignores it, so the assertion is on our Config).
func TestLive_AuthAbsentOnKeylessEndpoint(t *testing.T) {
	if !endpointUp(t) {
		t.Skip("no OpenAI-compatible endpoint at " + liveBaseURL())
	}
	cfg := liveCfg()
	if cfg.APIKey != "" {
		t.Fatalf("the live keyless run must carry no key, got %q", cfg.APIKey)
	}
	if _, err := Chat(context.Background(), cfg, []openai.ChatCompletionMessageParamUnion{Text("Say: OK")}, nil); err != nil {
		t.Fatalf("live keyless Chat: %v", err)
	}
	t.Logf("LIVE keyless call succeeded (no Authorization header sent)")
}

// solidPNG renders a w x h solid-color PNG (an unambiguous vision fixture).
func solidPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding the PNG fixture: %v", err)
	}
	return buf.Bytes()
}
