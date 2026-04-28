package llmchat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIProvider is the OpenAI-compat streaming chat-completions client used
// against Ollama, vLLM, openai.com, or any other backend that speaks the
// OpenAI wire format.
type OpenAIProvider struct {
	baseURL             string
	model               string
	apiKey              string
	responseTemperature float64
	responseTopP        float64

	// httpClient carries no Timeout so streaming response bodies are not cut
	// off mid-frame. Cancellation is driven by ctx instead.
	httpClient *http.Client

	// healthClient is a short-deadline client used for /readyz probes.
	healthClient *http.Client
}

// NewOpenAIProvider constructs a provider from a ProviderConfig. All
// validation already happened at ResolveProvider; this function does not
// re-check fields.
func NewOpenAIProvider(cfg ProviderConfig) *OpenAIProvider {
	return &OpenAIProvider{
		baseURL:             strings.TrimRight(cfg.BaseURL, "/"),
		model:               cfg.Model,
		apiKey:              cfg.APIKey,
		responseTemperature: cfg.ResponseTemperature,
		responseTopP:        cfg.ResponseTopP,
		httpClient:          &http.Client{}, // no timeout: context drives cancellation on streams
		healthClient:        &http.Client{Timeout: 2 * time.Second},
	}
}

type openaiMsg struct {
	Role    string `json:"role"`
	Content string `json:"content,omitempty"`
	Name    string `json:"name,omitempty"`
}

// openaiStreamFrame is one decoded `data: {...}` SSE frame.
type openaiStreamFrame struct {
	Choices []openaiStreamChoice `json:"choices"`
}

type openaiStreamChoice struct {
	Index        int               `json:"index"`
	Delta        openaiStreamDelta `json:"delta"`
	FinishReason *string           `json:"finish_reason,omitempty"`
}

type openaiStreamDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

var retryDelays = []time.Duration{
	100 * time.Millisecond,
	200 * time.Millisecond,
	400 * time.Millisecond,
}

// Complete issues a streaming chat-completion request and returns a channel
// that emits Chunks until the stream terminates.
func (p *OpenAIProvider) Complete(
	ctx context.Context,
	req CompleteRequest,
	mode SamplingMode,
) (<-chan Chunk, error) {
	body, err := p.buildRequestBody(req, mode)
	if err != nil {
		return nil, fmt.Errorf("llmchat/openai: build request: %w", err)
	}

	resp, err := p.doWithRetry(ctx, body)
	if err != nil {
		return nil, err
	}

	out := make(chan Chunk, 8)
	go p.stream(ctx, resp, out)
	return out, nil
}

// buildRequestBody serialises the OpenAI-compat POST body. Informational-only
// chat never sends OpenAI `tools` or `tool_choice`; every turn is a direct
// response grounded by the system prompt and knowledge pack.
func (p *OpenAIProvider) buildRequestBody(req CompleteRequest, _ SamplingMode) ([]byte, error) {
	msgs := make([]openaiMsg, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, openaiMsg{Role: m.Role, Content: m.Content, Name: m.Name})
	}

	payload := struct {
		Model       string      `json:"model"`
		Messages    []openaiMsg `json:"messages"`
		Stream      bool        `json:"stream"`
		Temperature float64     `json:"temperature"`
		TopP        float64     `json:"top_p"`
	}{
		Model:       p.model,
		Messages:    msgs,
		Stream:      true,
		Temperature: p.responseTemperature,
		TopP:        p.responseTopP,
	}
	return json.Marshal(payload)
}

// doWithRetry sends the POST with up to 4 attempts, retrying only on 5xx or
// transport errors. A 4xx surfaces immediately so malformed payloads fail loud.
func (p *OpenAIProvider) doWithRetry(ctx context.Context, body []byte) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= len(retryDelays); attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryDelays[attempt-1]):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("llmchat/openai: build http request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		if p.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+p.apiKey)
		}

		resp, err := p.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue // retry on transport error
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			preview, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			_ = resp.Body.Close()
			return nil, fmt.Errorf("llmchat/openai: non-retryable status %d: %s", resp.StatusCode, strings.TrimSpace(string(preview)))
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<14))
		_ = resp.Body.Close()
		lastErr = fmt.Errorf("server status %d", resp.StatusCode)
	}
	return nil, fmt.Errorf("llmchat/openai: retry exhausted: %w", lastErr)
}

// ErrPrematureStreamEnd is returned on the terminal Chunk when the SSE stream
// ends before the backend emits either [DONE] or a frame with a finish_reason.
var ErrPrematureStreamEnd = errors.New("llmchat/openai: stream ended before [DONE] or finish_reason")

// stream reads the SSE body frame-by-frame and emits Chunks.
func (p *OpenAIProvider) stream(ctx context.Context, resp *http.Response, out chan<- Chunk) {
	defer func() { _ = resp.Body.Close() }()
	defer close(out)

	reader := bufio.NewReader(resp.Body)
	var buf bytes.Buffer

	for {
		select {
		case <-ctx.Done():
			emit(ctx, out, Chunk{Done: true, Err: ctx.Err()})
			return
		default:
		}

		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			buf.Write(line)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if terminated := p.drainBuffer(ctx, &buf, out); terminated {
					return
				}
				emit(ctx, out, Chunk{Done: true, Err: ErrPrematureStreamEnd})
				return
			}
			emit(ctx, out, Chunk{Done: true, Err: fmt.Errorf("llmchat/openai: stream read: %w", err)})
			return
		}

		if !isBlankLine(line) {
			continue
		}

		frameBytes := buf.Bytes()
		buf.Reset()
		if stop := p.emitFrame(ctx, frameBytes, out); stop {
			return
		}
	}
}

// drainBuffer processes any frame still sitting in the buffer on EOF without a
// trailing blank line.
func (p *OpenAIProvider) drainBuffer(ctx context.Context, buf *bytes.Buffer, out chan<- Chunk) bool {
	if buf.Len() == 0 {
		return false
	}
	return p.emitFrame(ctx, buf.Bytes(), out)
}

// emitFrame decodes a single SSE block and forwards a Chunk. Returns true when
// the stream has terminated and the caller should stop reading.
func (p *OpenAIProvider) emitFrame(ctx context.Context, frame []byte, out chan<- Chunk) bool {
	for _, rawLine := range bytes.Split(frame, []byte{'\n'}) {
		line := bytes.TrimSpace(rawLine)
		if len(line) == 0 {
			continue
		}
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(line[len("data:"):])
		if bytes.Equal(payload, []byte("[DONE]")) {
			emit(ctx, out, Chunk{Done: true})
			return true
		}

		var parsed openaiStreamFrame
		if err := json.Unmarshal(payload, &parsed); err != nil {
			emit(ctx, out, Chunk{Done: true, Err: fmt.Errorf("llmchat/openai: decode frame: %w", err)})
			return true
		}

		chunk := Chunk{}
		for _, c := range parsed.Choices {
			if c.Delta.Content != "" {
				chunk.Delta += c.Delta.Content
			}
			if c.FinishReason != nil && *c.FinishReason != "" {
				chunk.FinishReason = *c.FinishReason
			}
		}
		if chunk.Delta == "" && chunk.FinishReason == "" {
			continue
		}
		emit(ctx, out, chunk)
		if chunk.FinishReason != "" {
			emit(ctx, out, Chunk{Done: true, FinishReason: chunk.FinishReason})
			return true
		}
	}
	return false
}

// emit sends a Chunk, honouring ctx cancellation so a slow reader cannot
// deadlock the goroutine.
func emit(ctx context.Context, out chan<- Chunk, c Chunk) {
	select {
	case <-ctx.Done():
	case out <- c:
	}
}

func isBlankLine(line []byte) bool {
	s := bytes.TrimRight(line, "\r\n")
	return len(s) == 0
}

// HealthCheck is a 2s-timeout GET {BaseURL}/models.
func (p *OpenAIProvider) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return fmt.Errorf("llmchat/openai: build health request: %w", err)
	}
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.healthClient.Do(req)
	if err != nil {
		return fmt.Errorf("llmchat/openai: health GET failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("llmchat/openai: health GET status %d", resp.StatusCode)
	}
	return nil
}
