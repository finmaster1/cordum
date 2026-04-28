package llmchat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newMiniredisClient(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis start: %v", err)
	}
	t.Cleanup(mr.Close)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()}), mr
}

func decodeReadyBody(t *testing.T, resp *http.Response) readyBody {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var body readyBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return body
}

func TestHandlers_Healthz(t *testing.T) {
	t.Parallel()

	rdb, _ := newMiniredisClient(t)
	h := NewHandlers(NewMockProvider(), rdb, time.Second)

	rec := httptest.NewRecorder()
	h.Healthz(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	body := decodeReadyBody(t, rec.Result())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%+v", rec.Code, body)
	}
	if body.Status != "ok" || body.Vllm != "ok" || body.Knowledge != "ok" {
		t.Errorf("body = %+v, want provider+knowledge ok", body)
	}
}

func TestHandlers_Healthz_ProviderDown(t *testing.T) {
	t.Parallel()

	rdb, _ := newMiniredisClient(t)
	mockProv := NewMockProvider()
	mockProv.SetHealthErr(errors.New("ollama down"))
	h := NewHandlers(mockProv, rdb, time.Second)

	rec := httptest.NewRecorder()
	h.Healthz(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	body := decodeReadyBody(t, rec.Result())
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503, body=%+v", rec.Code, body)
	}
	if !strings.Contains(body.Vllm, "ollama down") {
		t.Errorf("vllm = %q, want provider error", body.Vllm)
	}
	if body.Knowledge != "ok" {
		t.Errorf("knowledge = %q, want ok", body.Knowledge)
	}
}

func TestHandlers_Healthz_KnowledgeDown(t *testing.T) {
	t.Parallel()

	rdb, _ := newMiniredisClient(t)
	h := NewHandlers(NewMockProvider(), rdb, time.Second, WithKnowledgeCheck(func(_ context.Context) error {
		return errors.New("missing openapi")
	}))

	rec := httptest.NewRecorder()
	h.Healthz(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	body := decodeReadyBody(t, rec.Result())
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503, body=%+v", rec.Code, body)
	}
	if body.Vllm != "ok" {
		t.Errorf("vllm = %q, want ok", body.Vllm)
	}
	if !strings.Contains(body.Knowledge, "missing openapi") {
		t.Errorf("knowledge = %q, want missing openapi", body.Knowledge)
	}
}

func TestHandlers_Livez_ProcessOnly(t *testing.T) {
	t.Parallel()

	rdb, mr := newMiniredisClient(t)
	mr.Close()
	mockProv := NewMockProvider()
	mockProv.SetHealthErr(errors.New("provider down"))
	h := NewHandlers(mockProv, rdb, time.Second, WithKnowledgeCheck(func(_ context.Context) error {
		return errors.New("knowledge down")
	}))

	rec := httptest.NewRecorder()
	h.Livez(rec, httptest.NewRequest(http.MethodGet, "/livez", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestHandlers_Readyz_AllOK(t *testing.T) {
	t.Parallel()

	rdb, _ := newMiniredisClient(t)
	mockProv := NewMockProvider()
	h := NewHandlers(mockProv, rdb, time.Second)

	rec := httptest.NewRecorder()
	h.Readyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	body := decodeReadyBody(t, rec.Result())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%+v", rec.Code, body)
	}
	if body.Status != "ok" || body.Redis != "ok" || body.Vllm != "ok" || body.Knowledge != "ok" {
		t.Errorf("body = %+v, want all ok", body)
	}
	if got := mockProv.HealthCalls(); got != 1 {
		t.Errorf("HealthCalls = %d, want 1", got)
	}
}

func TestHandlers_Readyz_RedisDown(t *testing.T) {
	t.Parallel()

	rdb, mr := newMiniredisClient(t)
	mr.Close() // simulate redis outage; rdb's connect attempts will fail

	mockProv := NewMockProvider()
	h := NewHandlers(mockProv, rdb, 200*time.Millisecond)

	rec := httptest.NewRecorder()
	h.Readyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	body := decodeReadyBody(t, rec.Result())
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503, body=%+v", rec.Code, body)
	}
	if !strings.HasPrefix(body.Redis, "fail:") {
		t.Errorf("redis = %q, want fail:* prefix", body.Redis)
	}
	if body.Vllm != "ok" {
		t.Errorf("vllm = %q, want ok (provider was healthy)", body.Vllm)
	}
	if body.Status != "degraded" {
		t.Errorf("status = %q, want degraded", body.Status)
	}
}

func TestHandlers_Readyz_VLLMDown(t *testing.T) {
	t.Parallel()

	rdb, _ := newMiniredisClient(t)
	mockProv := NewMockProvider()
	mockProv.SetHealthErr(errors.New("connection refused"))
	h := NewHandlers(mockProv, rdb, time.Second)

	rec := httptest.NewRecorder()
	h.Readyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	body := decodeReadyBody(t, rec.Result())
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503, body=%+v", rec.Code, body)
	}
	if body.Redis != "ok" {
		t.Errorf("redis = %q, want ok", body.Redis)
	}
	if !strings.Contains(body.Vllm, "connection refused") {
		t.Errorf("vllm = %q, want substring 'connection refused'", body.Vllm)
	}
}

func TestHandlers_Readyz_BothDown(t *testing.T) {
	t.Parallel()

	rdb, mr := newMiniredisClient(t)
	mr.Close()

	mockProv := NewMockProvider()
	mockProv.SetHealthErr(errors.New("vllm not loaded"))
	h := NewHandlers(mockProv, rdb, 200*time.Millisecond)

	rec := httptest.NewRecorder()
	h.Readyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	body := decodeReadyBody(t, rec.Result())
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if !strings.HasPrefix(body.Redis, "fail:") {
		t.Errorf("redis = %q, want fail:* prefix", body.Redis)
	}
	if !strings.HasPrefix(body.Vllm, "fail:") {
		t.Errorf("vllm = %q, want fail:* prefix", body.Vllm)
	}
}

func TestHandlers_Readyz_DefaultProbeTimeout(t *testing.T) {
	t.Parallel()

	rdb, _ := newMiniredisClient(t)
	// Pass 0 to exercise the timeout fallback path.
	h := NewHandlers(NewMockProvider(), rdb, 0)
	if h.timeout != 2*time.Second {
		t.Errorf("default timeout = %v, want 2s", h.timeout)
	}
}
