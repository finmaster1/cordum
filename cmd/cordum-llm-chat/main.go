// Command cordum-llm-chat is the scaffold for the self-hosted Cordum LLM
// Chat Assistant service. Phase 1 delivered logger + buildinfo + env
// parsing + OpenAI-compat provider + /healthz + /readyz. Phase 3 wires
// the identity + persistence layer: Redis session store, per-session
// delegation tokens via the gateway, and idempotent chat-assistant
// agent bootstrap via the CAP SDK AgentClient (POST/GET/PUT
// /api/v1/agents). /api/v1/chat WS handlers land in phase 5.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	capsdk "github.com/cordum-io/cap/v2/sdk/go"
	"github.com/cordum/cordum/core/audit"
	gatewayauth "github.com/cordum/cordum/core/controlplane/gateway/auth"
	"github.com/cordum/cordum/core/infra/buildinfo"
	"github.com/cordum/cordum/core/infra/bus"
	"github.com/cordum/cordum/core/infra/logging"
	"github.com/cordum/cordum/core/infra/redisutil"
	"github.com/cordum/cordum/core/licensing"
	"github.com/cordum/cordum/core/llmchat"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
)

const (
	defaultHTTPAddr            = ":8091"
	defaultProvider            = "openai"
	defaultBaseURL             = "http://qwen-inference:8000/v1"
	defaultModel               = "qwen3-coder"
	defaultToolTemperature     = 0.3
	defaultToolTopP            = 0.9
	defaultSummaryTemperature  = 0.7
	defaultSummaryTopP         = 0.8
	defaultMaxToolCallsPerTurn = 12
	defaultMaxWallClockPerTurn = 60 * time.Second
	defaultMaxAssistantBytes   = 32768
	defaultDelegationTTL       = 15 * time.Minute
	bootstrapTimeout           = 30 * time.Second
	readyzProbeTimeout         = 2 * time.Second
	defaultGatewayHTTPTimeout  = 30 * time.Second
	shutdownGrace              = 10 * time.Second

	envCordumTLSCA       = "CORDUM_TLS_CA"
	envCordumTLSInsecure = "CORDUM_TLS_INSECURE"
)

// runtimeConfig is the fully-resolved, validated boot configuration.
// Kept separate from llmchat.ProviderConfig so transport + Redis wiring
// stays in the process binary, not leaked into the reusable provider
// package.
type runtimeConfig struct {
	HTTPAddr             string
	TLSCertFile          string
	TLSKeyFile           string
	RedisURL             string
	Provider             llmchat.ProviderConfig
	Budget               llmchat.BudgetConfig
	CordumAPIKey         string
	GatewayURL           string
	NATSURL              string
	ChatAssistantAgentID string
	Tenant               string
	DelegationTTL        time.Duration
}

func main() {
	logging.Init("llm-chat-server")
	buildinfo.Log("cordum-llm-chat")

	cfg, err := loadConfigFromEnv(os.Getenv)
	if err != nil {
		slog.Error("cordum-llm-chat: config load failed, refusing to start", "error", err)
		os.Exit(1)
	}

	provider, err := llmchat.ResolveProvider(cfg.Provider)
	if err != nil {
		slog.Error("cordum-llm-chat: provider resolve failed, refusing to start", "error", err)
		os.Exit(1)
	}

	redisClient, err := openRedis(cfg.RedisURL)
	if err != nil {
		slog.Error("cordum-llm-chat: redis connect failed, refusing to start", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := redisClient.Close(); err != nil {
			slog.Warn("cordum-llm-chat: redis close failed", "error", err)
		}
	}()

	sessionStore := llmchat.NewSessionStoreFromClient(redisClient)

	gatewayHTTPClient, err := gatewayHTTPClientFromEnv(os.Getenv, defaultGatewayHTTPTimeout)
	if err != nil {
		slog.Error("cordum-llm-chat: gateway TLS config failed, refusing to start", "error", err)
		os.Exit(1)
	}
	mcpHTTPClient, err := gatewayHTTPClientFromEnv(os.Getenv, -1)
	if err != nil {
		slog.Error("cordum-llm-chat: mcp gateway TLS config failed, refusing to start", "error", err)
		os.Exit(1)
	}

	// Wire the knowledge-pack substituters around the file-backed
	// prompt loader. When LLMCHAT_KNOWLEDGE_PACK_ENABLED=false the
	// placeholders pass through unchanged (rail #1 — substituters
	// WRAP, never REPLACE the prompt loader).
	basePromptLoader := llmchat.NewFilePromptLoader("")
	promptLoader := basePromptLoader
	if envOrDefault(os.Getenv, "LLMCHAT_KNOWLEDGE_PACK_ENABLED", "true") == "true" {
		kp := llmchat.NewKnowledgePackLoader(basePromptLoader)
		kp.Register("api_summary", llmchat.NewOpenAPISubstituter(os.Getenv("LLMCHAT_OPENAPI_PATH")))
		kp.Register("cordum_io_summary", llmchat.NewCordumIOSubstituter(os.Getenv("LLMCHAT_CORDUM_IO_PATH")))
		llmchat.ListenSIGHUP(kp) // build-tagged stub on Windows
		// Precompute on boot so the per-turn LLM call sees a hot
		// cache (rail #5).
		warmCtx, warmCancel := context.WithTimeout(context.Background(), 30*time.Second)
		if _, err := kp.Load(warmCtx); err != nil {
			slog.Warn("cordum-llm-chat: knowledge_pack_warm_failed", "error", err)
		}
		warmCancel()
		promptLoader = kp
	}

	mcpClient, err := llmchat.NewMCPClient(llmchat.MCPClientConfig{
		BaseURL:       cfg.GatewayURL,
		APIKey:        cfg.CordumAPIKey,
		TenantID:      cfg.Tenant,
		AgentID:       cfg.ChatAssistantAgentID,
		ClientName:    "cordum-llm-chat",
		ClientVersion: "0.1.0",
		HTTPClient:    mcpHTTPClient,
	})
	if err != nil {
		slog.Error("cordum-llm-chat: mcp client construction failed", "error", err)
		os.Exit(1)
	}
	defer mcpClient.Close()

	auditChainer := audit.NewChainer(redisClient, "audit:chain:")

	// chat-assistant bootstrap goes through the CAP SDK AgentClient,
	// which wraps the gateway's /api/v1/agents endpoints (same audit
	// chain + approval gate as any other Cordum agent identity). The
	// service API key authenticates this trust path; per-session
	// delegation tokens are issued separately by DelegationClient
	// below.
	agentRegistry, err := capsdk.NewAgentClient(capsdk.AgentClientConfig{
		BaseURL:    cfg.GatewayURL,
		APIKey:     cfg.CordumAPIKey,
		Tenant:     cfg.Tenant,
		HTTPClient: gatewayHTTPClient,
	})
	if err != nil {
		slog.Error("cordum-llm-chat: cap agent client construction failed", "error", err)
		os.Exit(1)
	}
	bootstrapper := llmchat.NewBootstrapper(agentRegistry, cfg.Tenant, auditChainer)
	bootCtx, cancelBoot := context.WithTimeout(context.Background(), bootstrapTimeout)
	resolvedAgentID, err := bootstrapper.Boot(bootCtx)
	cancelBoot()
	if err != nil {
		slog.Error("cordum-llm-chat: chat-assistant bootstrap failed", "error", err)
		os.Exit(1)
	}

	// The agent ID returned from Boot is the canonical identifier —
	// when bootstrap registers a new agent, the gateway assigns the
	// id server-side, which may differ from the env hint. Downstream
	// (DelegationClient) uses the resolved id; if the env supplied a
	// pin, we error out on mismatch so the operator knows their
	// configured pin is stale rather than silently issuing tokens for
	// the wrong agent.
	if cfg.ChatAssistantAgentID != "" && cfg.ChatAssistantAgentID != resolvedAgentID {
		slog.Error("cordum-llm-chat: env LLMCHAT_CHAT_ASSISTANT_AGENT_ID does not match registered agent",
			"env_id", cfg.ChatAssistantAgentID, "resolved_id", resolvedAgentID,
			"remediation", "either remove LLMCHAT_CHAT_ASSISTANT_AGENT_ID to use the resolved id, or update it to "+resolvedAgentID)
		os.Exit(1)
	}

	delegationClient := llmchat.NewDelegationClient(llmchat.DelegationConfig{
		BaseURL:    cfg.GatewayURL,
		AgentID:    resolvedAgentID,
		APIKey:     cfg.CordumAPIKey,
		Tenant:     cfg.Tenant,
		IssueTTL:   cfg.DelegationTTL,
		RetryDelay: 100 * time.Millisecond,
		HTTPClient: gatewayHTTPClient,
	})

	entitlementResolver := licensing.NewEntitlementResolver()
	entitlementResolver.Init()
	permissionChecker := gatewayauth.NewPermissionChecker(gatewayauth.NewRBACStoreFromClient(redisClient), func() licensing.Entitlements {
		return entitlementResolver.Entitlements()
	})
	metrics := llmchat.NewMetrics(prometheus.DefaultRegisterer)
	agent := llmchat.NewAgent(llmchat.AgentConfig{
		Provider:     provider,
		MCP:          mcpClient,
		Redactor:     llmchat.NewRedactor(),
		PromptLoader: promptLoader,
		Sessions:     sessionStore,
		Metrics:      metrics,
	})
	var approvalBus llmchat.ApprovalEventBus
	if strings.TrimSpace(cfg.NATSURL) != "" {
		natsBus, err := bus.NewNatsBus(cfg.NATSURL)
		if err != nil {
			slog.Warn("cordum-llm-chat: approval NATS bus unavailable; approval resume disabled", "error", err)
		} else {
			defer natsBus.Close()
			approvalBus = llmchat.NewNATSApprovalEventBus(natsBus)
		}
	}
	approvalResumer := llmchat.NewApprovalResumer(llmchat.ApprovalResumerConfig{Bus: approvalBus, Runner: agent})
	defer func() {
		if err := approvalResumer.Close(); err != nil {
			slog.Warn("cordum-llm-chat: approval resumer close failed", "error", err)
		}
	}()
	auditSender := llmchat.NewChainedAuditSender(auditChainer, nil)

	handlers := llmchat.NewHandlers(provider, redisClient, readyzProbeTimeout)
	chatHandlers := llmchat.NewChatHandlers(llmchat.ChatHandlersConfig{
		Agent:        agent,
		Sessions:     sessionStore,
		Entitlements: entitlementResolver,
		Permissions:  permissionChecker,
		Delegations:  delegationClient,
		Audit:        auditSender,
		Approvals:    approvalResumer,
		AgentID:      resolvedAgentID,
		Metrics:      metrics,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handlers.Healthz)
	mux.HandleFunc("/readyz", handlers.Readyz)
	mux.HandleFunc("/api/v1/chat/healthz", handlers.Readyz)
	registerMetricsHandler(mux, prometheus.DefaultGatherer)

	// Trusted-forwarder auth middleware. Every chat / admin route MUST
	// go through this so handlers see a populated gatewayauth.AuthContext
	// rather than reading from spoofable request headers directly.
	//
	// Trust model: this service runs BEHIND the cordum gateway, which is
	// the auth boundary. The gateway authenticates the end user (JWT /
	// session cookie / API key), then forwards the request to llm-chat
	// with `X-API-Key` matching cfg.CordumAPIKey (proves the caller is
	// the gateway, not a malicious client) plus identity-attributing
	// headers (`X-Cordum-Principal`, `X-Cordum-Tenant`, `X-Cordum-Role`,
	// `X-Cordum-Allow-Cross-Tenant`).
	//
	// Without a valid X-API-Key the request is rejected — spoofed
	// `X-Cordum-Principal: admin` from a direct caller fails closed.
	authedChat := requireTrustedForwarder(cfg.CordumAPIKey)

	mux.Handle("/api/v1/chat", authedChat(http.HandlerFunc(chatHandlers.HandleChatPost)))
	mux.Handle("/api/v1/chat/stream", authedChat(http.HandlerFunc(chatHandlers.HandleChatStream)))
	mux.Handle("/api/v1/chat/ws", authedChat(http.HandlerFunc(chatHandlers.HandleChatWS)))
	mux.Handle("/api/v1/chat/sessions", authedChat(http.HandlerFunc(chatHandlers.HandleListSessions)))
	mux.Handle("/api/v1/chat/sessions/", authedChat(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chatHandlers.HandleGetSession(w, r, strings.TrimPrefix(r.URL.Path, "/api/v1/chat/sessions/"))
	})))

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("cordum-llm-chat listening",
			"addr", cfg.HTTPAddr,
			"tls", cfg.TLSCertFile != "",
			"provider", cfg.Provider.Kind,
			"base_url", cfg.Provider.BaseURL,
			"model", cfg.Provider.Model,
		)
		var err error
		if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
			err = srv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case <-ctx.Done():
		slog.Info("cordum-llm-chat: shutdown signal received")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("cordum-llm-chat: graceful shutdown failed", "error", err)
			os.Exit(1)
		}
		slog.Info("cordum-llm-chat: shutdown complete")
	case err := <-serveErr:
		if err != nil {
			slog.Error("cordum-llm-chat: http server failed", "error", err)
			os.Exit(1)
		}
	}
}

func registerMetricsHandler(mux *http.ServeMux, gatherer prometheus.Gatherer) {
	if gatherer == nil {
		gatherer = prometheus.DefaultGatherer
	}
	// Co-locate /metrics with /healthz and /readyz on the llm-chat main
	// port. Scheduler/context-engine use separate metrics ports, but this
	// bug-fix keeps the surface at the DoD-required 127.0.0.1:8090/metrics
	// endpoint and avoids adding a compose/Helm port while the service is
	// already deployed behind the trusted gateway boundary.
	mux.Handle("/metrics", promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{}))
}

// loadConfigFromEnv resolves every boot env var into a validated
// runtimeConfig. Fails closed on missing required values and on any
// numeric parse error — operators should see a crisp error rather than
// a silent default that masks a typo.
func loadConfigFromEnv(getenv func(string) string) (runtimeConfig, error) {
	cfg := runtimeConfig{
		HTTPAddr:     envOrDefault(getenv, "CORDUM_LLM_CHAT_ADDR", defaultHTTPAddr),
		TLSCertFile:  strings.TrimSpace(getenv("CORDUM_LLM_CHAT_TLS_CERT_FILE")),
		TLSKeyFile:   strings.TrimSpace(getenv("CORDUM_LLM_CHAT_TLS_KEY_FILE")),
		RedisURL:     strings.TrimSpace(getenv("REDIS_URL")),
		CordumAPIKey: strings.TrimSpace(getenv("CORDUM_API_KEY")),
		GatewayURL:   strings.TrimSpace(getenv("CORDUM_GATEWAY_URL")),
		NATSURL:      strings.TrimSpace(getenv("NATS_URL")),
	}

	if cfg.RedisURL == "" {
		return runtimeConfig{}, fmt.Errorf("REDIS_URL is required")
	}
	if (cfg.TLSCertFile == "") != (cfg.TLSKeyFile == "") {
		return runtimeConfig{}, fmt.Errorf(
			"CORDUM_LLM_CHAT_TLS_CERT_FILE and CORDUM_LLM_CHAT_TLS_KEY_FILE must be set together",
		)
	}

	providerKind := strings.TrimSpace(getenv("LLMCHAT_PROVIDER"))
	if providerKind == "" {
		providerKind = defaultProvider
	}
	baseURL := strings.TrimSpace(getenv("LLMCHAT_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	model := strings.TrimSpace(getenv("LLMCHAT_MODEL"))
	if model == "" {
		model = defaultModel
	}

	toolTemp, err := envFloatOrDefault(getenv, "LLMCHAT_TOOL_TEMPERATURE", defaultToolTemperature)
	if err != nil {
		return runtimeConfig{}, err
	}
	toolTopP, err := envFloatOrDefault(getenv, "LLMCHAT_TOOL_TOP_P", defaultToolTopP)
	if err != nil {
		return runtimeConfig{}, err
	}
	summaryTemp, err := envFloatOrDefault(getenv, "LLMCHAT_SUMMARY_TEMPERATURE", defaultSummaryTemperature)
	if err != nil {
		return runtimeConfig{}, err
	}
	summaryTopP, err := envFloatOrDefault(getenv, "LLMCHAT_SUMMARY_TOP_P", defaultSummaryTopP)
	if err != nil {
		return runtimeConfig{}, err
	}

	maxToolCalls, err := envIntOrDefault(getenv, "LLMCHAT_MAX_TOOL_CALLS_PER_TURN", defaultMaxToolCallsPerTurn)
	if err != nil {
		return runtimeConfig{}, err
	}
	maxWallClock, err := envDurationOrDefault(getenv, "LLMCHAT_MAX_WALL_CLOCK_PER_TURN", defaultMaxWallClockPerTurn)
	if err != nil {
		return runtimeConfig{}, err
	}
	maxAssistantBytes, err := envIntOrDefault(getenv, "LLMCHAT_MAX_ASSISTANT_BYTES", defaultMaxAssistantBytes)
	if err != nil {
		return runtimeConfig{}, err
	}

	cfg.Provider = llmchat.ProviderConfig{
		Kind:               providerKind,
		BaseURL:            baseURL,
		Model:              model,
		APIKey:             strings.TrimSpace(getenv("LLMCHAT_API_KEY")),
		ToolTemperature:    toolTemp,
		ToolTopP:           toolTopP,
		SummaryTemperature: summaryTemp,
		SummaryTopP:        summaryTopP,
	}
	cfg.Budget = llmchat.BudgetConfig{
		MaxToolCallsPerTurn: maxToolCalls,
		MaxWallClockPerTurn: maxWallClock,
		MaxAssistantBytes:   maxAssistantBytes,
	}

	// LLMCHAT_CHAT_ASSISTANT_AGENT_ID is OPTIONAL: the bootstrap path
	// resolves the canonical chat-assistant agent id at startup
	// (either by reusing an existing identity or registering a new
	// one via MCP). When the env is set it acts as a pin — main.go
	// errors out post-bootstrap if the resolved id does not match,
	// surfacing stale operator config rather than silently issuing
	// delegations for the wrong agent. Greenfield deployments leave
	// it empty.
	cfg.ChatAssistantAgentID = strings.TrimSpace(getenv("LLMCHAT_CHAT_ASSISTANT_AGENT_ID"))
	cfg.Tenant = strings.TrimSpace(getenv("LLMCHAT_TENANT"))

	delegationTTLSeconds, err := envIntOrDefault(getenv, "LLMCHAT_DELEGATION_TTL_SECONDS", int(defaultDelegationTTL.Seconds()))
	if err != nil {
		return runtimeConfig{}, err
	}
	if delegationTTLSeconds <= 0 {
		return runtimeConfig{}, fmt.Errorf("LLMCHAT_DELEGATION_TTL_SECONDS must be positive")
	}
	cfg.DelegationTTL = time.Duration(delegationTTLSeconds) * time.Second

	return cfg, nil
}

func openRedis(redisURL string) (*redis.Client, error) {
	options, err := redisOptionsFromURL(redisURL)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(options)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return client, nil
}

func redisOptionsFromURL(redisURL string) (*redis.Options, error) {
	options, err := redisutil.ParseOptions(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	return options, nil
}

func gatewayHTTPClientFromEnv(getenv func(string) string, timeout time.Duration) (*http.Client, error) {
	// timeout < 0 intentionally means "no whole-request timeout". The MCP
	// SSE connection is long-lived and must not inherit the 30s REST timeout
	// used for short gateway API calls, or it will reconnect forever and spam
	// warning logs despite a healthy /mcp/sse transport.
	noTimeout := timeout < 0
	if timeout == 0 {
		timeout = defaultGatewayHTTPTimeout
	}
	caPath := strings.TrimSpace(getenv(envCordumTLSCA))
	insecure := parseBoolString(getenv(envCordumTLSInsecure))

	if caPath == "" && !insecure {
		client := &http.Client{}
		if !noTimeout {
			client.Timeout = timeout
		}
		return client, nil
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if insecure {
		// #nosec G402 -- explicit dev/debug escape hatch matching cordumctl.
		tlsConfig.InsecureSkipVerify = true
	}
	if caPath != "" {
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		if pool == nil {
			pool = x509.NewCertPool()
		}
		// #nosec G304 -- CA path is operator-configured via CORDUM_TLS_CA.
		pem, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", envCordumTLSCA, err)
		}
		if ok := pool.AppendCertsFromPEM(pem); !ok {
			return nil, fmt.Errorf("parse %s: no certificates found in %s", envCordumTLSCA, caPath)
		}
		tlsConfig.RootCAs = pool
	}
	transport.TLSClientConfig = tlsConfig
	client := &http.Client{Transport: transport}
	if !noTimeout {
		client.Timeout = timeout
	}
	return client, nil
}

func envOrDefault(getenv func(string) string, key, fallback string) string {
	if val := strings.TrimSpace(getenv(key)); val != "" {
		return val
	}
	return fallback
}

func envFloatOrDefault(getenv func(string) string, key string, fallback float64) (float64, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s=%q: %w", key, raw, err)
	}
	return v, nil
}

func envIntOrDefault(getenv func(string) string, key string, fallback int) (int, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s=%q: %w", key, raw, err)
	}
	return v, nil
}

func envDurationOrDefault(getenv func(string) string, key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s=%q: %w", key, raw, err)
	}
	return v, nil
}

func parseBoolString(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

// requireTrustedForwarder builds the auth middleware for chat / admin
// routes. The cordum-llm-chat service runs behind the cordum gateway,
// which is the auth boundary; the gateway forwards requests with
// `X-API-Key` matching `CORDUM_API_KEY` plus identity-attributing
// headers. This middleware:
//
//  1. Compares `X-API-Key` (or `Authorization: ApiKey <key>` /
//     `Authorization: Bearer <key>`) against the configured service
//     key in constant time. Mismatch → 401.
//  2. On match, populates `gatewayauth.AuthContext` from the trusted
//     forwarder headers `X-Cordum-Principal`, `X-Cordum-Tenant`,
//     `X-Cordum-Role`, `X-Cordum-Allow-Cross-Tenant`. These headers
//     are trusted ONLY because step 1 proved the caller is the gateway.
//  3. Passes the augmented request down to the handler chain.
//
// Without a valid X-API-Key, identity headers are ignored — a direct
// caller cannot spoof `X-Cordum-Principal: admin` to bypass admin
// gates.
//
// If apiKey is empty (misconfiguration), the middleware refuses every
// request with 503 to fail closed rather than silently allowing
// unauthenticated traffic.
func requireTrustedForwarder(apiKey string) func(http.Handler) http.Handler {
	expected := []byte(strings.TrimSpace(apiKey))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(expected) == 0 {
				slog.Error("cordum-llm-chat: refusing request — CORDUM_API_KEY is unset; service is misconfigured")
				writeAuthError(w, http.StatusServiceUnavailable, "service_misconfigured", "service is missing required CORDUM_API_KEY")
				return
			}
			provided := strings.TrimSpace(r.Header.Get("X-API-Key"))
			if provided == "" {
				if hdr := r.Header.Get("Authorization"); hdr != "" {
					switch {
					case strings.HasPrefix(hdr, "ApiKey "):
						provided = strings.TrimSpace(strings.TrimPrefix(hdr, "ApiKey "))
					case strings.HasPrefix(hdr, "Bearer "):
						provided = strings.TrimSpace(strings.TrimPrefix(hdr, "Bearer "))
					}
				}
			}
			if !constantTimeEqualString(provided, string(expected)) {
				writeAuthError(w, http.StatusUnauthorized, "invalid_api_key", "missing or invalid X-API-Key")
				return
			}
			authCtx := &gatewayauth.AuthContext{
				APIKey:           string(expected),
				PrincipalID:      strings.TrimSpace(r.Header.Get("X-Cordum-Principal")),
				Tenant:           strings.TrimSpace(r.Header.Get("X-Cordum-Tenant")),
				Role:             strings.TrimSpace(r.Header.Get("X-Cordum-Role")),
				AllowCrossTenant: strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Cordum-Allow-Cross-Tenant")), "true"),
			}
			ctx := context.WithValue(r.Context(), gatewayauth.ContextKey{}, authCtx)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// constantTimeEqualString avoids timing leaks when comparing the
// X-API-Key against the configured service key. Length-mismatch is
// also constant-time relative to itself.
func constantTimeEqualString(a, b string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

func writeAuthError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":   "request_failed",
		"code":    code,
		"message": msg,
		"status":  status,
	})
}
