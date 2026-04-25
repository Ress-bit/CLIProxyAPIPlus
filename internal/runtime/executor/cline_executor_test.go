package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestNewClineExecutor_UsesClineIdentifierAndBearerHeader(t *testing.T) {
	t.Parallel()

	e := NewClineExecutor(nil)
	if got := e.Identifier(); got != "cline" {
		t.Fatalf("Identifier() = %q, want %q", got, "cline")
	}

	auth := &cliproxyauth.Auth{Metadata: map[string]any{
		"access_token": "test-token",
	}}
	req, err := http.NewRequest(http.MethodPost, "https://example.invalid/chat/completions", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	if err := e.PrepareRequest(req, auth); err != nil {
		t.Fatalf("PrepareRequest() error = %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer workos:test-token" {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer workos:test-token")
	}
}

func TestFetchClineModels_UsesStaticFallbackModels(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ai/cline/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"free-model","name":"Free Model","context_length":1234,"max_tokens":5678,"pricing":{"prompt":"0","completion":"0"}},{"id":"paid-model","name":"Paid Model","context_length":999,"max_tokens":111,"pricing":{"prompt":"0.1","completion":"0.2"}}]}`))
	}))
	defer server.Close()

	auth := &cliproxyauth.Auth{Metadata: map[string]any{"base_url": server.URL + "/api/v1"}}
	models := FetchClineModels(context.Background(), auth, &config.Config{})
	if len(models) == 0 {
		t.Fatal("expected static fallback models")
	}
	if got := models[0].ID; got != "cline/auto" {
		t.Fatalf("first model id = %q, want cline/auto", got)
	}
}

func TestClineExecutorExecute_PostsRequestAndReturnsResponse(t *testing.T) {
	t.Parallel()

	var gotAuth string
	var gotPath string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	e := NewClineExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Metadata: map[string]any{
		"access_token": "test-token",
		"base_url":     server.URL,
	}}
	payload := []byte(`{"model":"claude-4-sonnet","messages":[{"role":"user","content":"hi"}]}`)
	resp, err := e.Execute(context.Background(), auth, cliproxyexecutor.Request{Model: "claude-4-sonnet", Payload: payload}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai"), Stream: false})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if gotAuth != "Bearer workos:test-token" {
		t.Fatalf("Authorization = %q, want %q", gotAuth, "Bearer workos:test-token")
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("path = %q, want %q", gotPath, "/chat/completions")
	}
	if len(gotBody) == 0 {
		t.Fatal("expected upstream request body")
	}
	if !gjson.GetBytes(gotBody, "include_reasoning").Bool() {
		t.Fatalf("expected include_reasoning=true in upstream body: %s", string(gotBody))
	}
	if gjson.GetBytes(gotBody, "stream_options").Exists() {
		t.Fatalf("expected stream_options removed for non-stream request: %s", string(gotBody))
	}
	if len(resp.Payload) == 0 {
		t.Fatal("expected response payload")
	}
}

func TestClineExecutorExecuteStream_StreamsSSEChunks(t *testing.T) {
	t.Parallel()

	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	e := NewClineExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Metadata: map[string]any{
		"access_token": "test-token",
		"base_url":     server.URL,
	}}
	payload := []byte(`{"model":"claude-4-sonnet","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	result, err := e.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{Model: "claude-4-sonnet", Payload: payload}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai"), Stream: true})
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}
	if result == nil {
		t.Fatal("expected stream result")
	}
	chunk, ok := <-result.Chunks
	if !ok {
		t.Fatal("expected at least one chunk")
	}
	if chunk.Err != nil {
		t.Fatalf("unexpected chunk error: %v", chunk.Err)
	}
	if len(chunk.Payload) == 0 {
		t.Fatal("expected chunk payload")
	}
	if !gjson.GetBytes(gotBody, "include_reasoning").Bool() {
		t.Fatalf("expected include_reasoning=true in streaming upstream body: %s", string(gotBody))
	}
	if !gjson.GetBytes(gotBody, "stream_options.include_usage").Bool() {
		t.Fatalf("expected stream_options.include_usage=true in streaming upstream body: %s", string(gotBody))
	}
}
