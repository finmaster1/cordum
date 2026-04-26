package llmchat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeGateway scripts /api/v1/agents/{id}/delegate + /api/v1/agents/revoke-delegation.
type fakeGateway struct {
	srv          *httptest.Server
	issueCalls   atomic.Int32
	revokeCalls  atomic.Int32
	issueHandler func(req *delegateRequestPayload, attempt int32) (status int, body delegateResponsePayload)
}

type delegateRequestPayload struct {
	TargetAgentID  string   `json:"target_agent_id"`
	AllowedActions []string `json:"allowed_actions"`
	AllowedTopics  []string `json:"allowed_topics"`
	TTLSeconds     int      `json:"ttl_seconds"`
	ParentToken    string   `json:"parent_token"`
}

type delegateResponsePayload struct {
	Token      string `json:"token"`
	KID        string `json:"kid"`
	ExpiresAt  string `json:"expires_at"`
	ChainDepth int    `json:"chain_depth"`
	JTI        string `json:"jti"`
}

func newFakeGateway(t *testing.T) *fakeGateway {
	t.Helper()
	g := &fakeGateway{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/agents/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/delegate") {
			http.NotFound(w, r)
			return
		}
		attempt := g.issueCalls.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var req delegateRequestPayload
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var status int
		var resp delegateResponsePayload
		if g.issueHandler != nil {
			status, resp = g.issueHandler(&req, attempt)
		} else {
			status = http.StatusCreated
			resp = delegateResponsePayload{
				Token:      "fake-token-" + req.TargetAgentID,
				KID:        "kid-1",
				ExpiresAt:  time.Now().Add(15 * time.Minute).UTC().Format(time.RFC3339Nano),
				ChainDepth: 1,
				JTI:        "jti-fake-1",
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/v1/agents/revoke-delegation", func(w http.ResponseWriter, _ *http.Request) {
		g.revokeCalls.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	g.srv = httptest.NewServer(mux)
	t.Cleanup(g.srv.Close)
	return g
}

func newTestDelegationClient(t *testing.T, g *fakeGateway) *DelegationClient {
	t.Helper()
	return NewDelegationClient(DelegationConfig{
		BaseURL:    g.srv.URL,
		AgentID:    "chat-assistant-1",
		APIKey:     "service-key",
		Tenant:     "tenant-a",
		IssueTTL:   15 * time.Minute,
		RetryDelay: 1 * time.Millisecond,
	})
}

func TestDelegationClient_IssueForSession(t *testing.T) {
	t.Parallel()
	g := newFakeGateway(t)
	c := newTestDelegationClient(t, g)

	sess, err := c.IssueForSession(context.Background(), "alice@cordum.io")
	if err != nil {
		t.Fatalf("IssueForSession: %v", err)
	}
	if sess.Token == "" {
		t.Fatal("Token should be populated")
	}
	if sess.JTI == "" {
		t.Fatal("JTI should be populated")
	}
	if sess.ExpiresAt.Before(time.Now()) {
		t.Errorf("ExpiresAt = %v, want future", sess.ExpiresAt)
	}
	if got := g.issueCalls.Load(); got != 1 {
		t.Errorf("issue calls = %d, want 1", got)
	}
}

func TestDelegationClient_OutboundBodyShape(t *testing.T) {
	t.Parallel()
	g := newFakeGateway(t)
	var captured delegateRequestPayload
	g.issueHandler = func(req *delegateRequestPayload, _ int32) (int, delegateResponsePayload) {
		captured = *req
		return http.StatusCreated, delegateResponsePayload{
			Token: "t", JTI: "j", ChainDepth: 1,
			ExpiresAt: time.Now().Add(15 * time.Minute).UTC().Format(time.RFC3339Nano),
		}
	}
	c := newTestDelegationClient(t, g)

	if _, err := c.IssueForSession(context.Background(), "bob@cordum.io"); err != nil {
		t.Fatalf("IssueForSession: %v", err)
	}

	if captured.TargetAgentID != "chat-assistant-1" {
		t.Errorf("target_agent_id = %q, want chat-assistant-1 (self-delegation per chain-depth-1)", captured.TargetAgentID)
	}
	if captured.TTLSeconds != 900 {
		t.Errorf("ttl_seconds = %d, want 900", captured.TTLSeconds)
	}
	if captured.ParentToken != "" {
		t.Errorf("parent_token = %q, want empty (root delegation for chat-assistant)", captured.ParentToken)
	}

	wantActions := []string{"read", "submit_job", "query_policy"}
	if len(captured.AllowedActions) != len(wantActions) {
		t.Errorf("allowed_actions = %v, want %v", captured.AllowedActions, wantActions)
	}
	for i, a := range wantActions {
		if i >= len(captured.AllowedActions) || captured.AllowedActions[i] != a {
			t.Errorf("allowed_actions[%d] = %q, want %q", i, captured.AllowedActions[i], a)
		}
	}
	if len(captured.AllowedTopics) != 1 || captured.AllowedTopics[0] != "job.*" {
		t.Errorf("allowed_topics = %v, want [job.*]", captured.AllowedTopics)
	}
}

func TestDelegationClient_Revoke(t *testing.T) {
	t.Parallel()
	g := newFakeGateway(t)
	c := newTestDelegationClient(t, g)

	if err := c.Revoke(context.Background(), "jti-to-revoke", "session_closed"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if got := g.revokeCalls.Load(); got != 1 {
		t.Errorf("revoke calls = %d, want 1", got)
	}
}

func TestDelegationClient_AutoRefreshNearExpiry(t *testing.T) {
	t.Parallel()
	g := newFakeGateway(t)
	g.issueHandler = func(_ *delegateRequestPayload, attempt int32) (int, delegateResponsePayload) {
		// First mint expires 30s out (< 60s refresh threshold), forcing
		// a re-issue on the next ForSession call.
		exp := 30 * time.Second
		if attempt > 1 {
			exp = 15 * time.Minute
		}
		return http.StatusCreated, delegateResponsePayload{
			Token:      "tok-" + string(rune('a'+attempt)),
			JTI:        "jti-" + string(rune('a'+attempt)),
			ChainDepth: 1,
			ExpiresAt:  time.Now().Add(exp).UTC().Format(time.RFC3339Nano),
		}
	}
	c := newTestDelegationClient(t, g)

	first, err := c.IssueForSession(context.Background(), "alice@cordum.io")
	if err != nil {
		t.Fatalf("IssueForSession: %v", err)
	}
	second, err := c.ForSession(context.Background(), "alice@cordum.io", first)
	if err != nil {
		t.Fatalf("ForSession: %v", err)
	}
	if second.Token == first.Token {
		t.Errorf("ForSession returned same token after near-expiry; expected refresh")
	}
	if got := g.issueCalls.Load(); got != 2 {
		t.Errorf("issue calls = %d, want 2 (one initial + one refresh)", got)
	}
}

func TestDelegationClient_ForSessionReusesFreshToken(t *testing.T) {
	t.Parallel()
	g := newFakeGateway(t)
	c := newTestDelegationClient(t, g)

	first, err := c.IssueForSession(context.Background(), "alice@cordum.io")
	if err != nil {
		t.Fatalf("IssueForSession: %v", err)
	}
	second, err := c.ForSession(context.Background(), "alice@cordum.io", first)
	if err != nil {
		t.Fatalf("ForSession: %v", err)
	}
	if second.Token != first.Token {
		t.Errorf("ForSession should reuse fresh token; first=%s second=%s", first.Token, second.Token)
	}
	if got := g.issueCalls.Load(); got != 1 {
		t.Errorf("issue calls = %d, want 1 (cached)", got)
	}
}

func TestDelegationClient_401IsTypedError(t *testing.T) {
	t.Parallel()
	g := newFakeGateway(t)
	g.issueHandler = func(_ *delegateRequestPayload, _ int32) (int, delegateResponsePayload) {
		return http.StatusUnauthorized, delegateResponsePayload{}
	}
	c := newTestDelegationClient(t, g)

	_, err := c.IssueForSession(context.Background(), "alice@cordum.io")
	if err == nil {
		t.Fatal("expected error on 401, got nil")
	}
	if !errors.Is(err, ErrDelegationUnauthorized) {
		t.Errorf("error = %v, want ErrDelegationUnauthorized", err)
	}
}

func TestDelegationClient_RetryOn5xx(t *testing.T) {
	t.Parallel()
	g := newFakeGateway(t)
	g.issueHandler = func(_ *delegateRequestPayload, attempt int32) (int, delegateResponsePayload) {
		if attempt < 2 {
			return http.StatusServiceUnavailable, delegateResponsePayload{}
		}
		return http.StatusCreated, delegateResponsePayload{
			Token: "tok-after-retry", JTI: "jti", ChainDepth: 1,
			ExpiresAt: time.Now().Add(15 * time.Minute).UTC().Format(time.RFC3339Nano),
		}
	}
	c := newTestDelegationClient(t, g)

	sess, err := c.IssueForSession(context.Background(), "alice@cordum.io")
	if err != nil {
		t.Fatalf("IssueForSession after retry: %v", err)
	}
	if sess.Token != "tok-after-retry" {
		t.Errorf("Token = %q, want tok-after-retry", sess.Token)
	}
	if got := g.issueCalls.Load(); got != 2 {
		t.Errorf("issue calls = %d, want 2", got)
	}
}

func TestDelegationClient_NoRetryOn4xx(t *testing.T) {
	t.Parallel()
	g := newFakeGateway(t)
	g.issueHandler = func(_ *delegateRequestPayload, _ int32) (int, delegateResponsePayload) {
		return http.StatusBadRequest, delegateResponsePayload{}
	}
	c := newTestDelegationClient(t, g)

	_, err := c.IssueForSession(context.Background(), "alice@cordum.io")
	if err == nil {
		t.Fatal("expected error on 400")
	}
	if got := g.issueCalls.Load(); got != 1 {
		t.Errorf("issue calls = %d, want 1 (no retry on 4xx)", got)
	}
}

func TestDelegationClient_ServiceAPIKeyHeader(t *testing.T) {
	t.Parallel()
	var sawKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawKey = r.Header.Get("X-API-Key")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"token":"t","jti":"j","chain_depth":1,"expires_at":"` + time.Now().Add(15*time.Minute).UTC().Format(time.RFC3339Nano) + `"}`))
	}))
	defer srv.Close()

	c := NewDelegationClient(DelegationConfig{
		BaseURL: srv.URL, AgentID: "chat-assistant-1", APIKey: "secret-svc-key",
		Tenant: "t", IssueTTL: 15 * time.Minute, RetryDelay: 1 * time.Millisecond,
	})
	if _, err := c.IssueForSession(context.Background(), "alice@cordum.io"); err != nil {
		t.Fatalf("IssueForSession: %v", err)
	}
	if sawKey != "secret-svc-key" {
		t.Errorf("X-API-Key = %q, want secret-svc-key", sawKey)
	}
}
