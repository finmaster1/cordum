package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
)

// helper: create a handler writing to a buffer for inspection.
func testHandler(t *testing.T, component string) (*bytes.Buffer, *RedactingHandler) {
	t.Helper()
	var buf bytes.Buffer
	h := newHandler(component, &buf)
	return &buf, h
}

// ---------- 1. Level filtering ----------

func TestLevelFiltering_DebugSuppressedAtInfo(t *testing.T) {
	t.Setenv("CORDUM_LOG_LEVEL", "info")
	t.Setenv("CORDUM_LOG_FORMAT", "text")

	buf, h := testHandler(t, "test")
	slog.New(h).Debug("should not appear")
	if buf.Len() != 0 {
		t.Fatalf("debug log should be suppressed at info level, got: %s", buf.String())
	}
}

func TestLevelFiltering_DebugVisibleAtDebug(t *testing.T) {
	t.Setenv("CORDUM_LOG_LEVEL", "debug")
	t.Setenv("CORDUM_LOG_FORMAT", "text")

	buf, h := testHandler(t, "test")
	slog.New(h).Debug("visible")
	if !strings.Contains(buf.String(), "visible") {
		t.Fatalf("debug log should appear at debug level, got: %s", buf.String())
	}
}

func TestLevelFiltering_WarnSuppressesInfo(t *testing.T) {
	t.Setenv("CORDUM_LOG_LEVEL", "warn")
	t.Setenv("CORDUM_LOG_FORMAT", "text")

	buf, h := testHandler(t, "test")
	logger := slog.New(h)
	logger.Info("should not appear")
	if buf.Len() != 0 {
		t.Fatalf("info log should be suppressed at warn level, got: %s", buf.String())
	}
	logger.Warn("visible")
	if !strings.Contains(buf.String(), "visible") {
		t.Fatalf("warn log should appear at warn level, got: %s", buf.String())
	}
}

func TestLevelFiltering_ErrorOnly(t *testing.T) {
	t.Setenv("CORDUM_LOG_LEVEL", "error")
	t.Setenv("CORDUM_LOG_FORMAT", "text")

	buf, h := testHandler(t, "test")
	logger := slog.New(h)
	logger.Warn("nope")
	if buf.Len() != 0 {
		t.Fatalf("warn should be suppressed at error level, got: %s", buf.String())
	}
	logger.Error("boom")
	if !strings.Contains(buf.String(), "boom") {
		t.Fatalf("error log should appear at error level, got: %s", buf.String())
	}
}

func TestLevelFiltering_DefaultIsInfo(t *testing.T) {
	t.Setenv("CORDUM_LOG_LEVEL", "")
	t.Setenv("CORDUM_LOG_FORMAT", "text")

	buf, h := testHandler(t, "test")
	logger := slog.New(h)
	logger.Debug("hidden")
	if buf.Len() != 0 {
		t.Fatalf("debug should be suppressed at default level, got: %s", buf.String())
	}
	logger.Info("shown")
	if !strings.Contains(buf.String(), "shown") {
		t.Fatalf("info should appear at default level, got: %s", buf.String())
	}
}

// ---------- 2. Format switching ----------

func TestFormatText_Shape(t *testing.T) {
	t.Setenv("CORDUM_LOG_LEVEL", "info")
	t.Setenv("CORDUM_LOG_FORMAT", "text")

	buf, h := testHandler(t, "gateway")
	slog.New(h).Info("hello", "key", "val")

	got := strings.TrimSpace(buf.String())
	// Required: [GATEWAY] INFO hello key=val
	if !strings.HasPrefix(got, "[GATEWAY] INFO hello") {
		t.Fatalf("text must start with [GATEWAY] INFO hello, got: %s", got)
	}
	if !strings.Contains(got, " key=val") {
		t.Fatalf("expected key=val in text output: %s", got)
	}
	// Must NOT use slog.TextHandler's time=/level=/msg= format.
	for _, banned := range []string{"time=", "level=", "msg="} {
		if strings.Contains(got, banned) {
			t.Fatalf("text output must not contain %q: %s", banned, got)
		}
	}
	// Component is in the [GATEWAY] prefix, not as a key=value attr.
	if strings.Contains(got, "component=") {
		t.Fatalf("component must be in prefix, not as attr: %s", got)
	}
}

func TestFormatJSON_Shape(t *testing.T) {
	t.Setenv("CORDUM_LOG_LEVEL", "info")
	t.Setenv("CORDUM_LOG_FORMAT", "json")

	buf, h := testHandler(t, "gateway")
	slog.New(h).Info("hello", "key", "val")

	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON, got: %s", buf.String())
	}
	if payload["msg"] != "hello" {
		t.Fatalf("unexpected msg: %v", payload["msg"])
	}
	if payload["component"] != "gateway" {
		t.Fatalf("expected component=gateway, got: %v", payload["component"])
	}
	if payload["key"] != "val" {
		t.Fatalf("expected key=val, got: %v", payload["key"])
	}
	// Exactly one "component" key — no duplicates.
	raw := buf.String()
	count := strings.Count(raw, `"component"`)
	if count != 1 {
		t.Fatalf("expected exactly 1 component key, got %d: %s", count, raw)
	}
}

// ---------- 3. Redaction ----------

func TestRedactionText(t *testing.T) {
	t.Setenv("CORDUM_LOG_LEVEL", "info")
	t.Setenv("CORDUM_LOG_FORMAT", "text")

	buf, h := testHandler(t, "test")
	slog.New(h).Info("login", "password", "s3cret", "user", "alice")

	got := buf.String()
	if strings.Contains(got, "s3cret") {
		t.Fatalf("sensitive value leaked: %s", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("expected [REDACTED]: %s", got)
	}
	if !strings.Contains(got, "user=alice") {
		t.Fatalf("non-sensitive value should appear: %s", got)
	}
}

func TestRedactionJSON(t *testing.T) {
	t.Setenv("CORDUM_LOG_LEVEL", "info")
	t.Setenv("CORDUM_LOG_FORMAT", "json")

	buf, h := testHandler(t, "test")
	slog.New(h).Error("auth fail", "api_key", "tok_abc123", "status", 401)

	if strings.Contains(buf.String(), "tok_abc123") {
		t.Fatalf("sensitive value leaked into JSON: %s", buf.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON: %s", buf.String())
	}
	if payload["api_key"] != "[REDACTED]" {
		t.Fatalf("expected api_key=[REDACTED], got: %v", payload["api_key"])
	}
}

func TestRedactionAllSensitiveKeys(t *testing.T) {
	t.Setenv("CORDUM_LOG_LEVEL", "info")
	t.Setenv("CORDUM_LOG_FORMAT", "text")

	keys := []string{"password", "user_password", "api_key", "apikey",
		"secret", "auth_token", "credential", "passwd"}
	for _, key := range keys {
		var buf bytes.Buffer
		h := newHandler("test", &buf)
		slog.New(h).Info("check", key, "sensitive_value_123")
		if strings.Contains(buf.String(), "sensitive_value_123") {
			t.Errorf("sensitiveKey(%q): value leaked into output", key)
		}
	}
}

// ---------- 4. Component tagging ----------

func TestComponentInText(t *testing.T) {
	t.Setenv("CORDUM_LOG_LEVEL", "info")
	t.Setenv("CORDUM_LOG_FORMAT", "text")

	buf, h := testHandler(t, "scheduler")
	slog.New(h).Info("tick")
	got := strings.TrimSpace(buf.String())
	if !strings.HasPrefix(got, "[SCHEDULER] INFO tick") {
		t.Fatalf("expected [SCHEDULER] INFO tick, got: %s", got)
	}
}

func TestComponentInJSON(t *testing.T) {
	t.Setenv("CORDUM_LOG_LEVEL", "info")
	t.Setenv("CORDUM_LOG_FORMAT", "json")

	buf, h := testHandler(t, "scheduler")
	slog.New(h).Info("tick")
	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON: %s", buf.String())
	}
	if payload["component"] != "scheduler" {
		t.Fatalf("expected component=scheduler, got: %v", payload["component"])
	}
}

// ---------- 5. WithAttrs / WithGroup ----------

func TestWithAttrsRedaction(t *testing.T) {
	t.Setenv("CORDUM_LOG_LEVEL", "info")
	t.Setenv("CORDUM_LOG_FORMAT", "text")

	buf, h := testHandler(t, "test")
	child := slog.New(h.WithAttrs([]slog.Attr{
		slog.String("token", "secret123"),
		slog.String("request_id", "abc"),
	}))
	child.Info("check")

	got := buf.String()
	if strings.Contains(got, "secret123") {
		t.Fatalf("WithAttrs should redact: %s", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] for token: %s", got)
	}
	if !strings.Contains(got, "request_id=abc") {
		t.Fatalf("expected request_id=abc: %s", got)
	}
}

func TestWithGroupJSON_ComponentTopLevel(t *testing.T) {
	t.Setenv("CORDUM_LOG_LEVEL", "info")
	t.Setenv("CORDUM_LOG_FORMAT", "json")

	buf, h := testHandler(t, "gateway")
	child := slog.New(h.WithGroup("auth"))
	child.Info("check", "password", "hunter2", "user", "bob")

	if strings.Contains(buf.String(), "hunter2") {
		t.Fatalf("WithGroup child should redact: %s", buf.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON: %s", buf.String())
	}
	// Component MUST be top-level, not nested under the group.
	if payload["component"] != "gateway" {
		t.Fatalf("component must be top-level, got payload: %v", payload)
	}
	// User attrs must be under the "auth" group.
	group, ok := payload["auth"].(map[string]any)
	if !ok {
		t.Fatalf("expected auth group object, got: %v", payload)
	}
	if group["password"] != "[REDACTED]" {
		t.Fatalf("expected password=[REDACTED] in group, got: %v", group["password"])
	}
	if group["user"] != "bob" {
		t.Fatalf("expected user=bob in group, got: %v", group["user"])
	}
	// Component must NOT appear inside the group.
	if _, exists := group["component"]; exists {
		t.Fatalf("component must not be inside the auth group: %v", group)
	}
}

func TestWithGroupText_DotPrefix(t *testing.T) {
	t.Setenv("CORDUM_LOG_LEVEL", "info")
	t.Setenv("CORDUM_LOG_FORMAT", "text")

	buf, h := testHandler(t, "gateway")
	child := slog.New(h.WithGroup("auth"))
	child.Info("check", "user", "bob")

	got := strings.TrimSpace(buf.String())
	// Text format with group: attrs are dot-prefixed.
	if !strings.Contains(got, "auth.user=bob") {
		t.Fatalf("expected auth.user=bob, got: %s", got)
	}
	if !strings.HasPrefix(got, "[GATEWAY] INFO check") {
		t.Fatalf("expected [GATEWAY] INFO check prefix, got: %s", got)
	}
}

// ---------- 6. Init reads env vars ----------

func TestInitSetsDefault(t *testing.T) {
	t.Setenv("CORDUM_LOG_LEVEL", "info")
	t.Setenv("CORDUM_LOG_FORMAT", "text")

	logger := Init("init-test")
	if logger == nil {
		t.Fatal("Init should return non-nil logger")
	}
	h := slog.Default().Handler()
	if _, ok := h.(*RedactingHandler); !ok {
		t.Fatalf("expected RedactingHandler as default, got: %T", h)
	}
}

func TestInitRespectsLevel(t *testing.T) {
	t.Setenv("CORDUM_LOG_LEVEL", "error")
	t.Setenv("CORDUM_LOG_FORMAT", "text")

	Init("test")
	h := slog.Default().Handler()
	rh, ok := h.(*RedactingHandler)
	if !ok {
		t.Fatalf("expected RedactingHandler, got: %T", h)
	}
	if rh.level != slog.LevelError {
		t.Fatalf("expected error level, got: %v", rh.level)
	}
}

// ---------- 7. Logger returns component logger ----------

func TestLoggerComponent(t *testing.T) {
	t.Setenv("CORDUM_LOG_LEVEL", "info")
	t.Setenv("CORDUM_LOG_FORMAT", "text")

	logger := Logger("my-service")
	if logger == nil {
		t.Fatal("Logger should return non-nil")
	}
	h := logger.Handler()
	rh, ok := h.(*RedactingHandler)
	if !ok {
		t.Fatalf("expected RedactingHandler, got: %T", h)
	}
	if rh.component != "my-service" {
		t.Fatalf("expected component=my-service, got: %s", rh.component)
	}
}

// ---------- 8. Backward compat ----------

func TestBackwardCompatInfo(t *testing.T) {
	t.Setenv("CORDUM_LOG_LEVEL", "info")
	t.Setenv("CORDUM_LOG_FORMAT", "text")

	var buf bytes.Buffer
	h := newHandler("compat", &buf)
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil))) })

	Info("worker", "hello", "key", "val")
	got := strings.TrimSpace(buf.String())
	// The wrapper overrides component to "worker".
	if !strings.HasPrefix(got, "[WORKER] INFO hello") {
		t.Fatalf("expected [WORKER] INFO hello, got: %s", got)
	}
	if !strings.Contains(got, "key=val") {
		t.Fatalf("expected key=val: %s", got)
	}
}

func TestBackwardCompatWarn(t *testing.T) {
	t.Setenv("CORDUM_LOG_LEVEL", "info")
	t.Setenv("CORDUM_LOG_FORMAT", "text")

	var buf bytes.Buffer
	h := newHandler("compat", &buf)
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil))) })

	Warn("worker", "slow", "latency", "500ms")
	got := buf.String()
	if !strings.Contains(got, "slow") || !strings.Contains(got, "latency=500ms") {
		t.Fatalf("backward-compat Warn failed: %s", got)
	}
}

func TestBackwardCompatError(t *testing.T) {
	t.Setenv("CORDUM_LOG_LEVEL", "info")
	t.Setenv("CORDUM_LOG_FORMAT", "text")

	var buf bytes.Buffer
	h := newHandler("compat", &buf)
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil))) })

	Error("gateway", "boom", "code", 500)
	got := buf.String()
	if !strings.Contains(got, "boom") {
		t.Fatalf("backward-compat Error failed: %s", got)
	}
}

func TestBackwardCompatRedaction(t *testing.T) {
	t.Setenv("CORDUM_LOG_LEVEL", "info")
	t.Setenv("CORDUM_LOG_FORMAT", "text")

	var buf bytes.Buffer
	h := newHandler("compat", &buf)
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil))) })

	Info("test", "check redaction", "password", "s3cret", "user", "alice")
	got := buf.String()
	if strings.Contains(got, "s3cret") {
		t.Fatalf("backward-compat should redact: %s", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("expected [REDACTED]: %s", got)
	}
	if !strings.Contains(got, "user=alice") {
		t.Fatalf("expected user=alice: %s", got)
	}
}

func TestBackwardCompatJSON_NoDuplicateComponent(t *testing.T) {
	t.Setenv("CORDUM_LOG_LEVEL", "info")
	t.Setenv("CORDUM_LOG_FORMAT", "json")

	var buf bytes.Buffer
	h := newHandler("compat", &buf)
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil))) })

	Error("gateway", "boom", "code", 500)
	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON: %s", buf.String())
	}
	if payload["msg"] != "boom" {
		t.Fatalf("unexpected msg: %v", payload["msg"])
	}
	// Must have the WRAPPER's component (gateway), not the handler's (compat).
	if payload["component"] != "gateway" {
		t.Fatalf("expected component=gateway, got: %v", payload["component"])
	}
	// Exactly one "component" key — the critical duplicate check.
	raw := buf.String()
	count := strings.Count(raw, `"component"`)
	if count != 1 {
		t.Fatalf("backward-compat must produce exactly 1 component key, got %d: %s", count, raw)
	}
}

func TestBackwardCompatText_ComponentOverride(t *testing.T) {
	t.Setenv("CORDUM_LOG_LEVEL", "info")
	t.Setenv("CORDUM_LOG_FORMAT", "text")

	var buf bytes.Buffer
	h := newHandler("init-component", &buf)
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil))) })

	Info("worker", "hello")
	got := strings.TrimSpace(buf.String())
	// Must use the wrapper's component (worker), not init-component.
	if !strings.HasPrefix(got, "[WORKER] INFO hello") {
		t.Fatalf("expected [WORKER] prefix, got: %s", got)
	}
	// Must NOT contain init-component.
	if strings.Contains(strings.ToLower(got), "init-component") {
		t.Fatalf("init-component should not appear: %s", got)
	}
}

// ---------- sensitiveKey unit tests ----------

func TestSensitiveKey(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"password", true},
		{"user_password", true},
		{"api_key", true},
		{"apikey", true},
		{"secret", true},
		{"auth_token", true},
		{"credential", true},
		{"user", false},
		{"status", false},
		{"error", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := sensitiveKey(tc.key); got != tc.want {
			t.Errorf("sensitiveKey(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}

// ---------- parseLevel ----------

func TestParseLevel(t *testing.T) {
	cases := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},
		{"invalid", slog.LevelInfo},
	}
	for _, tc := range cases {
		if got := parseLevel(tc.input); got != tc.want {
			t.Errorf("parseLevel(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
