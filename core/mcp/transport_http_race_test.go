package mcp

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestWriteSessionEventConcurrentRemove(t *testing.T) {
	transport := NewHTTPTransport(0, 0)

	var panics atomic.Int32
	const iterations = 1000

	for i := 0; i < iterations; i++ {
		// Create a session
		session := &httpSession{
			id:     "test-session",
			events: make(chan []byte, defaultSSEEventBuffer),
		}
		transport.mu.Lock()
		transport.sessions["test-session"] = session
		transport.mu.Unlock()

		var wg sync.WaitGroup
		wg.Add(2)

		// Writer goroutine
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panics.Add(1)
				}
			}()
			msg := &JSONRPCMessage{Method: "test"}
			_ = transport.writeSessionEvent("test-session", msg)
		}()

		// Remover goroutine
		go func() {
			defer wg.Done()
			transport.removeSession("test-session")
		}()

		wg.Wait()
	}

	if p := panics.Load(); p > 0 {
		t.Fatalf("got %d panics in %d iterations", p, iterations)
	}
}

func TestSSEBufferFullReturnsError(t *testing.T) {
	transport := NewHTTPTransport(0, 0)

	session := &httpSession{
		id:     "buf-test",
		events: make(chan []byte, 2), // tiny buffer
	}
	transport.mu.Lock()
	transport.sessions["buf-test"] = session
	transport.mu.Unlock()

	// Fill buffer
	msg := &JSONRPCMessage{Method: "test"}
	if err := transport.writeSessionEvent("buf-test", msg); err != nil {
		t.Fatalf("first write should succeed: %v", err)
	}
	if err := transport.writeSessionEvent("buf-test", msg); err != nil {
		t.Fatalf("second write should succeed: %v", err)
	}

	// Third should return error (buffer full), not block
	err := transport.writeSessionEvent("buf-test", msg)
	if err == nil {
		t.Fatal("expected buffer full error")
	}
	if err.Error() != "sse session buffer full" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteSessionEventClosedSession(t *testing.T) {
	transport := NewHTTPTransport(0, 0)

	session := &httpSession{
		id:     "closed-test",
		events: make(chan []byte, 10),
	}
	session.closed.Store(true)
	transport.mu.Lock()
	transport.sessions["closed-test"] = session
	transport.mu.Unlock()

	msg := &JSONRPCMessage{Method: "test"}
	err := transport.writeSessionEvent("closed-test", msg)
	if err != nil {
		t.Fatalf("closed session should return nil (silent skip), got: %v", err)
	}
}
