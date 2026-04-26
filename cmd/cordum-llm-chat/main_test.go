package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gatewayauth "github.com/cordum/cordum/core/controlplane/gateway/auth"
)

func fakeEnv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func TestLoadConfigFromEnv_DefaultsWhenOnlyRequiredSet(t *testing.T) {
	cfg, err := loadConfigFromEnv(fakeEnv(map[string]string{
		"REDIS_URL": "redis://localhost:6379/0",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ChatAssistantAgentID != "" {
		t.Errorf("ChatAssistantAgentID = %q, want empty (unpinned)", cfg.ChatAssistantAgentID)
	}
	if cfg.DelegationTTL != defaultDelegationTTL {
		t.Errorf("DelegationTTL = %v, want %v", cfg.DelegationTTL, defaultDelegationTTL)
	}
	if cfg.HTTPAddr != defaultHTTPAddr {
		t.Fatalf("HTTPAddr = %q, want %q", cfg.HTTPAddr, defaultHTTPAddr)
	}
	if cfg.Provider.Kind != defaultProvider {
		t.Fatalf("Provider.Kind = %q, want %q", cfg.Provider.Kind, defaultProvider)
	}
	if cfg.Provider.BaseURL != defaultBaseURL {
		t.Fatalf("Provider.BaseURL = %q, want %q", cfg.Provider.BaseURL, defaultBaseURL)
	}
	if cfg.Provider.Model != defaultModel {
		t.Fatalf("Provider.Model = %q, want %q", cfg.Provider.Model, defaultModel)
	}
	if cfg.Provider.ToolTemperature != defaultToolTemperature {
		t.Fatalf("ToolTemperature = %v, want %v", cfg.Provider.ToolTemperature, defaultToolTemperature)
	}
	if cfg.Provider.ToolTopP != defaultToolTopP {
		t.Fatalf("ToolTopP = %v, want %v", cfg.Provider.ToolTopP, defaultToolTopP)
	}
	if cfg.Provider.SummaryTemperature != defaultSummaryTemperature {
		t.Fatalf("SummaryTemperature = %v, want %v", cfg.Provider.SummaryTemperature, defaultSummaryTemperature)
	}
	if cfg.Provider.SummaryTopP != defaultSummaryTopP {
		t.Fatalf("SummaryTopP = %v, want %v", cfg.Provider.SummaryTopP, defaultSummaryTopP)
	}
	if cfg.Budget.MaxToolCallsPerTurn != defaultMaxToolCallsPerTurn {
		t.Fatalf("MaxToolCallsPerTurn = %d, want %d", cfg.Budget.MaxToolCallsPerTurn, defaultMaxToolCallsPerTurn)
	}
	if cfg.Budget.MaxWallClockPerTurn != defaultMaxWallClockPerTurn {
		t.Fatalf("MaxWallClockPerTurn = %v, want %v", cfg.Budget.MaxWallClockPerTurn, defaultMaxWallClockPerTurn)
	}
	if cfg.Budget.MaxAssistantBytes != defaultMaxAssistantBytes {
		t.Fatalf("MaxAssistantBytes = %d, want %d", cfg.Budget.MaxAssistantBytes, defaultMaxAssistantBytes)
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

func TestLoadConfigFromEnv_AllOverridesRead(t *testing.T) {
	cfg, err := loadConfigFromEnv(fakeEnv(map[string]string{
		"REDIS_URL":                       "redis://custom:6379/1",
		"NATS_URL":                        "nats://nats:4222",
		"CORDUM_API_KEY":                  "sekret",
		"CORDUM_GATEWAY_URL":              "https://gateway.internal:8443",
		"CORDUM_LLM_CHAT_ADDR":            ":9090",
		"CORDUM_LLM_CHAT_TLS_CERT_FILE":   "/tls/tls.crt",
		"CORDUM_LLM_CHAT_TLS_KEY_FILE":    "/tls/tls.key",
		"LLMCHAT_PROVIDER":                "openai",
		"LLMCHAT_BASE_URL":                "http://vllm:8000/v1",
		"LLMCHAT_MODEL":                   "qwen3-coder",
		"LLMCHAT_API_KEY":                 "token",
		"LLMCHAT_TOOL_TEMPERATURE":        "0.35",
		"LLMCHAT_TOOL_TOP_P":              "0.88",
		"LLMCHAT_SUMMARY_TEMPERATURE":     "0.72",
		"LLMCHAT_SUMMARY_TOP_P":           "0.81",
		"LLMCHAT_MAX_TOOL_CALLS_PER_TURN": "24",
		"LLMCHAT_MAX_WALL_CLOCK_PER_TURN": "90s",
		"LLMCHAT_MAX_ASSISTANT_BYTES":     "65536",
		"LLMCHAT_CHAT_ASSISTANT_AGENT_ID": "chat-assistant-prod",
		"LLMCHAT_TENANT":                  "tenant-a",
		"LLMCHAT_DELEGATION_TTL_SECONDS":  "1200",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HTTPAddr != ":9090" {
		t.Fatalf("HTTPAddr = %q, want :9090", cfg.HTTPAddr)
	}
	if cfg.TLSCertFile != "/tls/tls.crt" || cfg.TLSKeyFile != "/tls/tls.key" {
		t.Fatalf("TLS = %q/%q, want /tls/tls.crt //tls/tls.key", cfg.TLSCertFile, cfg.TLSKeyFile)
	}
	if cfg.CordumAPIKey != "sekret" {
		t.Fatalf("CordumAPIKey = %q, want sekret", cfg.CordumAPIKey)
	}
	if cfg.GatewayURL != "https://gateway.internal:8443" {
		t.Fatalf("GatewayURL = %q, want https://gateway.internal:8443", cfg.GatewayURL)
	}
	if cfg.NATSURL != "nats://nats:4222" {
		t.Fatalf("NATSURL = %q, want nats://nats:4222", cfg.NATSURL)
	}
	if cfg.Provider.APIKey != "token" {
		t.Fatalf("APIKey = %q, want token", cfg.Provider.APIKey)
	}
	if cfg.Provider.ToolTemperature != 0.35 {
		t.Fatalf("ToolTemperature = %v, want 0.35", cfg.Provider.ToolTemperature)
	}
	if cfg.Provider.ToolTopP != 0.88 {
		t.Fatalf("ToolTopP = %v, want 0.88", cfg.Provider.ToolTopP)
	}
	if cfg.Provider.SummaryTemperature != 0.72 {
		t.Fatalf("SummaryTemperature = %v, want 0.72", cfg.Provider.SummaryTemperature)
	}
	if cfg.Provider.SummaryTopP != 0.81 {
		t.Fatalf("SummaryTopP = %v, want 0.81", cfg.Provider.SummaryTopP)
	}
	if cfg.Budget.MaxToolCallsPerTurn != 24 {
		t.Fatalf("MaxToolCallsPerTurn = %d, want 24", cfg.Budget.MaxToolCallsPerTurn)
	}
	if cfg.Budget.MaxWallClockPerTurn != 90*time.Second {
		t.Fatalf("MaxWallClockPerTurn = %v, want 90s", cfg.Budget.MaxWallClockPerTurn)
	}
	if cfg.Budget.MaxAssistantBytes != 65536 {
		t.Fatalf("MaxAssistantBytes = %d, want 65536", cfg.Budget.MaxAssistantBytes)
	}
	if cfg.ChatAssistantAgentID != "chat-assistant-prod" {
		t.Errorf("ChatAssistantAgentID = %q, want chat-assistant-prod", cfg.ChatAssistantAgentID)
	}
	if cfg.Tenant != "tenant-a" {
		t.Errorf("Tenant = %q, want tenant-a", cfg.Tenant)
	}
	if cfg.DelegationTTL != 1200*time.Second {
		t.Errorf("DelegationTTL = %v, want 1200s", cfg.DelegationTTL)
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

func TestLoadConfigFromEnv_RejectsZeroDelegationTTL(t *testing.T) {
	_, err := loadConfigFromEnv(fakeEnv(map[string]string{
		"REDIS_URL":                      "redis://localhost:6379/0",
		"LLMCHAT_DELEGATION_TTL_SECONDS": "0",
	}))
	if err == nil {
		t.Fatal("expected error for zero LLMCHAT_DELEGATION_TTL_SECONDS")
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
		"LLMCHAT_TOOL_TEMPERATURE":        "not-a-float",
		"LLMCHAT_TOOL_TOP_P":              "abc",
		"LLMCHAT_SUMMARY_TEMPERATURE":     "inf!",
		"LLMCHAT_SUMMARY_TOP_P":           "oops",
		"LLMCHAT_MAX_TOOL_CALLS_PER_TURN": "twelve",
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
