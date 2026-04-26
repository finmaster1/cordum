package llmchat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// captureFlusher writes a value to fl and returns the http.Flusher
// version, panicking the test if the underlying writer cannot flush.
func captureFlusher(t *testing.T, w http.ResponseWriter) http.Flusher {
	t.Helper()
	fl, ok := w.(http.Flusher)
	if !ok {
		t.Fatal("response writer does not support flush; SSE tests need flushable writer")
	}
	return fl
}

// streamFrames writes each SSE event onto w with a Flush between
// frames so the client sees boundaries immediately. Callers must
// include the terminating `data: [DONE]` frame in the slice if
// required.
func streamFrames(t *testing.T, w http.ResponseWriter, frames []string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	fl := captureFlusher(t, w)
	for _, f := range frames {
		if _, err := io.WriteString(w, f); err != nil {
			t.Fatalf("write SSE frame: %v", err)
		}
		fl.Flush()
	}
}

// ssePayload wraps a JSON object as a single SSE event terminated by
// the required blank line.
func ssePayload(payload string) string {
	return "data: " + payload + "\n\n"
}

func collect(t *testing.T, ch <-chan Chunk) []Chunk {
	t.Helper()
	var got []Chunk
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case c, ok := <-ch:
			if !ok {
				return got
			}
			got = append(got, c)
		case <-timeout.C:
			t.Fatalf("collect timed out, got=%v", got)
			return got
		}
	}
}

func TestOpenAIProvider_TextStreaming(t *testing.T) {
	t.Parallel()

	frames := []string{
		ssePayload(`{"choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"}}]}`),
		ssePayload(`{"choices":[{"index":0,"delta":{"content":"lo, "}}]}`),
		ssePayload(`{"choices":[{"index":0,"delta":{"content":"world"},"finish_reason":"stop"}]}`),
		ssePayload("[DONE]"),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		streamFrames(t, w, frames)
	}))
	defer srv.Close()

	p := NewOpenAIProvider(ProviderConfig{
		Kind:               "openai",
		BaseURL:            srv.URL,
		Model:              "qwen3-coder",
		ToolTemperature:    0.3,
		ToolTopP:           0.9,
		SummaryTemperature: 0.7,
		SummaryTopP:        0.8,
	})

	ch, err := p.Complete(context.Background(), CompleteRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, SamplingModeSummary)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	chunks := collect(t, ch)

	var text strings.Builder
	var sawFinish, sawDone bool
	for _, c := range chunks {
		text.WriteString(c.Delta)
		if c.FinishReason == "stop" {
			sawFinish = true
		}
		if c.Done {
			sawDone = true
		}
	}
	if got := text.String(); got != "Hello, world" {
		t.Fatalf("text = %q, want %q", got, "Hello, world")
	}
	if !sawFinish {
		t.Error("expected at least one chunk with finish_reason=stop")
	}
	if !sawDone {
		t.Error("expected terminal Done chunk")
	}
}

func TestOpenAIProvider_ToolCallStreaming(t *testing.T) {
	t.Parallel()

	frames := []string{
		ssePayload(`{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"cordum_list_jobs","arguments":"{\"limit\":5}"}}]}}]}`),
		ssePayload(`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`),
		ssePayload("[DONE]"),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		streamFrames(t, w, frames)
	}))
	defer srv.Close()

	p := NewOpenAIProvider(ProviderConfig{Kind: "openai", BaseURL: srv.URL, Model: "qwen3-coder"})

	ch, err := p.Complete(context.Background(), CompleteRequest{
		Messages: []Message{{Role: "user", Content: "what's running?"}},
	}, SamplingModeToolCalls)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	chunks := collect(t, ch)

	var seenName, seenArgs string
	var sawFinish bool
	for _, c := range chunks {
		for _, tc := range c.ToolCalls {
			if tc.Name != "" {
				seenName = tc.Name
			}
			if len(tc.Arguments) > 0 {
				seenArgs = string(tc.Arguments)
			}
		}
		if c.FinishReason == "tool_calls" {
			sawFinish = true
		}
	}
	if seenName != "cordum_list_jobs" {
		t.Fatalf("tool_call name = %q, want cordum_list_jobs", seenName)
	}
	if seenArgs != `{"limit":5}` {
		t.Fatalf("tool_call args = %q, want {\"limit\":5}", seenArgs)
	}
	if !sawFinish {
		t.Error("expected finish_reason=tool_calls")
	}
}

// TestOpenAIProvider_SamplingModeSelectsTemps asserts the load-bearing
// two-pass sampling: tool-call mode uses 0.3/0.9, summary mode uses
// 0.7/0.8. QA (task-7dd1af21) explicitly verifies this.
func TestOpenAIProvider_SamplingModeSelectsTemps(t *testing.T) {
	t.Parallel()

	type captured struct {
		Temperature float64 `json:"temperature"`
		TopP        float64 `json:"top_p"`
	}

	cases := []struct {
		name     string
		mode     SamplingMode
		wantTemp float64
		wantTopP float64
	}{
		{"tool_calls uses 0.3/0.9", SamplingModeToolCalls, 0.3, 0.9},
		{"summary uses 0.7/0.8", SamplingModeSummary, 0.7, 0.8},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got captured
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Errorf("decode body: %v", err)
				}
				streamFrames(t, w, []string{
					ssePayload(`{"choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`),
					ssePayload("[DONE]"),
				})
			}))
			defer srv.Close()

			p := NewOpenAIProvider(ProviderConfig{
				Kind:               "openai",
				BaseURL:            srv.URL,
				Model:              "qwen3-coder",
				ToolTemperature:    0.3,
				ToolTopP:           0.9,
				SummaryTemperature: 0.7,
				SummaryTopP:        0.8,
			})
			ch, err := p.Complete(context.Background(), CompleteRequest{
				Messages: []Message{{Role: "user", Content: "ping"}},
			}, tc.mode)
			if err != nil {
				t.Fatalf("Complete: %v", err)
			}
			_ = collect(t, ch)

			if got.Temperature != tc.wantTemp {
				t.Errorf("temperature = %v, want %v", got.Temperature, tc.wantTemp)
			}
			if got.TopP != tc.wantTopP {
				t.Errorf("top_p = %v, want %v", got.TopP, tc.wantTopP)
			}
		})
	}
}

func TestOpenAIProvider_RetriesOn5xx(t *testing.T) {
	t.Parallel()

	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			http.Error(w, "rolling restart", http.StatusServiceUnavailable)
			return
		}
		streamFrames(t, w, []string{
			ssePayload(`{"choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`),
			ssePayload("[DONE]"),
		})
	}))
	defer srv.Close()

	p := NewOpenAIProvider(ProviderConfig{Kind: "openai", BaseURL: srv.URL, Model: "qwen3-coder"})
	ch, err := p.Complete(context.Background(), CompleteRequest{
		Messages: []Message{{Role: "user", Content: "ping"}},
	}, SamplingModeSummary)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	_ = collect(t, ch)

	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("attempts = %d, want 2 (one 503, one 200)", got)
	}
}

func TestOpenAIProvider_NoRetryOn4xx(t *testing.T) {
	t.Parallel()

	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		http.Error(w, "bad payload", http.StatusBadRequest)
	}))
	defer srv.Close()

	p := NewOpenAIProvider(ProviderConfig{Kind: "openai", BaseURL: srv.URL, Model: "qwen3-coder"})
	_, err := p.Complete(context.Background(), CompleteRequest{
		Messages: []Message{{Role: "user", Content: "ping"}},
	}, SamplingModeSummary)
	if err == nil {
		t.Fatal("expected error for 400, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error %v should mention status 400", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("attempts = %d, want 1 (no retry on 4xx)", got)
	}
}

// TestOpenAIProvider_SplitFrameAcrossWrites verifies the SSE buffer
// reassembles a payload split mid-frame across two writes.
func TestOpenAIProvider_SplitFrameAcrossWrites(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl := captureFlusher(t, w)
		// Half a frame, flush, then the rest.
		_, _ = io.WriteString(w, "data: {\"choi")
		fl.Flush()
		_, _ = io.WriteString(w, "ces\":[{\"index\":0,\"delta\":{\"content\":\"split-ok\"},\"finish_reason\":\"stop\"}]}\n\n")
		fl.Flush()
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		fl.Flush()
	}))
	defer srv.Close()

	p := NewOpenAIProvider(ProviderConfig{Kind: "openai", BaseURL: srv.URL, Model: "qwen3-coder"})
	ch, err := p.Complete(context.Background(), CompleteRequest{
		Messages: []Message{{Role: "user", Content: "split"}},
	}, SamplingModeSummary)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	chunks := collect(t, ch)

	var text strings.Builder
	for _, c := range chunks {
		text.WriteString(c.Delta)
	}
	if got := text.String(); got != "split-ok" {
		t.Fatalf("text = %q, want %q", got, "split-ok")
	}
}

func TestOpenAIProvider_AuthHeader(t *testing.T) {
	t.Parallel()

	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		streamFrames(t, w, []string{
			ssePayload(`{"choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`),
			ssePayload("[DONE]"),
		})
	}))
	defer srv.Close()

	p := NewOpenAIProvider(ProviderConfig{
		Kind:    "openai",
		BaseURL: srv.URL,
		Model:   "qwen3-coder",
		APIKey:  "sk-test",
	})
	ch, err := p.Complete(context.Background(), CompleteRequest{
		Messages: []Message{{Role: "user", Content: "ping"}},
	}, SamplingModeSummary)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	_ = collect(t, ch)

	if sawAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want Bearer sk-test", sawAuth)
	}
}

func TestOpenAIProvider_ToolsForwarded(t *testing.T) {
	t.Parallel()

	type fnSchema struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	type tool struct {
		Type     string   `json:"type"`
		Function fnSchema `json:"function"`
	}
	type body struct {
		Tools      []tool `json:"tools"`
		ToolChoice string `json:"tool_choice"`
	}

	var got body
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		streamFrames(t, w, []string{
			ssePayload(`{"choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`),
			ssePayload("[DONE]"),
		})
	}))
	defer srv.Close()

	p := NewOpenAIProvider(ProviderConfig{Kind: "openai", BaseURL: srv.URL, Model: "qwen3-coder"})

	rawSchema := json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer"}}}`)
	ch, err := p.Complete(context.Background(), CompleteRequest{
		Messages: []Message{{Role: "user", Content: "list"}},
		Tools: []Tool{{
			Name:        "cordum_list_jobs",
			Description: "List jobs",
			Parameters:  rawSchema,
		}},
	}, SamplingModeToolCalls)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	_ = collect(t, ch)

	if len(got.Tools) != 1 {
		t.Fatalf("tools forwarded = %d, want 1", len(got.Tools))
	}
	if got.Tools[0].Function.Name != "cordum_list_jobs" {
		t.Errorf("tool name = %q, want cordum_list_jobs", got.Tools[0].Function.Name)
	}
	if got.ToolChoice != "auto" {
		t.Errorf("tool_choice = %q, want auto", got.ToolChoice)
	}
}

// TestOpenAIProvider_PrematureEOF asserts that an SSE stream which
// ends without [DONE] or finish_reason surfaces ErrPrematureStreamEnd
// on the terminal Chunk — silent truncation would corrupt the audit
// trail and let a partial assistant turn pose as a complete one.
func TestOpenAIProvider_PrematureEOF(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl := captureFlusher(t, w)
		// Emit a single content frame, then close mid-stream — no
		// [DONE], no finish_reason. Models a network drop or backend
		// crash mid-response.
		_, _ = io.WriteString(w, ssePayload(`{"choices":[{"index":0,"delta":{"content":"par"}}]}`))
		fl.Flush()
	}))
	defer srv.Close()

	p := NewOpenAIProvider(ProviderConfig{Kind: "openai", BaseURL: srv.URL, Model: "qwen3-coder"})
	ch, err := p.Complete(context.Background(), CompleteRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, SamplingModeSummary)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	chunks := collect(t, ch)

	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}
	terminal := chunks[len(chunks)-1]
	if !terminal.Done {
		t.Fatalf("terminal Chunk.Done = false, full chunks=%+v", chunks)
	}
	if !errors.Is(terminal.Err, ErrPrematureStreamEnd) {
		t.Fatalf("terminal.Err = %v, want ErrPrematureStreamEnd", terminal.Err)
	}
}

// TestOpenAIProvider_UsageFrame asserts that an OpenAI-compatible
// streaming usage frame (choices: [], usage: {...}) is parsed without
// error and does NOT terminate the stream — text content following the
// usage block must still flow to the consumer.
func TestOpenAIProvider_UsageFrame(t *testing.T) {
	t.Parallel()

	frames := []string{
		ssePayload(`{"choices":[{"index":0,"delta":{"role":"assistant","content":"hello"}}]}`),
		// Usage frames have empty choices; OpenAI sends them when
		// stream_options.include_usage=true. Parser MUST NOT mistake
		// the empty-choices frame for a terminator.
		ssePayload(`{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`),
		ssePayload(`{"choices":[{"index":0,"delta":{"content":" world"},"finish_reason":"stop"}]}`),
		ssePayload("[DONE]"),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		streamFrames(t, w, frames)
	}))
	defer srv.Close()

	p := NewOpenAIProvider(ProviderConfig{Kind: "openai", BaseURL: srv.URL, Model: "qwen3-coder"})
	ch, err := p.Complete(context.Background(), CompleteRequest{
		Messages: []Message{{Role: "user", Content: "ping"}},
	}, SamplingModeSummary)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	chunks := collect(t, ch)

	var text strings.Builder
	var sawFinish bool
	for _, c := range chunks {
		if c.Err != nil {
			t.Fatalf("unexpected Chunk.Err = %v (usage frame should not error the stream)", c.Err)
		}
		text.WriteString(c.Delta)
		if c.FinishReason == "stop" {
			sawFinish = true
		}
	}
	if got := text.String(); got != "hello world" {
		t.Fatalf("text = %q, want %q (content following usage frame must flow through)", got, "hello world")
	}
	if !sawFinish {
		t.Error("expected finish_reason=stop after the usage frame")
	}
}

func TestOpenAIProvider_HealthCheckOK(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("path = %q, want /models", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"qwen3-coder"}]}`)
	}))
	defer srv.Close()

	p := NewOpenAIProvider(ProviderConfig{Kind: "openai", BaseURL: srv.URL})
	if err := p.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
}

func TestOpenAIProvider_HealthCheckFail(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	p := NewOpenAIProvider(ProviderConfig{Kind: "openai", BaseURL: srv.URL})
	err := p.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error from 503, got nil")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error %v should mention 503", err)
	}
}

func TestOpenAIProvider_ContextCancelEndsStream(t *testing.T) {
	t.Parallel()

	hold := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl := captureFlusher(t, w)
		_, _ = io.WriteString(w, ssePayload(`{"choices":[{"index":0,"delta":{"content":"slow"}}]}`))
		fl.Flush()
		select {
		case <-hold:
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()
	defer close(hold)

	p := NewOpenAIProvider(ProviderConfig{Kind: "openai", BaseURL: srv.URL})
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := p.Complete(ctx, CompleteRequest{
		Messages: []Message{{Role: "user", Content: "go"}},
	}, SamplingModeSummary)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	first := <-ch
	if first.Delta != "slow" {
		t.Fatalf("first delta = %q, want slow", first.Delta)
	}
	cancel()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case c, ok := <-ch:
			if !ok {
				return // channel closed cleanly
			}
			if c.Err != nil && !errors.Is(c.Err, context.Canceled) {
				t.Fatalf("terminal err = %v, want context.Canceled or channel close", c.Err)
			}
		case <-deadline:
			t.Fatal("stream did not close within 2s of cancel")
		}
	}
}
