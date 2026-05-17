package runtimeingest

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	edgecore "github.com/cordum/cordum/core/edge"
)

const (
	testTenantID    = "tenant-rt-1"
	testSessionID   = "edge-session-rt-1"
	testExecutionID = "edge-exec-rt-1"
	testSourceID    = "tetragon-test"
)

func testObservedAt() time.Time {
	return time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
}

func testEnvelope(kind string, mutators ...func(*RuntimeEventEnvelope)) RuntimeEventEnvelope {
	env := RuntimeEventEnvelope{
		TenantID:      testTenantID,
		SessionID:     testSessionID,
		ExecutionID:   testExecutionID,
		SourceEventID: "src-evt-" + kind,
		ObservedAt:    testObservedAt(),
		Kind:          kind,
	}
	switch kind {
	case KindProcessExec:
		env.Process = &ProcessSummary{
			ExecutableBasename: "curl",
			ExecutableSHA256:   "sha256:abc",
			ArgumentCount:      3,
		}
	case KindFileRead, KindFileWrite:
		op := "read"
		if kind == KindFileWrite {
			op = "write"
		}
		env.File = &FileSummary{Operation: op, PathRedacted: "/tmp/data"}
	case KindNetworkConnect:
		env.Network = &NetworkSummary{HostRedacted: "api.example.com", IPPrefix: "10.0.0.0/24", Port: 443, Protocol: "tcp"}
	case KindDNSQuery:
		env.DNS = &DNSSummary{QNameRedacted: "api.example.com", QType: "A"}
	}
	for _, m := range mutators {
		m(&env)
	}
	return env
}

func testBatch(events ...RuntimeEventEnvelope) RuntimeBatch {
	return RuntimeBatch{
		Source:  SourceIdentity{ID: testSourceID},
		BatchID: "batch-1",
		Events:  events,
	}
}

func mapOne(t *testing.T, kind string, mutators ...func(*RuntimeEventEnvelope)) edgecore.AgentActionEvent {
	t.Helper()
	a := NewAdapter(AdapterOptions{})
	res, err := a.Map(testBatch(testEnvelope(kind, mutators...)))
	if err != nil {
		t.Fatalf("Map %s: %v", kind, err)
	}
	if len(res.Events) != 1 {
		t.Fatalf("Map %s: events = %d, want 1", kind, len(res.Events))
	}
	return res.Events[0]
}

func TestAdapterMapsProcessExec(t *testing.T) {
	got := mapOne(t, KindProcessExec)
	if got.Kind != edgecore.EventKindRuntimeProcessExec {
		t.Fatalf("Kind = %q, want %q", got.Kind, edgecore.EventKindRuntimeProcessExec)
	}
	if got.Layer != edgecore.LayerRuntime {
		t.Fatalf("Layer = %q, want %q", got.Layer, edgecore.LayerRuntime)
	}
	if got.Decision != edgecore.DecisionRecorded {
		t.Fatalf("Decision = %q, want %q", got.Decision, edgecore.DecisionRecorded)
	}
	if got.RuleTier != "" {
		t.Fatalf("RuleTier = %q, want empty", got.RuleTier)
	}
	if got.Status != edgecore.ActionStatusOK {
		t.Fatalf("Status = %q, want %q", got.Status, edgecore.ActionStatusOK)
	}
	if got.TenantID != testTenantID || got.SessionID != testSessionID || got.ExecutionID != testExecutionID {
		t.Fatalf("identity = %q/%q/%q, want %q/%q/%q", got.TenantID, got.SessionID, got.ExecutionID, testTenantID, testSessionID, testExecutionID)
	}
	if got.EventID == "" {
		t.Fatalf("EventID empty")
	}
	if got.Timestamp.IsZero() {
		t.Fatalf("Timestamp zero")
	}
	if got.InputRedacted["executable_basename"] != "curl" {
		t.Fatalf("InputRedacted.executable_basename = %#v, want curl", got.InputRedacted["executable_basename"])
	}
	// Validate against the existing edge contract.
	if err := got.Validate(); err != nil {
		t.Fatalf("AgentActionEvent.Validate: %v", err)
	}
}

func TestAdapterMapsFileRead(t *testing.T) {
	got := mapOne(t, KindFileRead)
	if got.Kind != edgecore.EventKindRuntimeFileRead {
		t.Fatalf("Kind = %q, want %q", got.Kind, edgecore.EventKindRuntimeFileRead)
	}
	if got.Layer != edgecore.LayerRuntime {
		t.Fatalf("Layer = %q", got.Layer)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestAdapterMapsFileWrite(t *testing.T) {
	got := mapOne(t, KindFileWrite)
	if got.Kind != edgecore.EventKindRuntimeFileWrite {
		t.Fatalf("Kind = %q, want %q", got.Kind, edgecore.EventKindRuntimeFileWrite)
	}
}

func TestAdapterMapsNetworkConnect(t *testing.T) {
	got := mapOne(t, KindNetworkConnect)
	if got.Kind != edgecore.EventKindRuntimeNetworkConnect {
		t.Fatalf("Kind = %q, want %q", got.Kind, edgecore.EventKindRuntimeNetworkConnect)
	}
	if _, ok := got.InputRedacted["port"]; !ok {
		t.Fatalf("InputRedacted missing port: %#v", got.InputRedacted)
	}
	if got.InputRedacted["protocol"] != "tcp" {
		t.Fatalf("InputRedacted.protocol = %#v, want tcp", got.InputRedacted["protocol"])
	}
}

func TestAdapterMapsDNSQuery(t *testing.T) {
	got := mapOne(t, KindDNSQuery)
	if got.Kind != edgecore.EventKindRuntimeDNSQuery {
		t.Fatalf("Kind = %q, want %q", got.Kind, edgecore.EventKindRuntimeDNSQuery)
	}
}

func TestAdapterMapsBatchStableEventID(t *testing.T) {
	a := NewAdapter(AdapterOptions{})
	env := testEnvelope(KindProcessExec)
	res1, err := a.Map(testBatch(env))
	if err != nil {
		t.Fatalf("first map: %v", err)
	}
	res2, err := a.Map(testBatch(env))
	if err != nil {
		t.Fatalf("second map: %v", err)
	}
	if res1.Events[0].EventID != res2.Events[0].EventID {
		t.Fatalf("EventID not stable: %q vs %q", res1.Events[0].EventID, res2.Events[0].EventID)
	}
}

func TestAdapterRejectsUnknownKind(t *testing.T) {
	a := NewAdapter(AdapterOptions{})
	_, err := a.Map(testBatch(testEnvelope("runtime.process.exec", func(e *RuntimeEventEnvelope) { e.Kind = "runtime.bogus" })))
	if err == nil {
		t.Fatalf("expected error for unknown kind")
	}
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("err = %v, want ErrInvalidEnvelope", err)
	}
}

func TestAdapterRejectsMissingTenantID(t *testing.T) {
	a := NewAdapter(AdapterOptions{})
	_, err := a.Map(testBatch(testEnvelope(KindProcessExec, func(e *RuntimeEventEnvelope) { e.TenantID = " " })))
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("err = %v, want ErrInvalidEnvelope", err)
	}
}

func TestAdapterRejectsMissingSessionID(t *testing.T) {
	a := NewAdapter(AdapterOptions{})
	_, err := a.Map(testBatch(testEnvelope(KindProcessExec, func(e *RuntimeEventEnvelope) { e.SessionID = "" })))
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("err = %v, want ErrInvalidEnvelope", err)
	}
}

func TestAdapterRejectsMissingExecutionID(t *testing.T) {
	a := NewAdapter(AdapterOptions{})
	_, err := a.Map(testBatch(testEnvelope(KindProcessExec, func(e *RuntimeEventEnvelope) { e.ExecutionID = "" })))
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("err = %v, want ErrInvalidEnvelope", err)
	}
}

func TestAdapterRejectsMissingSourceID(t *testing.T) {
	a := NewAdapter(AdapterOptions{})
	batch := testBatch(testEnvelope(KindProcessExec))
	batch.Source.ID = "  "
	_, err := a.Map(batch)
	if !errors.Is(err, ErrInvalidBatch) {
		t.Fatalf("err = %v, want ErrInvalidBatch", err)
	}
}

func TestAdapterRejectsMissingSourceEventID(t *testing.T) {
	a := NewAdapter(AdapterOptions{})
	_, err := a.Map(testBatch(testEnvelope(KindProcessExec, func(e *RuntimeEventEnvelope) { e.SourceEventID = "" })))
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("err = %v, want ErrInvalidEnvelope", err)
	}
}

func TestAdapterRejectsMissingObservedAt(t *testing.T) {
	a := NewAdapter(AdapterOptions{})
	_, err := a.Map(testBatch(testEnvelope(KindProcessExec, func(e *RuntimeEventEnvelope) { e.ObservedAt = time.Time{} })))
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("err = %v, want ErrInvalidEnvelope", err)
	}
}

func TestAdapterRejectsEmptyBatch(t *testing.T) {
	a := NewAdapter(AdapterOptions{})
	_, err := a.Map(testBatch())
	if !errors.Is(err, ErrInvalidBatch) {
		t.Fatalf("err = %v, want ErrInvalidBatch", err)
	}
}

func TestAdapterRejectsBatchExceedingMaxEvents(t *testing.T) {
	a := NewAdapter(AdapterOptions{})
	events := make([]RuntimeEventEnvelope, MaxRuntimeBatchEvents+1)
	for i := range events {
		events[i] = testEnvelope(KindProcessExec, func(e *RuntimeEventEnvelope) { e.SourceEventID = "src-" + strings.Repeat("x", i+1) })
	}
	_, err := a.Map(testBatch(events...))
	if !errors.Is(err, ErrRuntimeBatchTooLarge) {
		t.Fatalf("err = %v, want ErrRuntimeBatchTooLarge", err)
	}
}

func TestAdapterRejectsBatchAtBoundaryExactlyMaxIsAccepted(t *testing.T) {
	a := NewAdapter(AdapterOptions{})
	events := make([]RuntimeEventEnvelope, MaxRuntimeBatchEvents)
	for i := range events {
		events[i] = testEnvelope(KindProcessExec, func(e *RuntimeEventEnvelope) {
			e.SourceEventID = "src-bdy-" + strings.Repeat("y", i+1)
		})
	}
	res, err := a.Map(testBatch(events...))
	if err != nil {
		t.Fatalf("boundary batch err = %v", err)
	}
	if len(res.Events) != MaxRuntimeBatchEvents {
		t.Fatalf("boundary batch events = %d, want %d", len(res.Events), MaxRuntimeBatchEvents)
	}
}

func TestAdapterRejectsEnvelopeExceedingMaxBytes(t *testing.T) {
	a := NewAdapter(AdapterOptions{})
	big := strings.Repeat("x", MaxRuntimeEnvelopeBytes+10)
	_, err := a.Map(testBatch(testEnvelope(KindProcessExec, func(e *RuntimeEventEnvelope) {
		e.Labels = map[string]string{"node": big}
	})))
	if !errors.Is(err, ErrRuntimeBatchTooLarge) {
		t.Fatalf("err = %v, want ErrRuntimeBatchTooLarge", err)
	}
}

func TestAdapterRejectsLabelOverflow(t *testing.T) {
	a := NewAdapter(AdapterOptions{})
	labels := make(map[string]string, MaxRuntimeLabelEntries+1)
	for i := 0; i <= MaxRuntimeLabelEntries; i++ {
		labels["k"+strings.Repeat("x", i)] = "v"
	}
	_, err := a.Map(testBatch(testEnvelope(KindProcessExec, func(e *RuntimeEventEnvelope) { e.Labels = labels })))
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("err = %v, want ErrInvalidEnvelope", err)
	}
}

func TestAdapterRedactsFilePath(t *testing.T) {
	got := mapOne(t, KindFileRead, func(e *RuntimeEventEnvelope) {
		e.File.PathRedacted = "/srv/AKIAIOSFODNN7EXAMPLE/key"
	})
	raw, _ := json.Marshal(got.InputRedacted)
	if strings.Contains(string(raw), "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("redactor leaked AKIA token: %s", raw)
	}
}

func TestAdapterRedactsNetworkHost(t *testing.T) {
	got := mapOne(t, KindNetworkConnect, func(e *RuntimeEventEnvelope) {
		e.Network.HostRedacted = "AKIAIOSFODNN7EXAMPLE.evil.example.com"
	})
	raw, _ := json.Marshal(got.InputRedacted)
	if strings.Contains(string(raw), "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("redactor leaked AKIA token in host: %s", raw)
	}
}

func TestAdapterRedactsDNSQname(t *testing.T) {
	got := mapOne(t, KindDNSQuery, func(e *RuntimeEventEnvelope) {
		e.DNS.QNameRedacted = "sk-AKIAIOSFODNN7EXAMPLE.example.com"
	})
	raw, _ := json.Marshal(got.InputRedacted)
	if strings.Contains(string(raw), "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("redactor leaked AKIA token: %s", raw)
	}
}

func TestAdapterMapsStatusFailed(t *testing.T) {
	got := mapOne(t, KindProcessExec, func(e *RuntimeEventEnvelope) { e.OutcomeStatus = "failed" })
	if got.Status != edgecore.ActionStatusFailed {
		t.Fatalf("Status = %q, want %q", got.Status, edgecore.ActionStatusFailed)
	}
}

func TestAdapterMapsStatusDegraded(t *testing.T) {
	got := mapOne(t, KindProcessExec, func(e *RuntimeEventEnvelope) { e.OutcomeStatus = "degraded" })
	if got.Status != edgecore.ActionStatusDegraded {
		t.Fatalf("Status = %q, want %q", got.Status, edgecore.ActionStatusDegraded)
	}
}

func TestAdapterRejectsUnknownOutcomeStatus(t *testing.T) {
	a := NewAdapter(AdapterOptions{})
	_, err := a.Map(testBatch(testEnvelope(KindProcessExec, func(e *RuntimeEventEnvelope) { e.OutcomeStatus = "bogus" })))
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("err = %v, want ErrInvalidEnvelope", err)
	}
}

func TestAdapterPreservesArtifactPointers(t *testing.T) {
	got := mapOne(t, KindFileWrite, func(e *RuntimeEventEnvelope) {
		// EventID stamped post-map; pre-fill with the deterministic format the
		// adapter would emit so artifact pointer validation can match.
		e.ArtifactPtrs = []edgecore.ArtifactPointer{{
			TenantID:       e.TenantID,
			SessionID:      e.SessionID,
			ExecutionID:    e.ExecutionID,
			ArtifactType:   edgecore.ArtifactTypeEvidenceBundle,
			RetentionClass: edgecore.RetentionClassStandard,
			RedactionLevel: edgecore.RedactionLevelStandard,
			SHA256:         "sha256:rt-artifact",
			URI:            "artifact://edge/rt-artifact",
			CreatedAt:      testObservedAt(),
		}}
	})
	if len(got.ArtifactPointers) != 1 {
		t.Fatalf("ArtifactPointers len = %d, want 1", len(got.ArtifactPointers))
	}
	if got.ArtifactPointers[0].EventID != got.EventID {
		t.Fatalf("artifact event_id = %q, want %q", got.ArtifactPointers[0].EventID, got.EventID)
	}
}

func TestAdapterRejectsArtifactPointerCrossTenant(t *testing.T) {
	a := NewAdapter(AdapterOptions{})
	_, err := a.Map(testBatch(testEnvelope(KindFileWrite, func(e *RuntimeEventEnvelope) {
		e.ArtifactPtrs = []edgecore.ArtifactPointer{{
			TenantID:       "different-tenant",
			SessionID:      e.SessionID,
			ExecutionID:    e.ExecutionID,
			ArtifactType:   edgecore.ArtifactTypeEvidenceBundle,
			RetentionClass: edgecore.RetentionClassStandard,
			RedactionLevel: edgecore.RedactionLevelStandard,
			SHA256:         "sha256:rt",
			URI:            "artifact://edge/rt",
			CreatedAt:      testObservedAt(),
		}}
	})))
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("err = %v, want ErrInvalidEnvelope", err)
	}
}

func TestAdapterDeterministicSampling(t *testing.T) {
	// With denominator > 1, sampling-out is observable. Run a small fleet of
	// envelopes through Map twice and assert each event lands in the same
	// accept/drop bucket across runs.
	a := NewAdapter(AdapterOptions{SampleNumerator: 1, SampleDenominator: 4})
	envelopes := make([]RuntimeEventEnvelope, 32)
	for i := range envelopes {
		envelopes[i] = testEnvelope(KindProcessExec, func(e *RuntimeEventEnvelope) {
			e.SourceEventID = "sample-" + strings.Repeat("z", i+1)
		})
	}
	res1, err := a.Map(testBatch(envelopes...))
	if err != nil {
		t.Fatalf("first Map: %v", err)
	}
	res2, err := a.Map(testBatch(envelopes...))
	if err != nil {
		t.Fatalf("second Map: %v", err)
	}
	if len(res1.Events) != len(res2.Events) {
		t.Fatalf("non-deterministic accept count: %d vs %d", len(res1.Events), len(res2.Events))
	}
	if len(res1.Dropped) != len(res2.Dropped) {
		t.Fatalf("non-deterministic drop count: %d vs %d", len(res1.Dropped), len(res2.Dropped))
	}
	if len(res1.Dropped) == 0 {
		t.Fatalf("expected some drops at 1/4 sampling on 32 envelopes (deterministic, may need to retune seed)")
	}
	for i := range res1.Dropped {
		if res1.Dropped[i].SourceEventID != res2.Dropped[i].SourceEventID {
			t.Fatalf("drop order non-deterministic: %q vs %q", res1.Dropped[i].SourceEventID, res2.Dropped[i].SourceEventID)
		}
		if res1.Dropped[i].Reason != DropReasonSampledOut {
			t.Fatalf("drop reason = %q, want %q", res1.Dropped[i].Reason, DropReasonSampledOut)
		}
	}
}

func TestDecodeBatchRejectsForbiddenTopLevelKey(t *testing.T) {
	body := []byte(`{
		"source":{"source_id":"tetragon-test"},
		"events":[{
			"tenant_id":"t","session_id":"s","execution_id":"e",
			"source_event_id":"se","observed_at":"2026-05-17T12:00:00Z",
			"kind":"runtime.process.exec",
			"argv":["curl","https://example.com"]
		}]
	}`)
	_, err := DecodeBatch(bytes.NewReader(body))
	if err == nil {
		t.Fatalf("expected error for argv field")
	}
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("err = %v, want ErrInvalidEnvelope", err)
	}
}

func TestDecodeBatchRejectsForbiddenKeysVariants(t *testing.T) {
	for _, forbidden := range []string{"args", "cmdline", "command_line", "env", "environment", "file_content", "file_contents", "packet", "payload", "body", "request_body", "response_body", "headers", "header", "cookie", "cookies", "secret", "secrets", "token", "tokens", "password", "passwords", "api_key", "apikey", "private_key", "dns_response", "response"} {
		t.Run(forbidden, func(t *testing.T) {
			body := []byte(`{
				"source":{"source_id":"tetragon-test"},
				"events":[{
					"tenant_id":"t","session_id":"s","execution_id":"e",
					"source_event_id":"se","observed_at":"2026-05-17T12:00:00Z",
					"kind":"runtime.process.exec",
					"` + forbidden + `":"x"
				}]
			}`)
			_, err := DecodeBatch(bytes.NewReader(body))
			if !errors.Is(err, ErrInvalidEnvelope) {
				t.Fatalf("forbidden %q: err = %v, want ErrInvalidEnvelope", forbidden, err)
			}
		})
	}
}

func TestDecodeBatchAcceptsCanonicalShape(t *testing.T) {
	body := []byte(`{
		"source":{"source_id":"tetragon-test"},
		"batch_id":"b1",
		"events":[{
			"tenant_id":"t","session_id":"s","execution_id":"e",
			"source_event_id":"se","observed_at":"2026-05-17T12:00:00Z",
			"kind":"runtime.process.exec",
			"process":{"executable_basename":"curl","argument_count":1}
		}]
	}`)
	batch, err := DecodeBatch(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("DecodeBatch: %v", err)
	}
	if batch.Source.ID != "tetragon-test" {
		t.Fatalf("source_id = %q", batch.Source.ID)
	}
	if len(batch.Events) != 1 || batch.Events[0].Kind != KindProcessExec {
		t.Fatalf("decoded batch = %#v", batch)
	}
}
