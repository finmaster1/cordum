package mockvllm

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// readSSEFrames pulls every `data: ...\n\n` chunk from the response body
// and returns the raw JSON payloads (stripped of "data: " prefix) plus a
// boolean indicating whether the [DONE] sentinel was the final frame.
func readSSEFrames(t *testing.T, body *http.Response) ([]string, bool) {
	t.Helper()
	defer func() { _ = body.Body.Close() }()
	scanner := bufio.NewScanner(body.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var frames []string
	sawDone := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			sawDone = true
			continue
		}
		frames = append(frames, payload)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan SSE: %v", err)
	}
	return frames, sawDone
}

func postCompletions(t *testing.T, s *Server) *http.Response {
	t.Helper()
	res, err := http.Post(
		s.URL+"/v1/chat/completions",
		"application/json",
		strings.NewReader(`{"model":"qwen","messages":[]}`),
	)
	if err != nil {
		t.Fatalf("post completions: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		_ = res.Body.Close()
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	return res
}

func TestNewServer_TextOnlyTurnEndsWithStop(t *testing.T) {
	s := NewServer(t, Script{Turns: []Turn{{
		TextDeltas:   []string{"Hel", "lo, ", "world"},
		FinishReason: "stop",
	}}})

	frames, done := readSSEFrames(t, postCompletions(t, s))
	if !done {
		t.Fatal("expected [DONE] sentinel")
	}
	if len(frames) != 4 {
		t.Fatalf("expected 4 frames (3 deltas + finish), got %d", len(frames))
	}
	// Concatenate decoded content; matches the underlying contract that
	// provider_openai.go reassembles deltas into a single string.
	var got string
	for _, f := range frames {
		var frame streamFrame
		if err := json.Unmarshal([]byte(f), &frame); err != nil {
			t.Fatalf("decode frame: %v", err)
		}
		if len(frame.Choices) == 0 {
			continue
		}
		got += frame.Choices[0].Delta.Content
	}
	if got != "Hello, world" {
		t.Fatalf("reassembled content = %q, want %q", got, "Hello, world")
	}
}

func TestNewServer_ToolCallTurnFinishesWithToolCalls(t *testing.T) {
	s := NewServer(t, Script{Turns: []Turn{{
		ToolCalls: []ToolCallDelta{
			{ID: "tc-1", Name: "cordum_list_jobs", Arguments: `{"limit":10}`},
		},
		FinishReason: "tool_calls",
	}}})

	frames, done := readSSEFrames(t, postCompletions(t, s))
	if !done {
		t.Fatal("expected [DONE] sentinel")
	}
	// Find the tool-call frame and the terminator frame.
	var sawToolCall bool
	var sawFinishToolCalls bool
	for _, raw := range frames {
		var frame streamFrame
		if err := json.Unmarshal([]byte(raw), &frame); err != nil {
			t.Fatalf("decode frame: %v", err)
		}
		if len(frame.Choices) == 0 {
			continue
		}
		choice := frame.Choices[0]
		if len(choice.Delta.ToolCalls) == 1 {
			tc := choice.Delta.ToolCalls[0]
			if tc.ID != "tc-1" || tc.Function.Name != "cordum_list_jobs" {
				t.Fatalf("unexpected tool call frame: %+v", tc)
			}
			if tc.Function.Arguments != `{"limit":10}` {
				t.Fatalf("tool args = %q", tc.Function.Arguments)
			}
			sawToolCall = true
		}
		if choice.FinishReason != nil && *choice.FinishReason == "tool_calls" {
			sawFinishToolCalls = true
		}
	}
	if !sawToolCall {
		t.Fatal("expected a tool_calls delta frame")
	}
	if !sawFinishToolCalls {
		t.Fatal("expected finish_reason=tool_calls on the terminal frame")
	}
}

func TestNewServer_PerTurnAdvance(t *testing.T) {
	s := NewServer(t, Script{Turns: []Turn{
		{TextDeltas: []string{"first"}, FinishReason: "stop"},
		{TextDeltas: []string{"second"}, FinishReason: "stop"},
	}})
	for i, want := range []string{"first", "second"} {
		frames, _ := readSSEFrames(t, postCompletions(t, s))
		var got string
		for _, raw := range frames {
			var frame streamFrame
			_ = json.Unmarshal([]byte(raw), &frame)
			if len(frame.Choices) > 0 {
				got += frame.Choices[0].Delta.Content
			}
		}
		if got != want {
			t.Fatalf("turn %d content = %q, want %q", i, got, want)
		}
	}
	if got := s.Calls(); got != 2 {
		t.Fatalf("Calls() = %d, want 2", got)
	}
}

func TestNewServer_OverflowFallsBackToStopTurn(t *testing.T) {
	s := NewServer(t, Script{Turns: []Turn{
		{TextDeltas: []string{"only"}, FinishReason: "stop"},
	}})
	// Burn the scripted turn.
	_ = postCompletions(t, s).Body.Close()
	// Subsequent request must still terminate cleanly with [DONE].
	res := postCompletions(t, s)
	frames, done := readSSEFrames(t, res)
	if !done {
		t.Fatal("overflow request must still emit [DONE]")
	}
	// Empty text + stop terminator is the documented fallback.
	if len(frames) == 0 {
		t.Fatal("expected at least the terminator frame")
	}
}

func TestNewServer_ModelsEndpointAlwaysHealthy(t *testing.T) {
	s := NewServerHealthy(t)
	res, err := http.Get(s.URL + "/v1/models")
	if err != nil {
		t.Fatalf("get /v1/models: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("models endpoint = %d", res.StatusCode)
	}
	var body struct {
		Object string           `json:"object"`
		Data   []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Object != "list" || len(body.Data) == 0 {
		t.Fatalf("unexpected models body: %+v", body)
	}
}

func TestNewServer_RejectsNonPostCompletions(t *testing.T) {
	s := NewServer(t, Script{})
	res, err := http.Get(s.URL + "/v1/chat/completions")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", res.StatusCode)
	}
}
