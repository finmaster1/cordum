package audit

import (
	"sync"
	"testing"
)

// mockSender collects events for test assertions.
type mockSender struct {
	mu     sync.Mutex
	events []SIEMEvent
}

func (m *mockSender) Send(e SIEMEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
}

func (m *mockSender) Close() error { return nil }

func (m *mockSender) Events() []SIEMEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]SIEMEvent, len(m.events))
	copy(out, m.events)
	return out
}

func TestSIEMEventFields(t *testing.T) {
	event := SIEMEvent{
		EventType: EventSystemAuth,
		Severity:  SeverityMedium,
		Action:    "auth.failure",
		Reason:    "invalid_credentials",
		Identity:  "ali***",
		Extra: map[string]string{
			"source_ip":   "192.168.1.1",
			"auth_method": "password",
		},
	}

	if event.EventType != "system.auth" {
		t.Fatalf("expected event type system.auth, got %s", event.EventType)
	}
	if event.Severity != "MEDIUM" {
		t.Fatalf("expected severity MEDIUM, got %s", event.Severity)
	}
	if event.Extra["source_ip"] != "192.168.1.1" {
		t.Fatalf("expected source_ip 192.168.1.1, got %s", event.Extra["source_ip"])
	}
}

func TestMockSenderCollectsEvents(t *testing.T) {
	sender := &mockSender{}

	sender.Send(SIEMEvent{Action: "auth.failure", Reason: "invalid_credentials"})
	sender.Send(SIEMEvent{Action: "data.read", Extra: map[string]string{"path": "/api/v1/policy/bundles"}})

	events := sender.Events()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Action != "auth.failure" {
		t.Fatalf("expected auth.failure, got %s", events[0].Action)
	}
	if events[1].Action != "data.read" {
		t.Fatalf("expected data.read, got %s", events[1].Action)
	}
}

func TestAuthEventType(t *testing.T) {
	if EventSystemAuth != "system.auth" {
		t.Fatalf("unexpected EventSystemAuth: %s", EventSystemAuth)
	}
}
