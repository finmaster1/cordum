package gateway

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cordum/cordum/core/controlplane/gateway/auth"
	"github.com/cordum/cordum/core/licensing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestHandleLLMChatProxyHealthForwardsReadyzWithTrustedIdentity(t *testing.T) {
	s, _, _ := newTestGateway(t)
	setTestEntitlements(t, s, licensing.PlanEnterprise, func(entitlements *licensing.Entitlements) {
		entitlements.LLMChatAssistant = true
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			t.Fatalf("upstream path = %q, want /readyz", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "forward-key" {
			t.Fatalf("X-API-Key = %q, want forward-key", got)
		}
		if got := r.Header.Get("X-Cordum-Tenant"); got != "tenant-a" {
			t.Fatalf("X-Cordum-Tenant = %q, want tenant-a", got)
		}
		if got := r.Header.Get("X-Cordum-Principal"); got != "alice" {
			t.Fatalf("X-Cordum-Principal = %q, want alice", got)
		}
		if got := r.Header.Get("X-Cordum-Role"); got != "operator" {
			t.Fatalf("X-Cordum-Role = %q, want operator", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","redis":"ok","vllm":"ok"}`))
	}))
	defer upstream.Close()

	t.Setenv(envLLMChatURL, upstream.URL)
	t.Setenv(envLLMChatForwardAPIKey, "forward-key")

	authCtx := &auth.AuthContext{
		Tenant:      "tenant-a",
		PrincipalID: "alice",
		Role:        "operator",
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/healthz", nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.ContextKey{}, authCtx))
	rec := httptest.NewRecorder()

	s.handleLLMChatProxy(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != `{"status":"ok","redis":"ok","vllm":"ok"}` {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestHandleLLMChatProxyRequiresEntitlementBeforeForwarding(t *testing.T) {
	s, _, _ := newTestGateway(t)
	setTestEntitlements(t, s, licensing.PlanCommunity, func(entitlements *licensing.Entitlements) {
		entitlements.LLMChatAssistant = false
	})

	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	t.Setenv(envLLMChatURL, upstream.URL)
	t.Setenv(envLLMChatForwardAPIKey, "forward-key")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/healthz", nil)
	rec := httptest.NewRecorder()

	s.handleLLMChatProxy(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 body=%s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("upstream was called despite missing entitlement")
	}
}

func TestHandleLLMChatProxyInjectsTraceContext(t *testing.T) {
	prevPropagator := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(prevPropagator) })

	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	ctx, span := tp.Tracer("test").Start(context.Background(), "browser")
	defer span.End()

	s, _, _ := newTestGateway(t)
	setTestEntitlements(t, s, licensing.PlanEnterprise, func(entitlements *licensing.Entitlements) {
		entitlements.LLMChatAssistant = true
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceparent := r.Header.Get("Traceparent")
		if traceparent == "" {
			t.Fatal("Traceparent header missing")
		}
		if wantTrace := span.SpanContext().TraceID().String(); !strings.Contains(traceparent, wantTrace) {
			t.Fatalf("Traceparent = %q, want trace id %s", traceparent, wantTrace)
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()
	t.Setenv(envLLMChatURL, upstream.URL)
	t.Setenv(envLLMChatForwardAPIKey, "forward-key")

	authCtx := &auth.AuthContext{Tenant: "tenant-a", PrincipalID: "alice", Role: "operator"}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", strings.NewReader(`{"message":"hello"}`))
	req = req.WithContext(context.WithValue(ctx, auth.ContextKey{}, authCtx))
	rec := httptest.NewRecorder()

	s.handleLLMChatProxy(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleLLMChatProxyTrustsConfiguredUpstreamCA(t *testing.T) {
	s, _, _ := newTestGateway(t)
	setTestEntitlements(t, s, licensing.PlanEnterprise, func(entitlements *licensing.Entitlements) {
		entitlements.LLMChatAssistant = true
	})

	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	caPath := filepath.Join(t.TempDir(), "llm-chat-ca.crt")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: upstream.Certificate().Raw})
	if err := os.WriteFile(caPath, caPEM, 0o600); err != nil {
		t.Fatalf("write ca: %v", err)
	}

	t.Setenv(envLLMChatURL, upstream.URL)
	t.Setenv(envLLMChatForwardAPIKey, "forward-key")
	t.Setenv(envLLMChatTLSCA, caPath)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/healthz", nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.ContextKey{}, &auth.AuthContext{
		Tenant:      "tenant-a",
		PrincipalID: "alice",
		Role:        "operator",
	}))
	rec := httptest.NewRecorder()

	s.handleLLMChatProxy(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleLLMChatProxyFallsBackToConfiguredPrincipal(t *testing.T) {
	s, _, _ := newTestGateway(t)
	setTestEntitlements(t, s, licensing.PlanEnterprise, func(entitlements *licensing.Entitlements) {
		entitlements.LLMChatAssistant = true
	})
	t.Setenv(envFallbackPrincipalID, "dev-admin")
	t.Setenv(envFallbackPrincipalRole, "admin")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Cordum-Principal"); got != "dev-admin" {
			t.Fatalf("X-Cordum-Principal = %q, want dev-admin", got)
		}
		if got := r.Header.Get("X-Cordum-Role"); got != "viewer" {
			t.Fatalf("X-Cordum-Role = %q, want viewer from authenticated role", got)
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()
	t.Setenv(envLLMChatURL, upstream.URL)
	t.Setenv(envLLMChatForwardAPIKey, "forward-key")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/healthz", nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.ContextKey{}, &auth.AuthContext{
		Tenant: "tenant-a",
		Role:   "viewer",
	}))
	rec := httptest.NewRecorder()

	s.handleLLMChatProxy(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
}
