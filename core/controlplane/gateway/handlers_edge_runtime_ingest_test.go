package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	edgecore "github.com/cordum/cordum/core/edge"
)

// enableRuntimeIngest flips the gateway feature flag on for the lifetime of
// the test. The default (unset) must stay 503 service_unavailable.
func enableRuntimeIngest(t *testing.T) {
	t.Helper()
	t.Setenv(envRuntimeIngestEnabled, "true")
}

func disableRuntimeIngest(t *testing.T) {
	t.Helper()
	t.Setenv(envRuntimeIngestEnabled, "")
}

func runtimeIngestBody(sessionID, executionID, tenantID, sourceEventID string) string {
	return `{
		"source": {"source_id":"tetragon-test"},
		"batch_id":"batch-` + sourceEventID + `",
		"events": [{
			"tenant_id":"` + tenantID + `",
			"session_id":"` + sessionID + `",
			"execution_id":"` + executionID + `",
			"source_event_id":"` + sourceEventID + `",
			"observed_at":"2026-05-17T12:00:00Z",
			"kind":"runtime.process.exec",
			"process": {"executable_basename":"curl","argument_count":1}
		}]
	}`
}

func TestRuntimeIngestDisabledReturns503(t *testing.T) {
	disableRuntimeIngest(t)
	_, handler := newEdgeRouteTestServer(t)
	session := createEdgeRouteSession(t, handler)
	execution := createEdgeRouteExecution(t, handler, session.SessionID)

	rr := edgeRoutePOST(t, handler, "/api/v1/edge/runtime/events",
		runtimeIngestBody(session.SessionID, execution.ExecutionID, edgeRouteTenant, "rt-evt-disabled"))

	assertEdgeErrorShape(t, rr, http.StatusServiceUnavailable, edgeErrCodeServiceUnavailable)
}

func TestRuntimeIngestEnabledRequiresTenantHeader(t *testing.T) {
	enableRuntimeIngest(t)
	_, handler := newEdgeRouteTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/edge/runtime/events",
		strings.NewReader(runtimeIngestBody("ignored", "ignored", edgeRouteTenant, "no-tenant-header")))
	addEdgeRouteAuth(req)
	req.Header.Set("Content-Type", "application/json")
	// Deliberately omit X-Tenant-ID.
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assertEdgeErrorShape(t, rr, http.StatusBadRequest, edgeErrCodeTenantRequired)
}

func TestRuntimeIngestEnabledRequiresAuth(t *testing.T) {
	enableRuntimeIngest(t)
	_, handler := newEdgeRouteTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/edge/runtime/events",
		strings.NewReader(runtimeIngestBody("s", "e", edgeRouteTenant, "no-auth")))
	req.Header.Set("X-Tenant-ID", edgeRouteTenant)
	req.Header.Set("Content-Type", "application/json")
	// Deliberately omit X-API-Key.
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized && rr.Code != http.StatusForbidden {
		t.Fatalf("no-auth status = %d, want 401 or 403 body=%s", rr.Code, rr.Body.String())
	}
}

func TestRuntimeIngestEnabledRejectsBodyTenantMismatch(t *testing.T) {
	enableRuntimeIngest(t)
	fix := newCrossTenantFixture(t)
	// header=A, body events stamp tenant=B → 403 tenant_mismatch.
	body := runtimeIngestBody(fix.tenantA.session.SessionID, fix.tenantA.execution.ExecutionID, fix.tenantB.tenantID, "mismatch-1")
	rr := fix.asAttacker(t, http.MethodPost, "/api/v1/edge/runtime/events", body)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("body-tenant mismatch status = %d, want 403 body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), edgeErrCodeTenantMismatch) {
		t.Fatalf("missing %q in body: %s", edgeErrCodeTenantMismatch, rr.Body.String())
	}
}

func TestRuntimeIngestEnabledRejectsCrossTenantSession(t *testing.T) {
	enableRuntimeIngest(t)
	fix := newCrossTenantFixture(t)
	body := runtimeIngestBody(fix.tenantB.session.SessionID, fix.tenantB.execution.ExecutionID, fix.tenantA.tenantID, "xtenant-sess")
	rr := fix.asAttacker(t, http.MethodPost, "/api/v1/edge/runtime/events", body)

	assertCrossTenantBlocked(t, rr, "runtime ingest cross-tenant parents")
	fix.assertNoTenantBLeak(t, rr, "runtime ingest cross-tenant parents")
}

func TestRuntimeIngestEnabledRejectsMissingParent(t *testing.T) {
	enableRuntimeIngest(t)
	_, handler := newEdgeRouteTestServer(t)
	body := runtimeIngestBody("edge-session-does-not-exist", "edge-exec-does-not-exist", edgeRouteTenant, "missing-parent")
	rr := edgeRoutePOST(t, handler, "/api/v1/edge/runtime/events", body)
	assertEdgeErrorShape(t, rr, http.StatusNotFound, edgeErrCodeNotFound)
}

func TestRuntimeIngestEnabledRejectsExecutionSessionMismatch(t *testing.T) {
	enableRuntimeIngest(t)
	_, handler := newEdgeRouteTestServer(t)
	session := createEdgeRouteSession(t, handler)
	otherSession := createEdgeRouteSession(t, handler)
	execution := createEdgeRouteExecution(t, handler, session.SessionID)
	body := runtimeIngestBody(otherSession.SessionID, execution.ExecutionID, edgeRouteTenant, "exec-mismatch")
	rr := edgeRoutePOST(t, handler, "/api/v1/edge/runtime/events", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("execution mismatch status = %d, want 400 body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), edgeErrCodeExecutionMismatch) {
		t.Fatalf("missing %q: %s", edgeErrCodeExecutionMismatch, rr.Body.String())
	}
}

func TestRuntimeIngestEnabledRejectsEmptyBatch(t *testing.T) {
	enableRuntimeIngest(t)
	_, handler := newEdgeRouteTestServer(t)
	body := `{"source":{"source_id":"tetragon-test"},"events":[]}`
	rr := edgeRoutePOST(t, handler, "/api/v1/edge/runtime/events", body)
	assertEdgeErrorShape(t, rr, http.StatusBadRequest, edgeErrCodeInvalidRequest)
}

func TestRuntimeIngestEnabledRejectsMissingSourceID(t *testing.T) {
	enableRuntimeIngest(t)
	_, handler := newEdgeRouteTestServer(t)
	session := createEdgeRouteSession(t, handler)
	execution := createEdgeRouteExecution(t, handler, session.SessionID)
	body := `{
		"source":{"source_id":""},
		"events":[{
			"tenant_id":"` + edgeRouteTenant + `",
			"session_id":"` + session.SessionID + `",
			"execution_id":"` + execution.ExecutionID + `",
			"source_event_id":"se","observed_at":"2026-05-17T12:00:00Z",
			"kind":"runtime.process.exec",
			"process":{"executable_basename":"curl","argument_count":1}
		}]
	}`
	rr := edgeRoutePOST(t, handler, "/api/v1/edge/runtime/events", body)
	assertEdgeErrorShape(t, rr, http.StatusBadRequest, edgeErrCodeInvalidRequest)
}

func TestRuntimeIngestEnabledRejectsForbiddenRawKey(t *testing.T) {
	enableRuntimeIngest(t)
	_, handler := newEdgeRouteTestServer(t)
	session := createEdgeRouteSession(t, handler)
	execution := createEdgeRouteExecution(t, handler, session.SessionID)
	body := `{
		"source":{"source_id":"tetragon-test"},
		"events":[{
			"tenant_id":"` + edgeRouteTenant + `",
			"session_id":"` + session.SessionID + `",
			"execution_id":"` + execution.ExecutionID + `",
			"source_event_id":"se","observed_at":"2026-05-17T12:00:00Z",
			"kind":"runtime.process.exec",
			"argv":["curl","https://evil.example.com"]
		}]
	}`
	rr := edgeRoutePOST(t, handler, "/api/v1/edge/runtime/events", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("forbidden-key status = %d, want 400 body=%s", rr.Code, rr.Body.String())
	}
	// The error must not echo the raw argv values back to the client.
	if strings.Contains(rr.Body.String(), "evil.example.com") {
		t.Fatalf("error response leaked raw argv: %s", rr.Body.String())
	}
}

func TestRuntimeIngestEnabledRejectsOversizeBatch(t *testing.T) {
	enableRuntimeIngest(t)
	_, handler := newEdgeRouteTestServer(t)
	session := createEdgeRouteSession(t, handler)
	execution := createEdgeRouteExecution(t, handler, session.SessionID)
	// Build a batch with too many events to exceed MaxRuntimeBatchEvents.
	var sb strings.Builder
	sb.WriteString(`{"source":{"source_id":"tetragon-test"},"events":[`)
	for i := range 300 {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{
			"tenant_id":"` + edgeRouteTenant + `",
			"session_id":"` + session.SessionID + `",
			"execution_id":"` + execution.ExecutionID + `",
			"source_event_id":"se-` + strings.Repeat("z", i+1) + `",
			"observed_at":"2026-05-17T12:00:00Z",
			"kind":"runtime.process.exec",
			"process":{"executable_basename":"curl","argument_count":1}
		}`)
	}
	sb.WriteString("]}")
	rr := edgeRoutePOST(t, handler, "/api/v1/edge/runtime/events", sb.String())
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize batch status = %d, want 413 body=%s", rr.Code, rr.Body.String())
	}
}

func TestRuntimeIngestEnabledAppendsEventsThroughStore(t *testing.T) {
	enableRuntimeIngest(t)
	s, handler := newEdgeRouteTestServer(t)
	session := createEdgeRouteSession(t, handler)
	execution := createEdgeRouteExecution(t, handler, session.SessionID)

	rr := edgeRoutePOST(t, handler, "/api/v1/edge/runtime/events",
		runtimeIngestBody(session.SessionID, execution.ExecutionID, edgeRouteTenant, "rt-append-1"))
	if rr.Code != http.StatusCreated {
		t.Fatalf("ingest status = %d, want 201 body=%s", rr.Code, rr.Body.String())
	}

	var resp runtimeIngestResponseShape
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rr.Body.String())
	}
	if resp.AcceptedCount != 1 {
		t.Fatalf("accepted_count = %d, want 1", resp.AcceptedCount)
	}

	page := readEdgeEventsFromStore(t, s, execution.ExecutionID)
	var runtimeEvents []edgecore.AgentActionEvent
	for _, ev := range page.Items {
		if ev.Layer == edgecore.LayerRuntime {
			runtimeEvents = append(runtimeEvents, ev)
		}
	}
	if len(runtimeEvents) != 1 {
		t.Fatalf("stored runtime events = %d, want 1", len(runtimeEvents))
	}
	got := runtimeEvents[0]
	if got.Kind != edgecore.EventKindRuntimeProcessExec {
		t.Fatalf("stored Kind = %q, want %q", got.Kind, edgecore.EventKindRuntimeProcessExec)
	}
	if got.Decision != edgecore.DecisionRecorded {
		t.Fatalf("stored Decision = %q, want %q", got.Decision, edgecore.DecisionRecorded)
	}
	if got.TenantID != edgeRouteTenant {
		t.Fatalf("stored TenantID = %q, want %q", got.TenantID, edgeRouteTenant)
	}
}

func TestRuntimeIngestEnabledMethodGETIsRejected(t *testing.T) {
	enableRuntimeIngest(t)
	_, handler := newEdgeRouteTestServer(t)
	rr := edgeRouteGET(t, handler, "/api/v1/edge/runtime/events")
	if rr.Code == http.StatusOK {
		t.Fatalf("GET runtime/events returned 200; want 404/405. body=%s", rr.Body.String())
	}
}

// runtimeIngestResponseShape mirrors handlers_edge_runtime_ingest.go's
// response struct for test decoding without exporting the handler-private
// type. Tests assert on the field via JSON tag, so this shape must stay in
// sync with the production response.
type runtimeIngestResponseShape struct {
	AcceptedCount int `json:"accepted_count"`
	DroppedCount  int `json:"dropped_count"`
}

// TestRuntimeIngestEnabledStoresRedactedRuntimeEvent is the
// persistence/integration test mandated by Phase 7. It creates a real
// session + execution through the gateway (Redis-backed via miniredis),
// posts a runtime batch carrying an AWS-key-shaped path, then reads the
// execution's events back from the store and asserts:
//   - the stored event has Layer=runtime, Kind=runtime.process.exec,
//     Decision=RECORDED, and the correct tenant/session/execution,
//   - Labels carry runtime.source_id stamped by the adapter,
//   - InputRedacted does NOT carry the AWS-key marker,
//   - the response shape matches the documented contract.
func TestRuntimeIngestEnabledStoresRedactedRuntimeEvent(t *testing.T) {
	enableRuntimeIngest(t)
	s, handler := newEdgeRouteTestServer(t)
	session := createEdgeRouteSession(t, handler)
	execution := createEdgeRouteExecution(t, handler, session.SessionID)

	body := `{
		"source":{"source_id":"tetragon-clusterA"},
		"events":[{
			"tenant_id":"` + edgeRouteTenant + `",
			"session_id":"` + session.SessionID + `",
			"execution_id":"` + execution.ExecutionID + `",
			"source_event_id":"rt-integ-1",
			"observed_at":"2026-05-17T12:34:56Z",
			"kind":"runtime.file.read",
			"file":{"operation":"read","path_redacted":"/var/AKIAIOSFODNN7EXAMPLE/data"},
			"labels":{"node":"node-7"}
		}]
	}`
	rr := edgeRoutePOST(t, handler, "/api/v1/edge/runtime/events", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("ingest status = %d, want 201 body=%s", rr.Code, rr.Body.String())
	}
	var resp runtimeIngestResponseShape
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rr.Body.String())
	}
	if resp.AcceptedCount != 1 || resp.DroppedCount != 0 {
		t.Fatalf("response counts = accepted:%d dropped:%d, want 1/0 body=%s", resp.AcceptedCount, resp.DroppedCount, rr.Body.String())
	}

	page := readEdgeEventsFromStore(t, s, execution.ExecutionID)
	var runtimeEvents []edgecore.AgentActionEvent
	for _, ev := range page.Items {
		if ev.Layer == edgecore.LayerRuntime {
			runtimeEvents = append(runtimeEvents, ev)
		}
	}
	if len(runtimeEvents) != 1 {
		t.Fatalf("stored runtime events = %d, want 1; all events=%#v", len(runtimeEvents), page.Items)
	}
	got := runtimeEvents[0]
	if got.Kind != edgecore.EventKindRuntimeFileRead {
		t.Fatalf("Kind = %q, want %q", got.Kind, edgecore.EventKindRuntimeFileRead)
	}
	if got.Decision != edgecore.DecisionRecorded {
		t.Fatalf("Decision = %q, want %q", got.Decision, edgecore.DecisionRecorded)
	}
	if got.TenantID != edgeRouteTenant {
		t.Fatalf("TenantID = %q, want %q", got.TenantID, edgeRouteTenant)
	}
	if got.SessionID != session.SessionID {
		t.Fatalf("SessionID = %q, want %q", got.SessionID, session.SessionID)
	}
	if got.ExecutionID != execution.ExecutionID {
		t.Fatalf("ExecutionID = %q, want %q", got.ExecutionID, execution.ExecutionID)
	}
	if got.Labels["runtime.source_id"] != "tetragon-clusterA" {
		t.Fatalf("Labels.runtime.source_id = %q, want %q", got.Labels["runtime.source_id"], "tetragon-clusterA")
	}
	if got.Labels["node"] != "node-7" {
		t.Fatalf("Labels.node = %q, want %q", got.Labels["node"], "node-7")
	}
	// The AWS-key-shaped substring must NOT survive into the stored
	// AgentActionEvent — that is the entire point of edge redaction.
	storedJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal stored event: %v", err)
	}
	if strings.Contains(string(storedJSON), "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("stored event leaked AKIA token: %s", storedJSON)
	}
}

// TestRuntimeIngestAllEventKinds_EndToEnd drives every supported
// runtime event kind — process.exec, file.read, file.write,
// network.connect, dns.query — through `s.registerRoutes`' mux to
// `/api/v1/edge/runtime/events` with a miniredis-backed store, then
// reads each event back to assert it carries the correct Kind, the
// correct tenant/session/execution, Decision=Recorded, Layer=runtime,
// and that an AWS-key-shaped canary placed in a redactable field of
// each summary struct does NOT survive into the stored event JSON.
//
// Existing coverage only exercised process.exec + (via
// TestRuntimeIngestEnabledStoresRedactedRuntimeEvent) file.read; this
// test closes the gap so all 5 known kinds — including the network /
// DNS pair the K8s detector emits — round-trip end-to-end.
func TestRuntimeIngestAllEventKinds_EndToEnd(t *testing.T) {
	enableRuntimeIngest(t)
	const canary = "AKIAIOSFODNN7EXAMPLE"

	cases := []struct {
		// kind is the wire `kind` string + the persisted EventKind.
		kind edgecore.EventKind
		// summaryJSON is the per-kind summary-block fragment. The
		// canary is embedded in a redactable field so we can assert
		// it does not survive into the stored event JSON.
		summaryJSON string
		// labelKey/labelValue is appended to the envelope's labels so
		// each subtest carries a unique marker the store filter can
		// pin against (so a partial-rejection regression cannot silently
		// pass by re-persisting an earlier kind).
		labelKey   string
		labelValue string
	}{
		{
			kind: edgecore.EventKindRuntimeProcessExec,
			// Canary placed with explicit word boundaries on both sides
			// (hyphen + period) so awsKeyPattern \bAKIA[0-9A-Z]{16}\b
			// can match; lowercase trailers would suppress the trailing
			// \b boundary and mask the regex.
			summaryJSON: `"process":{"executable_basename":"curl-` + canary + `.bin","argument_count":2}`,
			labelKey:    "kind_marker",
			labelValue:  "process-exec",
		},
		{
			kind:        edgecore.EventKindRuntimeFileRead,
			summaryJSON: `"file":{"operation":"read","path_redacted":"/var/log/` + canary + `/audit.log"}`,
			labelKey:    "kind_marker",
			labelValue:  "file-read",
		},
		{
			kind:        edgecore.EventKindRuntimeFileWrite,
			summaryJSON: `"file":{"operation":"write","path_redacted":"/srv/data/` + canary + `/staging.bin"}`,
			labelKey:    "kind_marker",
			labelValue:  "file-write",
		},
		{
			kind:        edgecore.EventKindRuntimeNetworkConnect,
			summaryJSON: `"network":{"host_redacted":"` + canary + `.s3.amazonaws.com","port":443,"protocol":"tcp"}`,
			labelKey:    "kind_marker",
			labelValue:  "network-connect",
		},
		{
			kind:        edgecore.EventKindRuntimeDNSQuery,
			summaryJSON: `"dns":{"qname_redacted":"` + canary + `.example.internal","qtype":"A"}`,
			labelKey:    "kind_marker",
			labelValue:  "dns-query",
		},
	}

	// Share the server + session + execution across subtests so the
	// table emits 5 events into the same execution, then each
	// subtest filters by its own kind+marker. This catches partial-
	// rejection / wrong-kind-persistence regressions.
	s, handler := newEdgeRouteTestServer(t)
	session := createEdgeRouteSession(t, handler)
	execution := createEdgeRouteExecution(t, handler, session.SessionID)

	for i, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			sourceEventID := "rt-all-kinds-" + string(tc.kind)
			body := `{
				"source":{"source_id":"tetragon-all-kinds"},
				"batch_id":"batch-all-kinds-` + tc.labelValue + `",
				"events":[{
					"tenant_id":"` + edgeRouteTenant + `",
					"session_id":"` + session.SessionID + `",
					"execution_id":"` + execution.ExecutionID + `",
					"source_event_id":"` + sourceEventID + `",
					"observed_at":"2026-05-17T12:00:0` + string(rune('0'+i)) + `Z",
					"kind":"` + string(tc.kind) + `",
					` + tc.summaryJSON + `,
					"labels":{"` + tc.labelKey + `":"` + tc.labelValue + `"}
				}]
			}`
			rr := edgeRoutePOST(t, handler, "/api/v1/edge/runtime/events", body)
			if rr.Code != http.StatusCreated {
				t.Fatalf("kind=%q POST status = %d, want 201 body=%s", tc.kind, rr.Code, rr.Body.String())
			}
			var resp runtimeIngestResponseShape
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("kind=%q decode response: %v body=%s", tc.kind, err, rr.Body.String())
			}
			if resp.AcceptedCount != 1 || resp.DroppedCount != 0 {
				t.Fatalf("kind=%q counts accepted=%d dropped=%d, want 1/0 body=%s",
					tc.kind, resp.AcceptedCount, resp.DroppedCount, rr.Body.String())
			}

			page := readEdgeEventsFromStore(t, s, execution.ExecutionID)
			var matched *edgecore.AgentActionEvent
			for j := range page.Items {
				ev := &page.Items[j]
				if ev.Layer == edgecore.LayerRuntime && ev.Kind == tc.kind && ev.Labels[tc.labelKey] == tc.labelValue {
					matched = ev
					break
				}
			}
			if matched == nil {
				t.Fatalf("kind=%q: no stored runtime event matched marker %q=%q; events=%#v",
					tc.kind, tc.labelKey, tc.labelValue, page.Items)
			}
			if matched.Decision != edgecore.DecisionRecorded {
				t.Fatalf("kind=%q Decision = %q, want %q", tc.kind, matched.Decision, edgecore.DecisionRecorded)
			}
			if matched.TenantID != edgeRouteTenant {
				t.Fatalf("kind=%q TenantID = %q, want %q", tc.kind, matched.TenantID, edgeRouteTenant)
			}
			if matched.SessionID != session.SessionID {
				t.Fatalf("kind=%q SessionID = %q, want %q", tc.kind, matched.SessionID, session.SessionID)
			}
			if matched.ExecutionID != execution.ExecutionID {
				t.Fatalf("kind=%q ExecutionID = %q, want %q", tc.kind, matched.ExecutionID, execution.ExecutionID)
			}
			if matched.Labels["runtime.source_id"] != "tetragon-all-kinds" {
				t.Fatalf("kind=%q Labels.runtime.source_id = %q, want %q",
					tc.kind, matched.Labels["runtime.source_id"], "tetragon-all-kinds")
			}
			storedJSON, err := json.Marshal(matched)
			if err != nil {
				t.Fatalf("kind=%q marshal: %v", tc.kind, err)
			}
			if strings.Contains(string(storedJSON), canary) {
				t.Fatalf("kind=%q stored event leaked AKIA canary %q: %s", tc.kind, canary, storedJSON)
			}
		})
	}
}

// TestRuntimeIngestEnabledAllOrNothingOnOneBadEnvelope locks in the
// all-or-nothing partial-rejection contract: a batch with one valid
// envelope and one invalid envelope must reject the whole batch and
// persist zero events. This prevents a future refactor from silently
// permitting partial persistence — partial appends would defeat the
// idempotency story and create gaps in the evidence stream.
func TestRuntimeIngestEnabledAllOrNothingOnOneBadEnvelope(t *testing.T) {
	enableRuntimeIngest(t)
	s, handler := newEdgeRouteTestServer(t)
	session := createEdgeRouteSession(t, handler)
	execution := createEdgeRouteExecution(t, handler, session.SessionID)
	body := `{
		"source":{"source_id":"tetragon-test"},
		"events":[
			{
				"tenant_id":"` + edgeRouteTenant + `",
				"session_id":"` + session.SessionID + `",
				"execution_id":"` + execution.ExecutionID + `",
				"source_event_id":"good-1","observed_at":"2026-05-17T12:00:00Z",
				"kind":"runtime.process.exec",
				"process":{"executable_basename":"curl","argument_count":1}
			},
			{
				"tenant_id":"` + edgeRouteTenant + `",
				"session_id":"` + session.SessionID + `",
				"execution_id":"` + execution.ExecutionID + `",
				"source_event_id":"","observed_at":"2026-05-17T12:00:01Z",
				"kind":"runtime.process.exec",
				"process":{"executable_basename":"curl","argument_count":1}
			}
		]
	}`
	rr := edgeRoutePOST(t, handler, "/api/v1/edge/runtime/events", body)
	if rr.Code == http.StatusCreated {
		t.Fatalf("partial-batch must not return 201; got %d body=%s", rr.Code, rr.Body.String())
	}
	page := readEdgeEventsFromStore(t, s, execution.ExecutionID)
	for _, ev := range page.Items {
		if ev.Layer == edgecore.LayerRuntime {
			t.Fatalf("partial-batch leaked runtime event %q (Layer=%q) into store", ev.EventID, ev.Layer)
		}
	}
}
