package gateway

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/cordum/cordum/core/audit"
)

// testAuditSender collects events for assertions.
type testAuditSender struct {
	mu     sync.Mutex
	events []audit.SIEMEvent
}

func (s *testAuditSender) Send(e audit.SIEMEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

func (s *testAuditSender) Close() error { return nil }

func (s *testAuditSender) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

func (s *testAuditSender) Get(i int) audit.SIEMEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.events[i]
}

// --- redactUsername tests ---

func TestRedactUsername(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "<unknown>"},
		{"ab", "ab***"},
		{"abc", "abc***"},
		{"alice", "ali***"},
		{"admin@example.com", "adm***"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := redactUsername(tt.input)
			if got != tt.expected {
				t.Fatalf("redactUsername(%q) = %q, want %q", tt.input, got, tt.expected)
			}
			// Verify no full username leakage for inputs > 3 chars.
			if len(tt.input) > 3 && got == tt.input {
				t.Fatalf("redactUsername should not return the full username")
			}
		})
	}
}

// --- isSensitiveRead tests ---

func TestIsSensitiveRead(t *testing.T) {
	tests := []struct {
		method   string
		path     string
		expected bool
	}{
		{"GET", "/api/v1/policy/bundles", true},
		{"GET", "/api/v1/policy/rules", true},
		{"GET", "/api/v1/users", true},
		{"GET", "/api/v1/auth/session", true},
		{"GET", "/api/v1/approvals", true},
		{"GET", "/api/v1/jobs", false},
		{"GET", "/api/v1/workflows", false},
		{"POST", "/api/v1/policy/bundles", false}, // Only GET
		{"GET", "/health", false},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			r := httptest.NewRequest(tt.method, tt.path, nil)
			got := isSensitiveRead(r)
			if got != tt.expected {
				t.Fatalf("isSensitiveRead(%s %s) = %v, want %v", tt.method, tt.path, got, tt.expected)
			}
		})
	}
}

// --- auditReadMiddleware tests ---

func TestAuditReadMiddleware_SensitiveAlwaysAudited(t *testing.T) {
	sender := &testAuditSender{}
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Rate 0.0 = off, but sensitive reads should still be audited.
	handler := auditReadMiddleware(sender, 0.0, inner)

	r := httptest.NewRequest("GET", "/api/v1/policy/bundles", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if sender.Len() != 1 {
		t.Fatalf("expected 1 audit event for sensitive read, got %d", sender.Len())
	}
	event := sender.Get(0)
	if event.Action != "data.read" {
		t.Fatalf("expected action data.read, got %s", event.Action)
	}
	if event.Extra["mandatory"] != "true" {
		t.Fatalf("expected mandatory=true for sensitive read")
	}
}

func TestAuditReadMiddleware_NonSensitiveSkippedAtZeroRate(t *testing.T) {
	sender := &testAuditSender{}
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := auditReadMiddleware(sender, 0.0, inner)

	// Non-sensitive GET should NOT be audited at rate 0.0.
	for i := 0; i < 20; i++ {
		r := httptest.NewRequest("GET", "/api/v1/jobs", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
	}

	if sender.Len() != 0 {
		t.Fatalf("expected 0 audit events at rate 0.0, got %d", sender.Len())
	}
}

func TestAuditReadMiddleware_FullRateAuditsAll(t *testing.T) {
	sender := &testAuditSender{}
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := auditReadMiddleware(sender, 1.0, inner)

	for i := 0; i < 10; i++ {
		r := httptest.NewRequest("GET", "/api/v1/jobs", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
	}

	if sender.Len() != 10 {
		t.Fatalf("expected 10 audit events at rate 1.0, got %d", sender.Len())
	}
}

func TestAuditReadMiddleware_PostNotAudited(t *testing.T) {
	sender := &testAuditSender{}
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := auditReadMiddleware(sender, 1.0, inner)

	// POST should never be audited by the read middleware.
	r := httptest.NewRequest("POST", "/api/v1/jobs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if sender.Len() != 0 {
		t.Fatalf("expected 0 events for POST, got %d", sender.Len())
	}
}

func TestAuditReadMiddleware_NilSenderPassthrough(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := auditReadMiddleware(nil, 1.0, inner)

	r := httptest.NewRequest("GET", "/api/v1/policy/bundles", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if !called {
		t.Fatal("expected inner handler to be called")
	}
}

func TestAuditReadMiddleware_NoPasswordsInEvents(t *testing.T) {
	sender := &testAuditSender{}
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := auditReadMiddleware(sender, 1.0, inner)

	r := httptest.NewRequest("GET", "/api/v1/policy/bundles", nil)
	r.Header.Set("Authorization", "Bearer secret-token-123")
	r.Header.Set("X-API-Key", "sk-secret-key-456")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if sender.Len() != 1 {
		t.Fatalf("expected 1 event, got %d", sender.Len())
	}
	event := sender.Get(0)
	// Verify no sensitive data leaked into the event.
	for key, val := range event.Extra {
		if val == "secret-token-123" || val == "sk-secret-key-456" {
			t.Fatalf("sensitive data leaked in event.Extra[%s]", key)
		}
	}
	if event.Identity == "secret-token-123" || event.Identity == "sk-secret-key-456" {
		t.Fatal("sensitive data leaked in event.Identity")
	}
}
