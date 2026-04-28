// Package llmchat provides the LLM provider abstraction behind the
// cordum-llm-chat service. Consumers call ResolveProvider with a
// ProviderConfig and receive a Provider whose Complete streams chunks
// back on a channel.
package llmchat

import (
	"context"
	"fmt"
	"time"
)

// SamplingMode selects response-generation settings for a Complete call.
// The prior tool-selection mode was retired with the informational-only
// scope reduction; callers now use one prose response path.
type SamplingMode int

const (
	// SamplingModeResponse is the only production sampling mode in
	// informational-only chat.
	SamplingModeResponse SamplingMode = iota
)

// String returns the canonical wire-name for a sampling mode; exposed so
// structured logs and traces can carry a human-readable value.
func (m SamplingMode) String() string {
	switch m {
	case SamplingModeResponse:
		return "response"
	default:
		return fmt.Sprintf("unknown(%d)", int(m))
	}
}

// ProviderConfig is the fully-resolved, validated provider configuration
// passed into ResolveProvider. Callers read env vars in the process
// entry-point and materialise this struct exactly once.
type ProviderConfig struct {
	// Kind is the provider type ("openai"). Future backends join via the
	// ResolveProvider switch; no inline defaulting so misconfig is loud.
	Kind string

	// BaseURL is the OpenAI-compat root, e.g. http://ollama:11434/v1.
	// The provider appends /chat/completions and /models itself.
	BaseURL string

	// Model is the model name the backend expects.
	Model string

	// APIKey is optional; populated when the backend requires Authorization.
	APIKey string

	// ResponseTemperature + ResponseTopP are applied to the single
	// informational-answer generation path.
	ResponseTemperature float64
	ResponseTopP        float64
}

// BudgetConfig carries per-turn safety bounds for the agent loop.
type BudgetConfig struct {
	MaxWallClockPerTurn time.Duration
	MaxAssistantBytes   int
}

// Message is one entry in the chat transcript fed to the model.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content,omitempty"`
	Name    string `json:"name,omitempty"`
}

// CompleteRequest carries the model input.
type CompleteRequest struct {
	Messages []Message
}

// Chunk is one frame of streaming output.
type Chunk struct {
	// Delta is a partial text fragment from the assistant response.
	Delta string

	// FinishReason is set on the terminal Chunk when the backend supplies
	// one ("stop", "length", ...). Empty otherwise.
	FinishReason string

	// Done is true on the final Chunk emitted before the channel closes.
	Done bool

	// Err is set when the stream aborts abnormally (network, 4xx, retry
	// exhaustion). A chunk with Err set is always the last chunk before the
	// channel closes.
	Err error
}

// Provider is the minimal surface a chat-capable LLM backend must implement.
type Provider interface {
	// Complete begins a streaming chat-completion call. The caller ranges over
	// the returned channel; the channel is closed after the final Chunk. An
	// error returned from Complete itself represents a pre-dispatch failure;
	// transient stream errors are surfaced via Chunk.Err.
	Complete(ctx context.Context, req CompleteRequest, mode SamplingMode) (<-chan Chunk, error)

	// HealthCheck is used by /readyz to probe the backend without performing
	// a full completion. Implementations should use a cheap endpoint.
	HealthCheck(ctx context.Context) error
}

// ResolveProvider returns a Provider matching the cfg.Kind. Unknown kinds fail
// closed — operators should see a crisp error rather than a silent fallback.
func ResolveProvider(cfg ProviderConfig) (Provider, error) {
	switch cfg.Kind {
	case "openai":
		return NewOpenAIProvider(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported LLMCHAT_PROVIDER %q", cfg.Kind)
	}
}
