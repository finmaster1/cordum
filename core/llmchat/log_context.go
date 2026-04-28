package llmchat

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"unicode"
)

const (
	logFieldSessionID     = "session_id"
	logFieldUserPrincipal = "user_principal"
	logFieldTenant        = "tenant"
	logFieldTraceID       = "trace_id"

	maxLogCorrelationValueLen = 128
)

type traceIDContextKey struct{}

var (
	traceparentRE              = regexp.MustCompile(`^[[:xdigit:]]{2}-([[:xdigit:]]{32})-[[:xdigit:]]{16}-[[:xdigit:]]{2}$`)
	secretLikeLogValuePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bBearer\s+\S+`),
		regexp.MustCompile(`(?i)\bX-API-Key\s*[:=]\s*\S+`),
		regexp.MustCompile(`(?i)\bCORDUM_API_KEY\s*[:=]\s*\S+`),
		regexp.MustCompile(`\beyJ[A-Za-z0-9._-]{20,}`),
		regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{12,}`),
		regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
		regexp.MustCompile(`\b[A-Fa-f0-9]{64,}\b`),
	}
)

func contextWithTraceID(ctx context.Context, traceID string) context.Context {
	traceID = safeLogCorrelationValue(traceID)
	if traceID == "" {
		return ctx
	}
	return context.WithValue(ctx, traceIDContextKey{}, traceID)
}

func traceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	traceID, _ := ctx.Value(traceIDContextKey{}).(string)
	return safeLogCorrelationValue(traceID)
}

func contextWithRequestTraceID(ctx context.Context, r *http.Request, fallback string) context.Context {
	return contextWithTraceID(ctx, traceIDFromRequest(r, fallback))
}

func traceIDFromRequest(r *http.Request, fallback string) string {
	if r != nil {
		if traceID := traceIDFromTraceparent(r.Header.Get("Traceparent")); traceID != "" {
			return traceID
		}
		for _, header := range []string{"X-Trace-Id", "X-Request-Id", "X-Correlation-Id"} {
			if traceID := safeLogCorrelationValue(r.Header.Get(header)); traceID != "" {
				return traceID
			}
		}
	}
	return safeLogCorrelationValue(fallback)
}

func traceIDFromTraceparent(raw string) string {
	raw = strings.TrimSpace(raw)
	matches := traceparentRE.FindStringSubmatch(raw)
	if len(matches) != 2 {
		return ""
	}
	traceID := strings.ToLower(matches[1])
	if traceID == "00000000000000000000000000000000" {
		return ""
	}
	return traceID
}

func sessionLogger(ctx context.Context, session *Session) *slog.Logger {
	attrs := make([]any, 0, 8)
	if session != nil {
		attrs = appendSafeLogAttr(attrs, logFieldSessionID, session.ID)
		attrs = appendSafeLogAttr(attrs, logFieldUserPrincipal, session.UserPrincipal)
		attrs = appendSafeLogAttr(attrs, logFieldTenant, session.Tenant)
	}
	traceID := traceIDFromContext(ctx)
	if traceID == "" && session != nil {
		traceID = safeLogCorrelationValue(session.ID)
	}
	attrs = appendSafeLogAttr(attrs, logFieldTraceID, traceID)
	return slog.Default().With(attrs...)
}

func requestLogger(ctx context.Context, r *http.Request, session *Session) *slog.Logger {
	if session != nil {
		ctx = contextWithRequestTraceID(ctx, r, session.ID)
		return sessionLogger(ctx, session)
	}
	traceID := traceIDFromRequest(r, "")
	attrs := make([]any, 0, 6)
	if r != nil {
		if user := defaultUserPrincipal(r); user != "" {
			attrs = appendSafeLogAttr(attrs, logFieldUserPrincipal, user)
		}
		if tenant := defaultTenant(r); tenant != "" {
			attrs = appendSafeLogAttr(attrs, logFieldTenant, tenant)
		}
	}
	attrs = appendSafeLogAttr(attrs, logFieldTraceID, traceID)
	return slog.Default().With(attrs...)
}

func appendSafeLogAttr(attrs []any, key, raw string) []any {
	if key == "" {
		return attrs
	}
	value := safeLogCorrelationValue(raw)
	if value == "" {
		return attrs
	}
	return append(attrs, key, value)
}

func safeLogCorrelationValue(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if secretLikeLogValue(raw) {
		return "[REDACTED]"
	}
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteByte('_')
		case unicode.IsControl(r):
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
		if b.Len() >= maxLogCorrelationValueLen {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

func secretLikeLogValue(raw string) bool {
	for _, pattern := range secretLikeLogValuePatterns {
		if pattern.MatchString(raw) {
			return true
		}
	}
	return false
}
