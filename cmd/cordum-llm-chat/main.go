// Command cordum-llm-chat is the scaffold for the self-hosted Cordum LLM
// Chat Assistant service. Phase 1 delivered logger + buildinfo + env
// parsing + OpenAI-compat provider + /healthz + /readyz. Phase 3 wires
// the identity + persistence layer: Redis session store, per-session
// delegation tokens via the gateway, and idempotent chat-assistant
// agent bootstrap via MCP. /api/v1/chat WS handlers land in phase 5.
package main

import (
	"context"
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

	"github.com/cordum/cordum/core/audit"
	"github.com/cordum/cordum/core/infra/buildinfo"
	"github.com/cordum/cordum/core/infra/logging"
	"github.com/cordum/cordum/core/llmchat"
	"github.com/cordum/cordum/core/mcp"
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
	shutdownGrace              = 10 * time.Second
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
	_ = sessionStore // consumed by phase-5 WS handler

	delegationClient := llmchat.NewDelegationClient(llmchat.DelegationConfig{
		BaseURL:    cfg.GatewayURL,
		AgentID:    cfg.ChatAssistantAgentID,
		APIKey:     cfg.CordumAPIKey,
		Tenant:     cfg.Tenant,
		IssueTTL:   cfg.DelegationTTL,
		RetryDelay: 100 * time.Millisecond,
	})
	_ = delegationClient // consumed by phase-5 WS handler

	mcpClient, err := llmchat.NewMCPClient(llmchat.MCPClientConfig{
		BaseURL:       cfg.GatewayURL,
		APIKey:        cfg.CordumAPIKey,
		TenantID:      cfg.Tenant,
		AgentID:       cfg.ChatAssistantAgentID,
		ClientName:    "cordum-llm-chat",
		ClientVersion: "0.1.0",
	})
	if err != nil {
		slog.Error("cordum-llm-chat: mcp client construction failed", "error", err)
		os.Exit(1)
	}
	defer mcpClient.Close()

	auditChainer := audit.NewChainer(redisClient, "audit:chain:")
	bootstrapper := llmchat.NewBootstrapper(mcpAdapter{client: mcpClient}, cfg.Tenant, auditChainer)
	bootCtx, cancelBoot := context.WithTimeout(context.Background(), bootstrapTimeout)
	if _, err := bootstrapper.Boot(bootCtx); err != nil {
		cancelBoot()
		slog.Error("cordum-llm-chat: chat-assistant bootstrap failed", "error", err)
		os.Exit(1)
	}
	cancelBoot()

	handlers := llmchat.NewHandlers(provider, redisClient, readyzProbeTimeout)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handlers.Healthz)
	mux.HandleFunc("/readyz", handlers.Readyz)

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

	cfg.ChatAssistantAgentID = strings.TrimSpace(getenv("LLMCHAT_CHAT_ASSISTANT_AGENT_ID"))
	if cfg.ChatAssistantAgentID == "" {
		return runtimeConfig{}, fmt.Errorf("LLMCHAT_CHAT_ASSISTANT_AGENT_ID is required")
	}
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
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
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

// mcpAdapter bridges *llmchat.MCPClient (which takes json.RawMessage
// args + a bearer token) to the llmchat.MCPCallToolClient interface
// (which takes map[string]any). Bootstrap uses the service API key
// (bearerToken=""), so the underlying MCP client falls through to the
// X-API-Key header path — by design, since registration runs before
// any per-session delegation could exist.
type mcpAdapter struct {
	client *llmchat.MCPClient
}

func (a mcpAdapter) CallTool(ctx context.Context, name string, args map[string]any) (*mcp.ToolCallResult, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("mcpAdapter: marshal args: %w", err)
	}
	return a.client.CallTool(ctx, name, raw, "")
}
