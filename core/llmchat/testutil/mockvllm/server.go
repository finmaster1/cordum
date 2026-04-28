// Package mockvllm provides an httptest.Server that speaks the OpenAI-compat
// streaming surface that core/llmchat/provider_openai.go expects, so the
// llm-chat integration tests in tests/integration/ can exercise the full
// agent loop without requiring a GPU or network access to a real vLLM
// instance.
//
// The server is deliberately script-driven, not generative: each call to
// /v1/chat/completions advances a turn counter and replays the next
// scripted Turn as a sequence of SSE frames terminated by `data: [DONE]`.
// A second mode (NewServerHealthy) exposes only /v1/models for cases that
// only need the readiness probe to pass.
//
// Concurrency: the server is goroutine-safe because it serialises script
// access behind a mutex, but each test should construct its own instance
// — the turn counter is shared across all requests to a single server,
// so cross-test reuse risks scripted-state leakage.
package mockvllm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Script is the deterministic playback contract: index N in Turns is
// emitted on the N-th request to /v1/chat/completions. Requests beyond
// the script length receive a single text turn finishing with stop.
type Script struct {
	Turns []Turn
}

// Turn is one scripted assistant turn served as a sequence of streaming
// text deltas terminated by FinishReason.
type Turn struct {
	// TextDeltas are the assistant content chunks emitted in order.
	TextDeltas []string

	// FinishReason is sent on the terminal frame of this turn. Use
	// "stop" for a clean text turn, "length" for max-tokens, etc.
	FinishReason string
}

// Server wraps an httptest.Server with a turn counter. Callers should
// defer Close() in the test that constructed it.
type Server struct {
	*httptest.Server
	mu      sync.Mutex
	script  Script
	turn    int
	healthy bool
}

// Calls returns the number of /v1/chat/completions requests served so
// far. Test assertions like "the agent loop made exactly two requests"
// can rely on this without race detection because the underlying
// counter is mutex-protected.
func (s *Server) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turn
}

// NewServer constructs a server that handles both /v1/models (always
// 200 OK) and /v1/chat/completions (script-driven SSE).
func NewServer(t *testing.T, script Script) *Server {
	t.Helper()
	return newServer(t, script, true)
}

// NewServerHealthy constructs a server that only handles /v1/models —
// useful when a test wants to assert the readiness probe path without
// scripting any assistant turns.
func NewServerHealthy(t *testing.T) *Server {
	t.Helper()
	return newServer(t, Script{}, true)
}

func newServer(t *testing.T, script Script, healthy bool) *Server {
	t.Helper()
	s := &Server{script: script, healthy: healthy}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/v1/chat/completions", s.handleCompletions)
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func (s *Server) handleModels(w http.ResponseWriter, _ *http.Request) {
	if !s.healthy {
		http.Error(w, "models unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data": []map[string]any{
			{"id": "qwen3-coder", "object": "model", "owned_by": "mockvllm"},
		},
	})
}

func (s *Server) handleCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	turn, ok := s.nextTurn()
	if !ok {
		// Out of script — fall back to a stop turn so tests that
		// over-iterate get a deterministic "no more scripted output"
		// signal instead of a hang.
		turn = Turn{
			TextDeltas:   []string{""},
			FinishReason: "stop",
		}
	}

	// SSE preamble — flush headers immediately so the client knows the
	// stream is alive even if the first delta has no content (e.g.
	// tool-only turns).
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	for i, delta := range turn.TextDeltas {
		frame := streamFrame{
			Choices: []streamChoice{
				{Delta: streamDelta{Content: delta, Role: roleIfFirst(i)}},
			},
		}
		writeFrame(w, flusher, frame)
	}
	finish := turn.FinishReason
	if finish == "" {
		finish = "stop"
	}
	writeFrame(w, flusher, streamFrame{
		Choices: []streamChoice{{
			Delta:        streamDelta{},
			FinishReason: &finish,
		}},
	})

	if _, err := fmt.Fprint(w, "data: [DONE]\n\n"); err != nil {
		return
	}
	if flusher != nil {
		flusher.Flush()
	}
}

func (s *Server) nextTurn() (Turn, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.turn
	s.turn++
	if idx < 0 || idx >= len(s.script.Turns) {
		return Turn{}, false
	}
	return s.script.Turns[idx], true
}

func roleIfFirst(i int) string {
	if i == 0 {
		return "assistant"
	}
	return ""
}

func writeFrame(w http.ResponseWriter, flusher http.Flusher, frame streamFrame) {
	buf, err := json.Marshal(frame)
	if err != nil {
		return
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", buf); err != nil {
		return
	}
	if flusher != nil {
		flusher.Flush()
	}
}

// streamFrame mirrors core/llmchat/provider_openai.go openaiStreamFrame
// — the JSON keys are pinned by the consuming SSE parser, so we keep
// them here verbatim instead of importing the unexported types.
type streamFrame struct {
	Choices []streamChoice `json:"choices"`
}

type streamChoice struct {
	Index        int         `json:"index"`
	Delta        streamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason,omitempty"`
}

type streamDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}
