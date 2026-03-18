package logging

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// sensitiveKeys lists substrings that mark an attribute key as secret.
var sensitiveKeys = []string{
	"password", "passwd", "secret", "token",
	"api_key", "apikey", "credential", "auth",
}

// ---------- public API ----------

// Init creates a RedactingHandler for the given component, sets it as
// slog.Default(), and returns the logger. It reads:
//   - CORDUM_LOG_LEVEL  (debug|info|warn|error, default info)
//   - CORDUM_LOG_FORMAT (text|json, default text)
func Init(component string) *slog.Logger {
	h := newHandler(component, os.Stderr)
	l := slog.New(h)
	slog.SetDefault(l)
	return l
}

// Logger returns a new *slog.Logger with the component attribute baked in,
// using the same env-driven level and format as Init. It does NOT call
// slog.SetDefault.
func Logger(component string) *slog.Logger {
	return slog.New(newHandler(component, os.Stderr))
}

// ---------- backward-compat wrappers (task rail: keep until task 3 migration) ----------

// Info logs at INFO level through slog.Default().
func Info(component, msg string, kv ...any) {
	logCompat(slog.LevelInfo, component, msg, kv...)
}

// Warn logs at WARN level through slog.Default().
func Warn(component, msg string, kv ...any) {
	logCompat(slog.LevelWarn, component, msg, kv...)
}

// Error logs at ERROR level through slog.Default().
func Error(component, msg string, kv ...any) {
	logCompat(slog.LevelError, component, msg, kv...)
}

// logCompat routes a legacy-signature call through the current default handler,
// overriding the component to match the caller's intent without producing
// duplicate component fields.
func logCompat(level slog.Level, component, msg string, kv ...any) {
	h := slog.Default().Handler()
	if rh, ok := h.(*RedactingHandler); ok {
		slog.New(rh.withComponent(component)).Log(context.Background(), level, msg, kv...)
		return
	}
	// Fallback for non-RedactingHandler (e.g., default slog before Init is called).
	slog.Default().With("component", component).Log(context.Background(), level, msg, kv...)
}

// ---------- handler construction ----------

func newHandler(component string, w io.Writer) *RedactingHandler {
	lvl := parseLevel(os.Getenv("CORDUM_LOG_LEVEL"))
	format := strings.ToLower(strings.TrimSpace(os.Getenv("CORDUM_LOG_FORMAT")))
	if format != "json" {
		format = "text"
	}

	opts := &slog.HandlerOptions{Level: lvl}

	var inner slog.Handler
	if format == "json" {
		inner = slog.NewJSONHandler(w, opts)
	}

	return &RedactingHandler{
		inner:     inner,
		level:     lvl,
		component: component,
		format:    format,
		writer:    w,
		mu:        &sync.Mutex{},
	}
}

func parseLevel(raw string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default: // "info" or unrecognised
		return slog.LevelInfo
	}
}

// ---------- RedactingHandler ----------

// RedactingHandler is a slog.Handler that redacts sensitive attribute values
// and injects a component name into every record.
type RedactingHandler struct {
	inner     slog.Handler // non-nil for JSON mode; nil for text mode
	level     slog.Level
	component string
	format    string    // "text" or "json"
	writer    io.Writer // output destination (used directly in text mode)
	preAttrs  []slog.Attr
	groups    []string
	mu        *sync.Mutex // protects writer in text mode; shared across copies
}

// Enabled reports whether the handler handles records at the given level.
func (h *RedactingHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

// Handle redacts sensitive attrs and delegates to the appropriate formatter.
func (h *RedactingHandler) Handle(ctx context.Context, r slog.Record) error {
	// Build redacted record: preAttrs first, then redacted record attrs.
	r2 := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r2.AddAttrs(h.preAttrs...)
	r.Attrs(func(a slog.Attr) bool {
		r2.AddAttrs(redactAttr(a))
		return true
	})

	if h.format == "json" {
		return h.handleJSON(ctx, r2)
	}
	return h.handleText(r2)
}

// handleJSON delegates to the inner slog.JSONHandler with component injected
// at top level (before any groups) so it never nests under a group.
func (h *RedactingHandler) handleJSON(ctx context.Context, r slog.Record) error {
	inner := h.inner.WithAttrs([]slog.Attr{slog.String("component", h.component)})
	for _, g := range h.groups {
		inner = inner.WithGroup(g)
	}
	return inner.Handle(ctx, r)
}

// handleText writes log lines in the format:
//
//	[COMPONENT] LEVEL msg key=value ...
func (h *RedactingHandler) handleText(r slog.Record) error {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "[%s] %s %s",
		strings.ToUpper(h.component),
		r.Level.String(),
		r.Message,
	)

	prefix := strings.Join(h.groups, ".")
	if prefix != "" {
		prefix += "."
	}

	r.Attrs(func(a slog.Attr) bool {
		writeTextAttr(&buf, prefix, a)
		return true
	})

	buf.WriteByte('\n')

	h.mu.Lock()
	_, err := h.writer.Write(buf.Bytes())
	h.mu.Unlock()
	return err
}

// writeTextAttr appends a single attr in key=value format, recursing into groups.
func writeTextAttr(buf *bytes.Buffer, prefix string, a slog.Attr) {
	if a.Equal(slog.Attr{}) {
		return
	}
	if a.Value.Kind() == slog.KindGroup {
		gprefix := prefix
		if a.Key != "" {
			gprefix = prefix + a.Key + "."
		}
		for _, ga := range a.Value.Group() {
			writeTextAttr(buf, gprefix, ga)
		}
		return
	}
	buf.WriteByte(' ')
	buf.WriteString(prefix)
	buf.WriteString(a.Key)
	buf.WriteByte('=')
	buf.WriteString(a.Value.String())
}

// WithAttrs returns a new handler whose output includes the given attrs
// (values redacted where needed).
func (h *RedactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = redactAttr(a)
	}
	return &RedactingHandler{
		inner:     h.inner,
		level:     h.level,
		component: h.component,
		format:    h.format,
		writer:    h.writer,
		preAttrs:  append(cloneAttrs(h.preAttrs), redacted...),
		groups:    cloneStrings(h.groups),
		mu:        h.mu,
	}
}

// WithGroup returns a new handler that opens a group.
func (h *RedactingHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &RedactingHandler{
		inner:     h.inner,
		level:     h.level,
		component: h.component,
		format:    h.format,
		writer:    h.writer,
		preAttrs:  cloneAttrs(h.preAttrs),
		groups:    append(cloneStrings(h.groups), name),
		mu:        h.mu,
	}
}

// withComponent returns a shallow copy with a different component name.
// Used by backward-compat wrappers to override component without producing
// duplicate component fields.
func (h *RedactingHandler) withComponent(c string) *RedactingHandler {
	return &RedactingHandler{
		inner:     h.inner,
		level:     h.level,
		component: c,
		format:    h.format,
		writer:    h.writer,
		preAttrs:  cloneAttrs(h.preAttrs),
		groups:    cloneStrings(h.groups),
		mu:        h.mu,
	}
}

// ---------- redaction helpers ----------

func redactAttr(a slog.Attr) slog.Attr {
	// Recurse into groups.
	if a.Value.Kind() == slog.KindGroup {
		attrs := a.Value.Group()
		redacted := make([]slog.Attr, len(attrs))
		for i, ga := range attrs {
			redacted[i] = redactAttr(ga)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(redacted...)}
	}
	if sensitiveKey(a.Key) {
		return slog.String(a.Key, "[REDACTED]")
	}
	return a
}

// sensitiveKey returns true if the key name suggests a secret or credential.
func sensitiveKey(key string) bool {
	k := strings.ToLower(key)
	for _, s := range sensitiveKeys {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// ---------- slice helpers ----------

func cloneAttrs(a []slog.Attr) []slog.Attr {
	if a == nil {
		return nil
	}
	out := make([]slog.Attr, len(a))
	copy(out, a)
	return out
}

func cloneStrings(s []string) []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
}
