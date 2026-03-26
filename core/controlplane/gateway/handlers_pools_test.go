package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cordum/cordum/core/configsvc"
	"github.com/cordum/cordum/core/infra/config"
)

// seedPoolConfig writes a pools config doc to the test configSvc.
func seedPoolConfig(t *testing.T, s *server, topics map[string]any, pools map[string]config.PoolConfig) {
	t.Helper()
	err := s.configSvc.SetWithRetry(context.Background(), configsvc.ScopeSystem, "default", 1, func(doc *configsvc.Document) error {
		if doc.Data == nil {
			doc.Data = map[string]any{}
		}
		doc.Data["pools"] = map[string]any{
			"topics": topics,
			"pools":  pools,
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed pool config: %v", err)
	}
}

func adminRequest(method, url string, body any) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	r := httptest.NewRequest(method, url, &buf)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Cordum-Role", "admin")
	r.Header.Set("X-Cordum-Principal", "test-admin")
	return r
}

// withPathValues sets PathValue entries on a request (stdlib mux doesn't do
// this for direct handler calls — only when routed through the mux).
func withPathValues(r *http.Request, kvs ...string) *http.Request {
	for i := 0; i+1 < len(kvs); i += 2 {
		r.SetPathValue(kvs[i], kvs[i+1])
	}
	return r
}

func TestHandleCreatePool(t *testing.T) {
	s, _, _ := newTestGateway(t)

	// Seed empty config with one topic so extractPoolsFromConfig works
	seedPoolConfig(t, s, map[string]any{"job.existing": "existing-pool"}, map[string]config.PoolConfig{
		"existing-pool": {Requires: []string{}},
	})

	// Create new pool — should succeed
	req := withPathValues(adminRequest("PUT", "/api/v1/pools/new-pool", map[string]any{
		"requires":    []string{"docker"},
		"description": "a test pool",
	}), "name", "new-pool")
	w := httptest.NewRecorder()
	s.handleCreatePool(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Create duplicate — should 409
	req2 := withPathValues(adminRequest("PUT", "/api/v1/pools/new-pool", map[string]any{
		"description": "duplicate",
	}), "name", "new-pool")
	w2 := httptest.NewRecorder()
	s.handleCreatePool(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestHandleUpdatePool(t *testing.T) {
	s, _, _ := newTestGateway(t)
	seedPoolConfig(t, s, map[string]any{"job.test": "test-pool"}, map[string]config.PoolConfig{
		"test-pool": {Requires: []string{"docker"}, Description: "original"},
	})

	// Update existing — should succeed
	req := withPathValues(adminRequest("PATCH", "/api/v1/pools/test-pool", map[string]any{
		"description": "updated",
	}), "name", "test-pool")
	w := httptest.NewRecorder()
	s.handleUpdatePool(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Update non-existent — should 404
	req2 := withPathValues(adminRequest("PATCH", "/api/v1/pools/no-such-pool", map[string]any{
		"description": "nope",
	}), "name", "no-such-pool")
	w2 := httptest.NewRecorder()
	s.handleUpdatePool(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestHandleDeletePool(t *testing.T) {
	s, _, _ := newTestGateway(t)
	seedPoolConfig(t, s, map[string]any{"job.test": "pool-a", "job.other": "pool-b"}, map[string]config.PoolConfig{
		"pool-a": {},
		"pool-b": {},
	})

	// Delete pool with active topic mapping — should fail
	req := withPathValues(adminRequest("DELETE", "/api/v1/pools/pool-a", nil), "name", "pool-a")
	w := httptest.NewRecorder()
	s.handleDeletePool(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for active mapping, got %d: %s", w.Code, w.Body.String())
	}

	// Delete with force — should succeed
	req2 := withPathValues(adminRequest("DELETE", "/api/v1/pools/pool-a?force=true", nil), "name", "pool-a")
	w2 := httptest.NewRecorder()
	s.handleDeletePool(w2, req2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("expected 204 with force, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestHandleDrainPool(t *testing.T) {
	s, _, _ := newTestGateway(t)
	seedPoolConfig(t, s, map[string]any{"job.test": "test-pool"}, map[string]config.PoolConfig{
		"test-pool": {Status: "active"},
	})

	// Drain active pool — should succeed
	req := withPathValues(adminRequest("POST", "/api/v1/pools/test-pool/drain", map[string]any{
		"timeout_seconds": 120,
	}), "name", "test-pool")
	w := httptest.NewRecorder()
	s.handleDrainPool(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Drain already draining — should fail
	req2 := withPathValues(adminRequest("POST", "/api/v1/pools/test-pool/drain", map[string]any{}), "name", "test-pool")
	w2 := httptest.NewRecorder()
	s.handleDrainPool(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for already draining, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestHandleAddRemoveTopic(t *testing.T) {
	s, _, _ := newTestGateway(t)
	seedPoolConfig(t, s, map[string]any{"job.existing": "test-pool"}, map[string]config.PoolConfig{
		"test-pool": {},
	})

	// Add topic — should succeed
	req := withPathValues(adminRequest("PUT", "/api/v1/pools/test-pool/topics/job.new.topic", nil), "name", "test-pool", "topic", "job.new.topic")
	w := httptest.NewRecorder()
	s.handleAddTopicToPool(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Remove topic — should succeed
	req2 := withPathValues(adminRequest("DELETE", "/api/v1/pools/test-pool/topics/job.new.topic", nil), "name", "test-pool", "topic", "job.new.topic")
	w2 := httptest.NewRecorder()
	s.handleRemoveTopicFromPool(w2, req2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w2.Code, w2.Body.String())
	}
}
