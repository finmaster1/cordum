package gateway

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/cordum/cordum/core/controlplane/gateway/auth"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

const (
	envLLMChatURL            = "CORDUM_LLM_CHAT_URL"
	envLLMChatForwardAPIKey  = "CORDUM_LLM_CHAT_FORWARD_API_KEY" // #nosec G101 -- environment variable name only.
	envLLMChatTLSCA          = "CORDUM_LLM_CHAT_TLS_CA"
	envLLMChatTLSInsecure    = "CORDUM_LLM_CHAT_TLS_INSECURE"
	envFallbackPrincipalID   = "CORDUM_PRINCIPAL_ID"
	envFallbackPrincipalRole = "CORDUM_PRINCIPAL_ROLE"
	defaultLLMChatURL        = "http://llm-chat:8090"
	llmChatFeatureName       = "llm_chat_assistant"
)

// handleLLMChatProxy keeps the browser-facing chat API on the gateway origin
// while preserving the cordum-llm-chat trust boundary: the gateway remains the
// only component that authenticates the user, then forwards a service API key
// plus trusted identity headers to the chat service.
func (s *server) handleLLMChatProxy(w http.ResponseWriter, r *http.Request) {
	if !s.requireFeatureEntitlement(w, llmChatFeatureName, "LLM chat assistant requires an Enterprise license") {
		return
	}

	upstream, err := llmChatUpstreamURL()
	if err != nil {
		slog.Warn("llmchat proxy upstream invalid", "error", err)
		writeErrorJSON(w, http.StatusServiceUnavailable, "llm chat upstream unavailable")
		return
	}
	forwardKey := llmChatForwardAPIKey()
	if forwardKey == "" {
		slog.Error("llmchat proxy forward key missing")
		writeErrorJSON(w, http.StatusServiceUnavailable, "llm chat upstream unavailable")
		return
	}
	transport, err := llmChatProxyTransport()
	if err != nil {
		slog.Warn("llmchat proxy TLS config invalid", "error", err)
		writeErrorJSON(w, http.StatusServiceUnavailable, "llm chat upstream unavailable")
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(upstream)
	if transport != nil {
		proxy.Transport = transport
	}
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Path = llmChatUpstreamPath(upstream, r.URL.Path)
		req.URL.RawPath = ""
		if r.URL.Path == "/api/v1/chat/healthz" {
			req.URL.RawQuery = ""
		}
		req.Host = upstream.Host

		// Do not pass end-user credentials to the private service. Replace them
		// with the gateway->llm-chat service key and identity headers derived
		// from the authenticated gateway context.
		otel.GetTextMapPropagator().Inject(req.Context(), propagation.HeaderCarrier(req.Header))
		req.Header.Del("Authorization")
		req.Header.Set("X-API-Key", forwardKey)
		req.Header.Set("X-Cordum-Tenant", tenantFromRequest(r))
		if authCtx := auth.FromRequest(r); authCtx != nil {
			principal := strings.TrimSpace(authCtx.PrincipalID)
			if principal == "" {
				principal = strings.TrimSpace(os.Getenv(envFallbackPrincipalID))
			}
			role := strings.TrimSpace(authCtx.Role)
			if role == "" {
				role = strings.TrimSpace(os.Getenv(envFallbackPrincipalRole))
			}
			req.Header.Set("X-Cordum-Principal", principal)
			req.Header.Set("X-Cordum-Role", role)
			if authCtx.AllowCrossTenant {
				req.Header.Set("X-Cordum-Allow-Cross-Tenant", "true")
			} else {
				req.Header.Del("X-Cordum-Allow-Cross-Tenant")
			}
		} else {
			req.Header.Del("X-Cordum-Principal")
			req.Header.Del("X-Cordum-Role")
			req.Header.Del("X-Cordum-Allow-Cross-Tenant")
		}
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		if r.URL.Path == "/readyz" || r.URL.Path == "/api/v1/chat/healthz" {
			// The dashboard probes every 10s and the llmchat compose profile is
			// often disabled on non-GPU developer machines. Return unavailable
			// without warning-level log spam; the hidden button is the signal.
			slog.Debug("llmchat readiness proxy unavailable", "error", err)
			writeErrorJSON(w, http.StatusServiceUnavailable, "llm chat upstream unavailable")
			return
		}
		slog.Warn("llmchat proxy request failed", "path", r.URL.Path, "error", err)
		writeErrorJSON(w, http.StatusBadGateway, "llm chat upstream unavailable")
	}
	proxy.ServeHTTP(w, r)
}

func llmChatUpstreamURL() (*url.URL, error) {
	raw := strings.TrimSpace(os.Getenv(envLLMChatURL))
	if raw == "" {
		raw = defaultLLMChatURL
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return nil, fmt.Errorf("missing host")
	}
	return parsed, nil
}

func llmChatForwardAPIKey() string {
	if key := strings.TrimSpace(os.Getenv(envLLMChatForwardAPIKey)); key != "" {
		return key
	}
	return strings.TrimSpace(os.Getenv("CORDUM_API_KEY"))
}

func llmChatProxyTransport() (http.RoundTripper, error) {
	caPath := strings.TrimSpace(os.Getenv(envLLMChatTLSCA))
	insecure := parseTruthyEnv(os.Getenv(envLLMChatTLSInsecure))
	if caPath == "" && !insecure {
		return nil, nil
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if insecure {
		// #nosec G402 -- explicit operator-controlled dev escape hatch.
		tlsConfig.InsecureSkipVerify = true
	}
	if caPath != "" {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		// #nosec G304 -- CA path is operator-configured via CORDUM_LLM_CHAT_TLS_CA.
		pem, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", envLLMChatTLSCA, err)
		}
		if ok := pool.AppendCertsFromPEM(pem); !ok {
			return nil, fmt.Errorf("parse %s: no certificates found in %s", envLLMChatTLSCA, caPath)
		}
		tlsConfig.RootCAs = pool
	}
	transport.TLSClientConfig = tlsConfig
	return transport, nil
}

func parseTruthyEnv(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func llmChatUpstreamPath(upstream *url.URL, requestPath string) string {
	if requestPath == "/api/v1/chat/healthz" {
		return joinURLPath(upstream.Path, "/readyz")
	}
	return joinURLPath(upstream.Path, requestPath)
}

func joinURLPath(base, path string) string {
	switch {
	case base == "":
		if strings.HasPrefix(path, "/") {
			return path
		}
		return "/" + path
	case strings.HasSuffix(base, "/") && strings.HasPrefix(path, "/"):
		return base + strings.TrimPrefix(path, "/")
	case !strings.HasSuffix(base, "/") && !strings.HasPrefix(path, "/"):
		return base + "/" + path
	default:
		return base + path
	}
}
