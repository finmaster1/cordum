package llmchat

import (
	"context"
	"errors"
	"testing"
)

func TestMockProvider_ScriptedChunks(t *testing.T) {
	t.Parallel()

	m := NewMockProvider()
	m.SetScript([]Chunk{
		{Delta: "hello"},
		{Delta: " world", FinishReason: "stop"},
	})

	ch, err := m.Complete(context.Background(), CompleteRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, SamplingModeResponse)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	var got []Chunk
	for c := range ch {
		got = append(got, c)
	}
	if len(got) < 2 {
		t.Fatalf("got %d chunks, want >= 2", len(got))
	}
	if got[0].Delta != "hello" {
		t.Errorf("chunk[0].Delta = %q, want hello", got[0].Delta)
	}
	if got[1].Delta != " world" {
		t.Errorf("chunk[1].Delta = %q, want ' world'", got[1].Delta)
	}
	if !got[len(got)-1].Done {
		t.Error("expected terminal Done chunk")
	}
}

func TestMockProvider_CapturesRequestAndMode(t *testing.T) {
	t.Parallel()

	m := NewMockProvider()
	req := CompleteRequest{Messages: []Message{{Role: "user", Content: "ping"}}}

	ch, err := m.Complete(context.Background(), req, SamplingModeResponse)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	for range ch {
	}

	gotReq, gotMode := m.LastRequest()
	if len(gotReq.Messages) != 1 || gotReq.Messages[0].Content != "ping" {
		t.Errorf("captured request = %+v, want one user message 'ping'", gotReq)
	}
	if gotMode != SamplingModeResponse {
		t.Errorf("captured mode = %v, want %v", gotMode, SamplingModeResponse)
	}
	if got := m.Calls(); got != 1 {
		t.Errorf("Calls = %d, want 1", got)
	}
}

func TestMockProvider_EmptyScriptStillCloses(t *testing.T) {
	t.Parallel()

	m := NewMockProvider()
	ch, err := m.Complete(context.Background(), CompleteRequest{}, SamplingModeResponse)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()
	<-done
}

func TestMockProvider_ContextCancel(t *testing.T) {
	t.Parallel()

	m := NewMockProvider()
	long := make([]Chunk, 0, 32)
	for range 32 {
		long = append(long, Chunk{Delta: "tick"})
	}
	m.SetScript(long)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ch, err := m.Complete(ctx, CompleteRequest{}, SamplingModeResponse)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	for c := range ch {
		if c.Err != nil && !errors.Is(c.Err, context.Canceled) {
			t.Fatalf("unexpected err on chunk = %v", c.Err)
		}
	}
}

func TestMockProvider_HealthCheckHappyPath(t *testing.T) {
	t.Parallel()

	m := NewMockProvider()
	if err := m.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck = %v, want nil", err)
	}
	if got := m.HealthCalls(); got != 1 {
		t.Errorf("HealthCalls = %d, want 1", got)
	}
}
