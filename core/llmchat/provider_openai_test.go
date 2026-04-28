package llmchat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOpenAIProvider_StreamsTextAndOmitsToolFields(t *testing.T) {
	reqBodies := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path=%s want /v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization=%q want bearer", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		reqBodies <- body
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w,
			ssePayload(`{"choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"}}]}`)+
				ssePayload(`{"choices":[{"index":0,"delta":{"content":" world"},"finish_reason":"stop"}]}`),
		)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(ProviderConfig{
		BaseURL:             server.URL + "/v1",
		Model:               "qwen-info",
		APIKey:              "test-key",
		ResponseTemperature: 0.42,
		ResponseTopP:        0.77,
	})
	ch, err := provider.Complete(context.Background(), CompleteRequest{Messages: []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hi"},
	}}, SamplingModeResponse)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	var text string
	var sawDone bool
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		text += chunk.Delta
		if chunk.Done {
			sawDone = true
		}
	}
	if text != "Hello world" || !sawDone {
		t.Fatalf("text=%q sawDone=%v, want Hello world + done", text, sawDone)
	}

	body := <-reqBodies
	for _, forbidden := range []string{"tools", "tool_choice", "tool_" + "call_id"} {
		if _, ok := body[forbidden]; ok {
			t.Fatalf("request body contains retired %q field: %+v", forbidden, body)
		}
	}
	if body["model"] != "qwen-info" || body["stream"] != true || body["temperature"] != 0.42 || body["top_p"] != 0.77 {
		t.Fatalf("body = %+v, want model/stream/response sampling", body)
	}
	msgs, ok := body["messages"].([]any)
	if !ok || len(msgs) != 2 {
		t.Fatalf("messages = %#v, want 2", body["messages"])
	}
}

func TestOpenAIProvider_IgnoresUnexpectedActionDeltasFromMisconfiguredBackend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		payload := `{"choices":[{"index":0,"delta":{"` + "tool_" + `calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"cordum_list_jobs","arguments":"{}"}}]}}]}`
		_, _ = io.WriteString(w,
			ssePayload(payload)+
				ssePayload(`{"choices":[{"index":0,"delta":{"content":"Use the Jobs page instead."},"finish_reason":"stop"}]}`),
		)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(ProviderConfig{BaseURL: server.URL, Model: "qwen-info"})
	ch, err := provider.Complete(context.Background(), CompleteRequest{}, SamplingModeResponse)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	var text string
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		text += chunk.Delta
	}
	if strings.Contains(text, "cordum_list_jobs") || text != "Use the Jobs page instead." {
		t.Fatalf("text=%q, want only assistant content and no action leakage", text)
	}
}

func TestOpenAIProvider_NonRetryable4xxReturnsBodyPreview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad model", http.StatusBadRequest)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(ProviderConfig{BaseURL: server.URL, Model: "bad"})
	_, err := provider.Complete(context.Background(), CompleteRequest{}, SamplingModeResponse)
	if err == nil || !strings.Contains(err.Error(), "non-retryable status 400") || !strings.Contains(err.Error(), "bad model") {
		t.Fatalf("err=%v, want non-retryable 400 with preview", err)
	}
}

func TestOpenAIProvider_Retries5xxThenSucceeds(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) < 3 {
			http.Error(w, "warming", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ssePayload(`{"choices":[{"index":0,"delta":{"content":"ready"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	provider := NewOpenAIProvider(ProviderConfig{BaseURL: server.URL, Model: "qwen-info"})
	ch, err := provider.Complete(context.Background(), CompleteRequest{}, SamplingModeResponse)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	var text string
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		text += chunk.Delta
	}
	if text != "ready" || atomic.LoadInt32(&attempts) != 3 {
		t.Fatalf("text=%q attempts=%d, want ready after 3 attempts", text, attempts)
	}
}

func TestOpenAIProvider_PrematureEOFReturnsChunkError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ssePayload(`{"choices":[{"index":0,"delta":{"content":"partial"}}]}`))
	}))
	defer server.Close()

	provider := NewOpenAIProvider(ProviderConfig{BaseURL: server.URL, Model: "qwen-info"})
	ch, err := provider.Complete(context.Background(), CompleteRequest{}, SamplingModeResponse)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	var sawPremature bool
	for chunk := range ch {
		if chunk.Err == ErrPrematureStreamEnd {
			sawPremature = true
		}
	}
	if !sawPremature {
		t.Fatal("expected ErrPrematureStreamEnd terminal chunk")
	}
}

func TestOpenAIProvider_HealthCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path=%s want /v1/models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer health-key" {
			t.Fatalf("Authorization=%q want health-key", got)
		}
		_, _ = fmt.Fprintln(w, `{"data":[]}`)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(ProviderConfig{BaseURL: server.URL + "/v1", APIKey: "health-key"})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := provider.HealthCheck(ctx); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
}

func ssePayload(payload string) string { return "data: " + payload + "\n\n" }
