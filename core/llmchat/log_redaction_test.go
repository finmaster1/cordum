package llmchat

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSessionLoggerIncludesSafeCorrelationKeys(t *testing.T) {
	var buf bytes.Buffer
	restore := installJSONTestLogger(t, &buf)
	defer restore()

	ctx := contextWithTraceID(context.Background(), "trace-123")
	sessionLogger(ctx, &Session{
		ID:            "session-123",
		UserPrincipal: "user-a",
		Tenant:        "tenant-a",
	}).Info("llmchat/agent: turn_start")

	obj := decodeSingleLogRecord(t, buf.String())
	for key, want := range map[string]string{
		logFieldSessionID:     "session-123",
		logFieldUserPrincipal: "user-a",
		logFieldTenant:        "tenant-a",
		logFieldTraceID:       "trace-123",
	} {
		if got := obj[key]; got != want {
			t.Fatalf("%s = %v, want %q in log record %v", key, got, want, obj)
		}
	}
}

func TestSessionLoggerFallsBackToSessionIDTraceID(t *testing.T) {
	var buf bytes.Buffer
	restore := installJSONTestLogger(t, &buf)
	defer restore()

	sessionLogger(context.Background(), &Session{
		ID:            "session-fallback",
		UserPrincipal: "user-a",
		Tenant:        "tenant-a",
	}).Info("llmchat/agent: turn_start")

	obj := decodeSingleLogRecord(t, buf.String())
	if got := obj[logFieldTraceID]; got != "session-fallback" {
		t.Fatalf("trace_id = %v, want session fallback", got)
	}
}

func TestSessionLoggerRedactsSecretLikeCorrelationValues(t *testing.T) {
	var buf bytes.Buffer
	restore := installJSONTestLogger(t, &buf)
	defer restore()

	rawHexSecret := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	ctx := contextWithTraceID(context.Background(), rawHexSecret)
	sessionLogger(ctx, &Session{
		ID:            "session-123",
		UserPrincipal: "Bearer bearer-super-secret-value",
		Tenant:        "CORDUM_API_KEY=cordum-api-key-super-secret",
	}).Info("llmchat/agent: turn_start")

	raw := buf.String()
	for _, forbidden := range []string{
		rawHexSecret,
		"bearer-super-secret-value",
		"cordum-api-key-super-secret",
	} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("log record leaked forbidden token %q: %s", forbidden, raw)
		}
	}
	obj := decodeSingleLogRecord(t, raw)
	for _, key := range []string{logFieldUserPrincipal, logFieldTenant, logFieldTraceID} {
		if got := obj[key]; got != "[REDACTED]" {
			t.Fatalf("%s = %v, want [REDACTED] in log record %v", key, got, obj)
		}
	}
}

func TestTraceIDFromRequestPrefersTraceparentAndRejectsSecretLikeHeaders(t *testing.T) {
	req := httptestNewRequest(t)
	req.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	req.Header.Set("X-Request-Id", "Bearer bearer-super-secret-value")

	if got := traceIDFromRequest(req, "fallback-session"); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("traceIDFromRequest = %q, want traceparent trace id", got)
	}

	req.Header.Del("Traceparent")
	if got := traceIDFromRequest(req, "fallback-session"); got != "[REDACTED]" {
		t.Fatalf("secret-like X-Request-Id = %q, want [REDACTED]", got)
	}
}

func installJSONTestLogger(t *testing.T, buf *bytes.Buffer) func() {
	t.Helper()
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return func() { slog.SetDefault(orig) }
}

func decodeSingleLogRecord(t *testing.T, raw string) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d log lines, want 1: %q", len(lines), raw)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &obj); err != nil {
		t.Fatalf("log line is not JSON: %v\n%s", err, lines[0])
	}
	return obj
}

func httptestNewRequest(t *testing.T) *http.Request {
	t.Helper()
	return httptest.NewRequest(http.MethodGet, "/api/v1/chat", nil)
}
