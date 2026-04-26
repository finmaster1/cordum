package llmchat

import (
	"context"
	"sync"
)

// MockProvider is a scripted Provider implementation for tests. The
// caller stages a sequence of Chunks via SetScript; subsequent
// Complete calls drain that script onto the returned channel. The
// last seen request and SamplingMode are captured under a mutex so
// table-driven tests can assert on them without fighting goroutines.
//
// HealthCheck returns the configured HealthErr (nil by default), so
// readyz tests can simulate both healthy and degraded backends.
type MockProvider struct {
	mu          sync.Mutex
	script      []Chunk
	lastReq     CompleteRequest
	lastMode    SamplingMode
	calls       int
	healthErr   error
	healthCalls int
}

// NewMockProvider returns a fresh MockProvider with an empty script.
func NewMockProvider() *MockProvider {
	return &MockProvider{}
}

// SetScript replaces the chunk stream the next Complete call will
// emit. A nil or empty slice is allowed; Complete will then close
// the channel after emitting a single Done chunk.
func (m *MockProvider) SetScript(chunks []Chunk) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.script = append([]Chunk(nil), chunks...)
}

// SetHealthErr configures the error returned by HealthCheck. Pass
// nil to mark the provider healthy.
func (m *MockProvider) SetHealthErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healthErr = err
}

// LastRequest returns the most recent (request, mode) pair seen by
// Complete. Useful for asserting two-pass sampling carried the
// expected mode through the agent loop.
func (m *MockProvider) LastRequest() (CompleteRequest, SamplingMode) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastReq, m.lastMode
}

// Calls returns the number of Complete invocations seen.
func (m *MockProvider) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// HealthCalls returns the number of HealthCheck invocations seen.
func (m *MockProvider) HealthCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.healthCalls
}

// Complete emits the scripted chunks. If the script is empty the
// channel is closed after a single Done chunk so callers using
// `for chunk := range ch` exit cleanly.
func (m *MockProvider) Complete(
	ctx context.Context,
	req CompleteRequest,
	mode SamplingMode,
) (<-chan Chunk, error) {
	m.mu.Lock()
	m.lastReq = req
	m.lastMode = mode
	m.calls++
	script := append([]Chunk(nil), m.script...)
	m.mu.Unlock()

	out := make(chan Chunk, len(script)+1)
	go func() {
		defer close(out)
		for _, c := range script {
			select {
			case <-ctx.Done():
				out <- Chunk{Done: true, Err: ctx.Err()}
				return
			case out <- c:
			}
			if c.Done {
				return
			}
		}
		// Always end with a terminal Done if the script did not
		// already emit one; preserves the Provider contract that the
		// channel closes with Done=true exactly once.
		select {
		case <-ctx.Done():
			out <- Chunk{Done: true, Err: ctx.Err()}
		case out <- Chunk{Done: true}:
		}
	}()
	return out, nil
}

// HealthCheck returns the configured health error.
func (m *MockProvider) HealthCheck(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healthCalls++
	return m.healthErr
}
