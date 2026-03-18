package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIdEchoedInResponse(t *testing.T) {
	s, _, _ := newTestGateway(t)
	s.tenant = "default"

	handler := requestLoggingMiddleware(http.HandlerFunc(s.handleGetWorkers))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers", nil)
	req.Header.Set("X-Request-Id", "test-req-123")
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	got := rec.Header().Get("X-Request-Id")
	if got != "test-req-123" {
		t.Fatalf("expected X-Request-Id test-req-123, got %q", got)
	}
}

func TestRequestIdGeneratedWhenMissing(t *testing.T) {
	s, _, _ := newTestGateway(t)
	s.tenant = "default"

	handler := requestLoggingMiddleware(http.HandlerFunc(s.handleGetWorkers))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	got := rec.Header().Get("X-Request-Id")
	if got == "" {
		t.Fatal("expected X-Request-Id to be generated, got empty")
	}
}

func TestJobSubmitReturnsTraceIdHeader(t *testing.T) {
	s, _, _ := newTestGateway(t)
	s.tenant = "default"

	payload := map[string]any{
		"prompt": "hello",
		"topic":  "job.test",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-Request-Id", "submit-req-456")
	rec := httptest.NewRecorder()

	s.handleSubmitJobHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body: %s", rec.Code, rec.Body.String())
	}

	traceHeader := rec.Header().Get("X-Trace-Id")
	if traceHeader == "" {
		t.Fatal("expected X-Trace-Id header, got empty")
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["trace_id"] != traceHeader {
		t.Fatalf("X-Trace-Id header %q does not match body trace_id %q", traceHeader, resp["trace_id"])
	}
}

func TestRequestIdFromContextMiddleware(t *testing.T) {
	var capturedReqID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReqID = requestIdFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := requestLoggingMiddleware(inner)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	req.Header.Set("X-Request-Id", "ctx-req-789")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if capturedReqID != "ctx-req-789" {
		t.Fatalf("expected requestIdFromContext to return ctx-req-789, got %q", capturedReqID)
	}
}

func TestCorsExposesCorrelationHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := corsMiddleware(inner)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	exposed := rec.Header().Get("Access-Control-Expose-Headers")
	if exposed == "" {
		t.Fatal("expected Access-Control-Expose-Headers, got empty")
	}
	for _, h := range []string{"X-Request-Id", "X-Trace-Id"} {
		if !containsHeader(exposed, h) {
			t.Fatalf("Access-Control-Expose-Headers missing %s, got %q", h, exposed)
		}
	}
}

func containsHeader(exposed, header string) bool {
	for _, part := range bytes.Split([]byte(exposed), []byte(",")) {
		if string(bytes.TrimSpace(part)) == header {
			return true
		}
	}
	return false
}
