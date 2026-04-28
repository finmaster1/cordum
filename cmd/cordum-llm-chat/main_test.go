package main

import (
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gatewayauth "github.com/cordum/cordum/core/controlplane/gateway/auth"
	"github.com/cordum/cordum/core/llmchat"
	"github.com/prometheus/client_golang/prometheus"
)

func fakeEnv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func TestRegisterMetricsHandlerExposesPrometheusEndpoint(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics := llmchat.NewMetrics(reg)
	metrics.IncSessions()
	metrics.ObserveVLLMLatency(250 * time.Millisecond)
	metrics.IncTokenBudgetUsed(42)
	metrics.IncError("vllm_call_failed")

	mux := http.NewServeMux()
	registerMetricsHandler(mux, reg)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := server.Client().Get(server.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /metrics status = %d, want 200", resp.StatusCode)
	}

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /metrics body: %v", err)
	}
	body := string(rawBody)
	for _, name := range []string{
		"chat_sessions_active",
		"chat_vllm_latency_seconds_bucket",
		"chat_token_budget_used_total",
		"chat_errors_total",
	} {
		if !strings.Contains(body, name) {
			t.Fatalf("/metrics body missing %q:\n%s", name, body)
		}
	}
}

func TestLoadConfigFromEnv_DefaultsToOllamaCPUBackend(t *testing.T) {
	cfg, err := loadConfigFromEnv(fakeEnv(map[string]string{
		"REDIS_URL": "redis://localhost:6379/0",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Backend != backendOllamaCPU {
		t.Fatalf("Backend = %q, want %q", cfg.Backend, backendOllamaCPU)
	}
	if cfg.ChatAssistantAgentID != "" {
		t.Errorf("ChatAssistantAgentID = %q, want empty (unpinned)", cfg.ChatAssistantAgentID)
	}
	if cfg.HTTPAddr != defaultHTTPAddr {
		t.Fatalf("HTTPAddr = %q, want %q", cfg.HTTPAddr, defaultHTTPAddr)
	}
	if cfg.Provider.Kind != defaultProvider {
		t.Fatalf("Provider.Kind = %q, want %q", cfg.Provider.Kind, defaultProvider)
	}
	if cfg.Provider.BaseURL != defaultOllamaBaseURL {
		t.Fatalf("Provider.BaseURL = %q, want %q", cfg.Provider.BaseURL, defaultOllamaBaseURL)
	}
	if cfg.Provider.Model != defaultOllamaModel {
		t.Fatalf("Provider.Model = %q, want %q", cfg.Provider.Model, defaultOllamaModel)
	}
	if cfg.Provider.ResponseTemperature != defaultResponseTemperature {
		t.Fatalf("ResponseTemperature = %v, want %v", cfg.Provider.ResponseTemperature, defaultResponseTemperature)
	}
	if cfg.Provider.ResponseTopP != defaultResponseTopP {
		t.Fatalf("ResponseTopP = %v, want %v", cfg.Provider.ResponseTopP, defaultResponseTopP)
	}
}

func TestLoadConfigFromEnv_MissingRedisURL(t *testing.T) {
	_, err := loadConfigFromEnv(fakeEnv(nil))
	if err == nil {
		t.Fatal("expected error for missing REDIS_URL, got nil")
	}
	if !strings.Contains(err.Error(), "REDIS_URL") {
		t.Fatalf("error = %v, want REDIS_URL in message", err)
	}
}

func TestLoadConfigFromEnv_VLLMGPUBackendDefaults(t *testing.T) {
	cfg, err := loadConfigFromEnv(fakeEnv(map[string]string{
		"REDIS_URL":           "redis://localhost:6379/0",
		"LLMCHAT_OPS_BACKEND": "vllm-gpu",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Backend != backendVLLMGPU {
		t.Fatalf("Backend = %q, want %q", cfg.Backend, backendVLLMGPU)
	}
	if cfg.Provider.BaseURL != defaultVLLMBaseURL {
		t.Fatalf("Provider.BaseURL = %q, want %q", cfg.Provider.BaseURL, defaultVLLMBaseURL)
	}
	if cfg.Provider.Model != defaultVLLMModel {
		t.Fatalf("Provider.Model = %q, want %q", cfg.Provider.Model, defaultVLLMModel)
	}
}

func TestLoadConfigFromEnv_RejectsInvalidBackend(t *testing.T) {
	_, err := loadConfigFromEnv(fakeEnv(map[string]string{
		"REDIS_URL":           "redis://localhost:6379/0",
		"LLMCHAT_OPS_BACKEND": "llamacpp",
	}))
	if err == nil {
		t.Fatal("expected invalid backend error, got nil")
	}
	want := "unsupported LLMCHAT_OPS_BACKEND=llamacpp; allowed: ollama-cpu, vllm-gpu"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestLoadConfigFromEnv_AllOverridesRead(t *testing.T) {
	cfg, err := loadConfigFromEnv(fakeEnv(map[string]string{
		"REDIS_URL":                       "redis://custom:6379/1",
		"CORDUM_API_KEY":                  "sekret",
		"CORDUM_GATEWAY_URL":              "https://gateway.internal:8443",
		"CORDUM_LLM_CHAT_ADDR":            ":9090",
		"CORDUM_LLM_CHAT_TLS_CERT_FILE":   "/tls/tls.crt",
		"CORDUM_LLM_CHAT_TLS_KEY_FILE":    "/tls/tls.key",
		"LLMCHAT_OPS_BACKEND":             "vllm-gpu",
		"LLMCHAT_PROVIDER":                "openai",
		"LLMCHAT_BASE_URL":                "https://vllm.internal/v1",
		"LLMCHAT_MODEL":                   "qwen3-custom",
		"LLMCHAT_API_KEY":                 "token",
		"LLMCHAT_SUMMARY_TEMPERATURE":     "0.72",
		"LLMCHAT_SUMMARY_TOP_P":           "0.81",
		"LLMCHAT_MAX_WALL_CLOCK_PER_TURN": "90s",
		"LLMCHAT_MAX_ASSISTANT_BYTES":     "65536",
		"LLMCHAT_CHAT_ASSISTANT_AGENT_ID": "chat-assistant-prod",
		"LLMCHAT_TENANT":                  "tenant-a",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HTTPAddr != ":9090" {
		t.Fatalf("HTTPAddr = %q, want :9090", cfg.HTTPAddr)
	}
	if cfg.TLSCertFile != "/tls/tls.crt" || cfg.TLSKeyFile != "/tls/tls.key" {
		t.Fatalf("TLS = %q/%q, want /tls/tls.crt /tls/tls.key", cfg.TLSCertFile, cfg.TLSKeyFile)
	}
	if cfg.CordumAPIKey != "sekret" {
		t.Fatalf("CordumAPIKey = %q, want sekret", cfg.CordumAPIKey)
	}
	if cfg.GatewayURL != "https://gateway.internal:8443" {
		t.Fatalf("GatewayURL = %q, want https://gateway.internal:8443", cfg.GatewayURL)
	}
	if cfg.Backend != backendVLLMGPU {
		t.Fatalf("Backend = %q, want %q", cfg.Backend, backendVLLMGPU)
	}
	if cfg.Provider.BaseURL != "https://vllm.internal/v1" {
		t.Fatalf("BaseURL = %q, want override", cfg.Provider.BaseURL)
	}
	if cfg.Provider.Model != "qwen3-custom" {
		t.Fatalf("Model = %q, want qwen3-custom", cfg.Provider.Model)
	}
	if cfg.Provider.APIKey != "token" {
		t.Fatalf("APIKey = %q, want token", cfg.Provider.APIKey)
	}
	if cfg.Provider.ResponseTemperature != 0.72 {
		t.Fatalf("ResponseTemperature = %v, want 0.72", cfg.Provider.ResponseTemperature)
	}
	if cfg.Provider.ResponseTopP != 0.81 {
		t.Fatalf("ResponseTopP = %v, want 0.81", cfg.Provider.ResponseTopP)
	}
	if cfg.ChatAssistantAgentID != "chat-assistant-prod" {
		t.Errorf("ChatAssistantAgentID = %q, want chat-assistant-prod", cfg.ChatAssistantAgentID)
	}
	if cfg.Tenant != "tenant-a" {
		t.Errorf("Tenant = %q, want tenant-a", cfg.Tenant)
	}
}

func TestLogActiveBackend(t *testing.T) {
	cfg := runtimeConfig{
		Backend: backendOllamaCPU,
		Provider: llmchat.ProviderConfig{
			BaseURL: defaultOllamaBaseURL,
			Model:   defaultOllamaModel,
		},
	}
	var buf strings.Builder
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(orig)

	logActiveBackend(cfg)

	got := buf.String()
	for _, want := range []string{
		`msg="llm-chat backend active"`,
		`backend=ollama-cpu`,
		`base_url=http://ollama:11434/v1`,
		`model=qwen2.5-coder:3b-instruct-q4_K_M`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log output %q missing %s", got, want)
		}
	}
}

func TestLoadConfigFromEnv_AcceptsMissingAgentIDPin(t *testing.T) {
	cfg, err := loadConfigFromEnv(fakeEnv(map[string]string{
		"REDIS_URL": "redis://localhost:6379/0",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ChatAssistantAgentID != "" {
		t.Errorf("ChatAssistantAgentID = %q, want empty (pin is optional; bootstrap resolves it)", cfg.ChatAssistantAgentID)
	}
}

func TestLoadConfigFromEnv_TLSPairMustMatch(t *testing.T) {
	cases := []map[string]string{
		{
			"REDIS_URL":                     "redis://localhost:6379/0",
			"CORDUM_LLM_CHAT_TLS_CERT_FILE": "/tls/tls.crt",
		},
		{
			"REDIS_URL":                    "redis://localhost:6379/0",
			"CORDUM_LLM_CHAT_TLS_KEY_FILE": "/tls/tls.key",
		},
	}
	for _, env := range cases {
		if _, err := loadConfigFromEnv(fakeEnv(env)); err == nil {
			t.Fatalf("expected TLS pair error for env=%v", env)
		}
	}
}

func TestLoadConfigFromEnv_RejectsMalformedNumbers(t *testing.T) {
	cases := map[string]string{
		"LLMCHAT_SUMMARY_TEMPERATURE":     "inf!",
		"LLMCHAT_SUMMARY_TOP_P":           "oops",
		"LLMCHAT_MAX_WALL_CLOCK_PER_TURN": "sixty",
		"LLMCHAT_MAX_ASSISTANT_BYTES":     "big",
	}
	for key, bad := range cases {
		env := map[string]string{
			"REDIS_URL": "redis://localhost:6379/0",
			key:         bad,
		}
		if _, err := loadConfigFromEnv(fakeEnv(env)); err == nil {
			t.Fatalf("expected parse error for %s=%q, got nil", key, bad)
		}
	}
}

func TestEnvHelpersFallback(t *testing.T) {
	get := fakeEnv(map[string]string{
		"FILLED": "  custom  ",
	})

	if got := envOrDefault(get, "FILLED", "fallback"); got != "custom" {
		t.Fatalf("envOrDefault(FILLED) = %q, want custom", got)
	}
	if got := envOrDefault(get, "MISSING", "fallback"); got != "fallback" {
		t.Fatalf("envOrDefault(MISSING) = %q, want fallback", got)
	}

	if got, err := envFloatOrDefault(get, "MISSING", 1.5); err != nil || got != 1.5 {
		t.Fatalf("envFloatOrDefault(MISSING) = %v, %v, want 1.5, nil", got, err)
	}
	if got, err := envIntOrDefault(get, "MISSING", 7); err != nil || got != 7 {
		t.Fatalf("envIntOrDefault(MISSING) = %d, %v, want 7, nil", got, err)
	}
	if got, err := envDurationOrDefault(get, "MISSING", 3*time.Second); err != nil || got != 3*time.Second {
		t.Fatalf("envDurationOrDefault(MISSING) = %v, %v, want 3s, nil", got, err)
	}
}

func TestRedisOptionsFromURLAppliesTLSFromEnv(t *testing.T) {
	t.Setenv("REDIS_TLS_CA", "")
	t.Setenv("REDIS_TLS_CERT", "")
	t.Setenv("REDIS_TLS_KEY", "")
	t.Setenv("REDIS_TLS_INSECURE", "")
	t.Setenv("REDIS_TLS_SERVER_NAME", "redis")

	opts, err := redisOptionsFromURL("rediss://:secret@redis:6379/0")
	if err != nil {
		t.Fatalf("redisOptionsFromURL: %v", err)
	}
	if opts.TLSConfig == nil {
		t.Fatal("TLSConfig nil; REDIS_TLS_* env was not applied")
	}
	if opts.TLSConfig.ServerName != "redis" {
		t.Fatalf("TLSConfig.ServerName = %q, want redis", opts.TLSConfig.ServerName)
	}
}

func TestGatewayHTTPClientFromEnvTrustsConfiguredCA(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	caPath := filepath.Join(t.TempDir(), "ca.crt")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(caPath, caPEM, 0o600); err != nil {
		t.Fatalf("write ca: %v", err)
	}

	client, err := gatewayHTTPClientFromEnv(fakeEnv(map[string]string{
		envCordumTLSCA: caPath,
	}), time.Second)
	if err != nil {
		t.Fatalf("gatewayHTTPClientFromEnv: %v", err)
	}
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("GET with configured CA: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestGatewayHTTPClientFromEnvMissingCAErrors(t *testing.T) {
	_, err := gatewayHTTPClientFromEnv(fakeEnv(map[string]string{
		envCordumTLSCA: filepath.Join(t.TempDir(), "missing-ca.crt"),
	}), time.Second)
	if err == nil {
		t.Fatal("expected missing CORDUM_TLS_CA error, got nil")
	}
	if !strings.Contains(err.Error(), envCordumTLSCA) {
		t.Fatalf("error = %v, want %s", err, envCordumTLSCA)
	}
}

func TestGatewayHTTPClientFromEnvNegativeTimeoutDisablesDeadline(t *testing.T) {
	client, err := gatewayHTTPClientFromEnv(fakeEnv(map[string]string{}), -1)
	if err != nil {
		t.Fatalf("gatewayHTTPClientFromEnv: %v", err)
	}
	if client.Timeout != 0 {
		t.Fatalf("client.Timeout = %s, want 0 for long-lived clients", client.Timeout)
	}
}

// QA reopen #2 regression: chat / admin routes MUST go through a
// trusted-forwarder middleware that gates spoofed identity headers
// behind a valid X-API-Key. Without the API key, no AuthContext is
// populated and the request is rejected at the boundary, NOT at the
// handler.
func TestRequireTrustedForwarder(t *testing.T) {
	const goodKey = "svc-test-key"

	t.Run("missing X-API-Key returns 401 without invoking handler", func(t *testing.T) {
		invoked := false
		next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { invoked = true })
		mw := requireTrustedForwarder(goodKey)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", strings.NewReader(`{}`))
		req.Header.Set("X-Cordum-Principal", "admin")
		rec := httptest.NewRecorder()
		mw(next).ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		if invoked {
			t.Fatal("inner handler invoked despite missing API key — middleware fail-open")
		}
		if !strings.Contains(rec.Body.String(), "invalid_api_key") {
			t.Errorf("body = %q, want code=invalid_api_key", rec.Body.String())
		}
	})

	t.Run("wrong X-API-Key returns 401 (constant-time)", func(t *testing.T) {
		invoked := false
		next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { invoked = true })
		mw := requireTrustedForwarder(goodKey)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", strings.NewReader(`{}`))
		req.Header.Set("X-API-Key", "attacker-guess")
		rec := httptest.NewRecorder()
		mw(next).ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		if invoked {
			t.Fatal("inner handler invoked despite wrong API key")
		}
	})

	t.Run("spoofed identity headers without API key fail closed", func(t *testing.T) {
		var seenAuth *gatewayauth.AuthContext
		next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			seenAuth = gatewayauth.FromRequest(r)
		})
		mw := requireTrustedForwarder(goodKey)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", strings.NewReader(`{}`))
		req.Header.Set("X-Cordum-Principal", "admin")
		req.Header.Set("X-Cordum-Tenant", "tenant-victim")
		req.Header.Set("X-Cordum-Role", "admin")
		// no X-API-Key
		rec := httptest.NewRecorder()
		mw(next).ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (spoofed identity must fail without API key)", rec.Code)
		}
		if seenAuth != nil {
			t.Fatal("AuthContext populated despite missing API key — boundary leaked")
		}
	})

	t.Run("valid X-API-Key sets AuthContext from forwarder headers", func(t *testing.T) {
		var seenAuth *gatewayauth.AuthContext
		next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			seenAuth = gatewayauth.FromRequest(r)
		})
		mw := requireTrustedForwarder(goodKey)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", strings.NewReader(`{}`))
		req.Header.Set("X-API-Key", goodKey)
		req.Header.Set("X-Cordum-Principal", "alice@example.com")
		req.Header.Set("X-Cordum-Tenant", "tenant-a")
		req.Header.Set("X-Cordum-Role", "operator")
		req.Header.Set("X-Cordum-Allow-Cross-Tenant", "true")
		rec := httptest.NewRecorder()
		mw(next).ServeHTTP(rec, req)
		if seenAuth == nil {
			t.Fatalf("AuthContext nil, want populated; resp=%d body=%q", rec.Code, rec.Body.String())
		}
		if seenAuth.PrincipalID != "alice@example.com" {
			t.Errorf("PrincipalID = %q, want alice@example.com", seenAuth.PrincipalID)
		}
		if seenAuth.Tenant != "tenant-a" {
			t.Errorf("Tenant = %q, want tenant-a", seenAuth.Tenant)
		}
		if seenAuth.Role != "operator" {
			t.Errorf("Role = %q, want operator", seenAuth.Role)
		}
		if !seenAuth.AllowCrossTenant {
			t.Errorf("AllowCrossTenant = false, want true")
		}
	})

	t.Run("Authorization: Bearer fallback is honored", func(t *testing.T) {
		var seenAuth *gatewayauth.AuthContext
		next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			seenAuth = gatewayauth.FromRequest(r)
		})
		mw := requireTrustedForwarder(goodKey)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+goodKey)
		req.Header.Set("X-Cordum-Principal", "bob@example.com")
		rec := httptest.NewRecorder()
		mw(next).ServeHTTP(rec, req)
		if seenAuth == nil || seenAuth.PrincipalID != "bob@example.com" {
			t.Fatalf("Authorization Bearer fallback failed; auth=%+v code=%d", seenAuth, rec.Code)
		}
	})

	t.Run("empty configured key fails closed with 503", func(t *testing.T) {
		mw := requireTrustedForwarder("")
		req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", strings.NewReader(`{}`))
		req.Header.Set("X-API-Key", "anything")
		rec := httptest.NewRecorder()
		mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503 (misconfigured service must fail closed)", rec.Code)
		}
	})
}

// constantTimeEqualString is a security primitive — verify it doesn't
// short-circuit on length differences and rejects empty strings.
func TestConstantTimeEqualString(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"", "", false},        // both empty → fail-closed (not authenticated)
		{"x", "", false},       // one empty → fail-closed
		{"", "x", false},       // one empty → fail-closed
		{"abc", "abc", true},   // equal
		{"abc", "abd", false},  // last byte diff
		{"abc", "abcd", false}, // length diff
	}
	for _, tc := range cases {
		if got := constantTimeEqualString(tc.a, tc.b); got != tc.want {
			t.Errorf("constantTimeEqualString(%q,%q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
