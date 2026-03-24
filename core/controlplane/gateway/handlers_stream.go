package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cordum/cordum/core/infra/bus"
	"github.com/cordum/cordum/core/infra/store"
	wf "github.com/cordum/cordum/core/workflow"
	"github.com/prometheus/client_golang/prometheus"
	capsdk "github.com/cordum/cordum/core/protocol/capsdk"
	pb "github.com/cordum/cordum/core/protocol/pb/v1"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var wsPacketsDroppedTotal = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "cordum_gateway_ws_packets_dropped_total",
	Help: "Total WebSocket bus packets dropped due to marshal failure",
})

var wsSlowClientEvictions = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "cordum_gateway_ws_slow_client_evictions_total",
	Help: "Total WebSocket clients evicted because their send buffer was full",
}, []string{"reason"})

func init() {
	prometheus.MustRegister(wsPacketsDroppedTotal)
	prometheus.MustRegister(wsSlowClientEvictions)
}

const (
	defaultWSClientBufSize = 256
	minWSClientBufSize     = 1
	maxWSClientBufSize     = 10000
)

// wsClientBufferSize reads CORDUM_WS_CLIENT_BUFFER_SIZE and clamps to [1, 10000].
func wsClientBufferSize() int {
	raw := strings.TrimSpace(os.Getenv("CORDUM_WS_CLIENT_BUFFER_SIZE"))
	if raw == "" {
		return defaultWSClientBufSize
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < minWSClientBufSize {
		slog.Warn("invalid CORDUM_WS_CLIENT_BUFFER_SIZE, using default",
			"value", raw, "default", defaultWSClientBufSize)
		return defaultWSClientBufSize
	}
	if v > maxWSClientBufSize {
		slog.Warn("CORDUM_WS_CLIENT_BUFFER_SIZE exceeds max, clamping",
			"requested", v, "max", maxWSClientBufSize)
		return maxWSClientBufSize
	}
	return v
}

// stopBusTaps shuts down the broadcast goroutine by closing eventsCh.
// It is safe to call multiple times.
func (s *server) stopBusTaps() {
	s.stopBusTapsOnce.Do(func() {
		s.eventsStopped.Store(true)
		if s.eventsCh != nil {
			close(s.eventsCh)
		}
	})
}

// wsRevalidateInterval controls how often long-lived WebSocket connections
// re-check the caller's API key. If the key has been revoked or rotated the
// connection is closed within this window.
const wsRevalidateInterval = 60 * time.Second

type wsClient struct {
	ch               chan wsEvent
	tenant           string
	allowCrossTenant bool
	jobID            string
	apiKey           string // stored for periodic revalidation, never logged
	closeOnce        sync.Once
}

// closeChannel closes the client's event channel exactly once.
// Safe to call from both the broadcast loop (eviction) and handler defer.
func (c *wsClient) closeChannel() {
	c.closeOnce.Do(func() { close(c.ch) })
}

type wsEvent struct {
	data   []byte
	tenant string
	jobID  string
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return isAllowedOrigin(r) },
}

// negotiateSubprotocol builds a response header that echoes back only the
// "cordum-api-key" subprotocol identifier — never the credential itself.
// Previous versions echoed the full dot-format token (cordum-api-key.<base64>),
// which leaked the API key in response headers visible to proxies and DevTools.
func negotiateSubprotocol(r *http.Request) http.Header {
	for _, p := range websocket.Subprotocols(r) {
		if strings.EqualFold(p, wsAuthSubprotocol) {
			// Comma-separated format: ["cordum-api-key", "<base64>"]
			// Echo back the bare identifier only.
			return http.Header{"Sec-Websocket-Protocol": {wsAuthSubprotocol}}
		}
		if strings.HasPrefix(strings.ToLower(p), strings.ToLower(wsAuthSubprotocol)+".") {
			// Legacy dot format: "cordum-api-key.<base64>"
			// Still accept for backward compat, but only echo the identifier.
			return http.Header{"Sec-Websocket-Protocol": {wsAuthSubprotocol}}
		}
	}
	return nil
}

// startWorkerExpiry launches a background goroutine that evicts stale entries
// from the workerSeen and workers maps. This prevents unbounded growth when
// workers disconnect without sending a final heartbeat.
func (s *server) startWorkerExpiry() {
	s.workerExpireStop = make(chan struct{})
	go func() {
		ticker := time.NewTicker(workerHeartbeatTTL / 2)
		defer ticker.Stop()
		for {
			select {
			case <-s.workerExpireStop:
				return
			case <-ticker.C:
				now := time.Now().UTC()
				cutoff := now.Add(-workerHeartbeatTTL)
				s.workerMu.Lock()
				for id, seen := range s.workerSeen {
					if seen.Before(cutoff) {
						delete(s.workerSeen, id)
						delete(s.workers, id)
					}
				}
				s.workerMu.Unlock()
			}
		}
	}()
}

// stopWorkerExpiry signals the expiry goroutine to stop. Safe to call multiple times.
func (s *server) stopWorkerExpiry() {
	s.workerExpireOnce.Do(func() {
		if s.workerExpireStop != nil {
			close(s.workerExpireStop)
		}
	})
}

// startBusTaps subscribes to heartbeats and system events once for the lifetime of the gateway.
func (s *server) startBusTaps() error {
	// Heartbeats -> worker registry snapshot
	if err := s.bus.Subscribe(capsdk.SubjectHeartbeat, "", func(p *pb.BusPacket) error {
		if hb := p.GetHeartbeat(); hb != nil {
			s.workerMu.Lock()
			s.workers[hb.WorkerId] = hb
			if s.workerSeen == nil {
				s.workerSeen = make(map[string]time.Time)
			}
			s.workerSeen[hb.WorkerId] = time.Now().UTC()
			s.workerMu.Unlock()
			// Also stream heartbeats to WS listeners (best effort).
			s.enqueueBusPacket(p)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("subscribe %s: %w", capsdk.SubjectHeartbeat, err)
	}

	// DLQ tap to persist entries. Uses queue group so only one gateway replica
	// writes to Redis per DLQ event (N replicas → 1 write instead of N).
	// WS forwarding is handled separately by the sys.job.> broadcast subscription.
	if s.dlqStore != nil {
		if err := s.bus.Subscribe(capsdk.SubjectDLQ, "cordum-gateway", func(p *pb.BusPacket) error {
			if jr := p.GetJobResult(); jr != nil {
				jobID := strings.TrimSpace(jr.JobId)
				topic := ""
				lastState := ""
				attempts := 0
				dlqCtx, dlqCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer dlqCancel()
				if s.jobStore != nil && jobID != "" {
					if t, err := s.jobStore.GetTopic(dlqCtx, jobID); err == nil {
						topic = t
					}
					if st, err := s.jobStore.GetState(dlqCtx, jobID); err == nil {
						lastState = string(st)
					}
					if a, err := s.jobStore.GetAttempts(dlqCtx, jobID); err == nil {
						attempts = a
					}
				}
				if err := s.dlqStore.Add(dlqCtx, store.DLQEntry{
					JobID:      jobID,
					Topic:      topic,
					Status:     jr.Status.String(),
					Reason:     jr.ErrorMessage,
					ReasonCode: strings.TrimSpace(jr.ErrorCode),
					LastState:  lastState,
					Attempts:   attempts,
					CreatedAt:  time.Now().UTC(),
				}); err != nil {
					slog.Error("dlq add failed", "job_id", jobID, "error", err)
				}

				// Best effort: ensure a result exists for failed-to-dispatch jobs so clients can inspect `res:<job_id>`.
				if s.memStore != nil && s.jobStore != nil && jobID != "" {
					resKey := store.MakeResultKey(jobID)
					resPtr := store.PointerForKey(resKey)
					body := map[string]any{
						"job_id":       jobID,
						"status":       jr.Status.String(),
						"error":        map[string]any{"message": jr.ErrorMessage},
						"processed_by": "cordum-scheduler",
						"completed_at": time.Now().UTC().Format(time.RFC3339),
					}
					if data, err := json.Marshal(body); err == nil {
						if err := s.memStore.PutResult(dlqCtx, resKey, data); err != nil {
							slog.Error("store result failed", "job_id", jobID, "error", err)
						}
					}
					if existing, err := s.jobStore.GetResultPtr(dlqCtx, jobID); err != nil || strings.TrimSpace(existing) == "" {
						if err := s.jobStore.SetResultPtr(dlqCtx, jobID, resPtr); err != nil {
							slog.Error("set result pointer failed", "job_id", jobID, "error", err)
						}
					}
				}
			}
			return nil
		}); err != nil {
			return fmt.Errorf("subscribe %s: %w", capsdk.SubjectDLQ, err)
		}
	}

	// Event taps -> broadcast channel
	for _, subj := range []string{"sys.job.>", "sys.audit.>"} {
		subject := subj
		if err := s.bus.Subscribe(subject, "", func(p *pb.BusPacket) error {
			// Always broadcast to WS clients first — duplicate broadcasts are
			// harmless (dashboard re-renders) and ensures visibility even on retry.
			s.enqueueBusPacket(p)
			if subject == "sys.job.>" {
				// Check if gateway is shutting down before starting
				// potentially long-running workflow result processing.
				select {
				case <-s.shutdownCh:
					return nil
				default:
				}
				handlerCtx, handlerCancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer handlerCancel()
				if err := s.handleWorkflowJobResult(handlerCtx, p.GetJobResult()); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			slog.Error("bus subscribe failed", "subject", subject, "error", err)
		}
	}

	// Broadcast loop to WS clients
	go func() {
		for {
			select {
			case evt, ok := <-s.eventsCh:
				if !ok {
					return
				}
				var slowClients []*websocket.Conn
				// Write lock covers detect-slow + delete to avoid TOCTOU race.
				s.clientsMu.Lock()
				for conn, client := range s.clients {
					if client == nil {
						continue
					}
					if !client.allowCrossTenant {
						if evt.tenant == "" || evt.tenant != client.tenant {
							continue
						}
					}
					if client.jobID != "" && evt.jobID != client.jobID {
						continue
					}
					select {
					case client.ch <- evt:
					default:
						slowClients = append(slowClients, conn)
					}
				}
				for _, conn := range slowClients {
					if client := s.clients[conn]; client != nil {
						client.closeChannel()
					}
					delete(s.clients, conn)
				}
				s.clientsMu.Unlock()
				if n := len(slowClients); n > 0 {
					wsSlowClientEvictions.WithLabelValues("buffer_full").Add(float64(n))
					slog.Warn("ws: evicted slow clients", "count", n)
				}
			case <-s.shutdownCh:
				return
			}
		}
	}()

	s.startWorkerExpiry()

	return nil
}

func (s *server) enqueueBusPacket(p *pb.BusPacket) {
	if s == nil || p == nil {
		return
	}

	// Filter quarantined/denied job results: strip sensitive content before broadcast.
	p = filterQuarantinedPacket(p)

	data, err := marshalBusPacketForWS(p)
	if err != nil {
		wsPacketsDroppedTotal.Inc()
		slog.Error(
			"websocket bus packet dropped after all marshal attempts failed",
			"packet_type", busPacketType(p),
			"trace_id", sanitizeUTF8ForLog(strings.TrimSpace(p.GetTraceId())),
			"sender_id", sanitizeUTF8ForLog(strings.TrimSpace(p.GetSenderId())),
			"error", err,
		)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	tenant, _ := s.tenantForBusPacket(ctx, p)
	cancel()
	jobID := jobIDForBusPacket(p)
	s.enqueueWSEvent(data, tenant, jobID)
}

func marshalBusPacketForWS(p *pb.BusPacket) ([]byte, error) {
	if p == nil {
		return nil, errors.New("bus packet required")
	}

	data, err := protojson.Marshal(p)
	if err == nil {
		return data, nil
	}

	packetType := busPacketType(p)
	traceID := sanitizeUTF8ForLog(strings.TrimSpace(p.GetTraceId()))
	slog.Error(
		"protojson marshal failed for websocket bus packet",
		"packet_type", packetType,
		"trace_id", traceID,
		"error", err,
	)

	data, sanitizedErr := marshalSanitizedBusPacketForWS(p)
	if sanitizedErr == nil {
		return data, nil
	}

	data, fallbackErr := json.Marshal(p)
	if fallbackErr == nil {
		slog.Error(
			"sanitized protojson fallback failed; using stdlib JSON fallback for websocket bus packet",
			"packet_type", packetType,
			"trace_id", traceID,
			"error", sanitizedErr,
		)
		return data, nil
	}

	slog.Error(
		"failed to marshal websocket bus packet fallback; dropping packet",
		"packet_type", packetType,
		"trace_id", traceID,
		"sanitize_error", sanitizedErr,
		"error", fallbackErr,
	)
	return nil, fallbackErr
}

func marshalSanitizedBusPacketForWS(p *pb.BusPacket) ([]byte, error) {
	cloned, ok := proto.Clone(p).(*pb.BusPacket)
	if !ok || cloned == nil {
		return nil, errors.New("failed to clone bus packet for websocket fallback")
	}
	sanitizeProtoStrings(cloned.ProtoReflect())
	return protojson.Marshal(cloned)
}

func sanitizeProtoStrings(msg protoreflect.Message) {
	if !msg.IsValid() {
		return
	}

	msg.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch {
		case fd.IsMap():
			sanitizeProtoMapField(msg, fd, v.Map())
		case fd.IsList():
			sanitizeProtoListField(fd, v.List())
		case fd.Kind() == protoreflect.MessageKind:
			sanitizeProtoStrings(v.Message())
		case fd.Kind() == protoreflect.StringKind:
			sanitized := sanitizeUTF8ForLog(v.String())
			if sanitized != v.String() {
				msg.Set(fd, protoreflect.ValueOfString(sanitized))
			}
		}
		return true
	})
}

func sanitizeProtoListField(fd protoreflect.FieldDescriptor, list protoreflect.List) {
	switch fd.Kind() {
	case protoreflect.StringKind:
		for i := 0; i < list.Len(); i++ {
			current := list.Get(i).String()
			sanitized := sanitizeUTF8ForLog(current)
			if sanitized != current {
				list.Set(i, protoreflect.ValueOfString(sanitized))
			}
		}
	case protoreflect.MessageKind:
		for i := 0; i < list.Len(); i++ {
			sanitizeProtoStrings(list.Get(i).Message())
		}
	}
}

func sanitizeProtoMapField(msg protoreflect.Message, fd protoreflect.FieldDescriptor, m protoreflect.Map) {
	type mapEntry struct {
		key   protoreflect.MapKey
		value protoreflect.Value
	}

	entries := make([]mapEntry, 0)
	changed := false

	m.Range(func(k protoreflect.MapKey, v protoreflect.Value) bool {
		entry := mapEntry{key: k, value: v}

		if fd.MapKey().Kind() == protoreflect.StringKind {
			sanitizedKey := sanitizeUTF8ForLog(k.String())
			if sanitizedKey != k.String() {
				entry.key = protoreflect.ValueOfString(sanitizedKey).MapKey()
				changed = true
			}
		}

		switch fd.MapValue().Kind() {
		case protoreflect.StringKind:
			sanitizedValue := sanitizeUTF8ForLog(v.String())
			if sanitizedValue != v.String() {
				entry.value = protoreflect.ValueOfString(sanitizedValue)
				changed = true
			}
		case protoreflect.MessageKind:
			sanitizeProtoStrings(v.Message())
		}

		entries = append(entries, entry)
		return true
	})

	if !changed {
		return
	}

	msg.Clear(fd)
	dst := msg.Mutable(fd).Map()
	for _, entry := range entries {
		dst.Set(entry.key, entry.value)
	}
}

func sanitizeUTF8ForLog(value string) string {
	return strings.ToValidUTF8(value, "\uFFFD")
}

func busPacketType(p *pb.BusPacket) string {
	if p == nil {
		return "unknown"
	}
	switch p.GetPayload().(type) {
	case *pb.BusPacket_JobRequest:
		return "job_request"
	case *pb.BusPacket_JobResult:
		return "job_result"
	case *pb.BusPacket_Heartbeat:
		return "heartbeat"
	case *pb.BusPacket_Alert:
		return "alert"
	case *pb.BusPacket_JobProgress:
		return "job_progress"
	case *pb.BusPacket_JobCancel:
		return "job_cancel"
	default:
		return "unknown"
	}
}

// filterQuarantinedPacket strips result payloads from quarantined/denied job results
// so that sensitive content (secrets, PII) is not broadcast to WebSocket clients.
// Status and metadata are preserved so the dashboard can show the state transition.
func filterQuarantinedPacket(p *pb.BusPacket) *pb.BusPacket {
	jr := p.GetJobResult()
	if jr == nil {
		return p
	}
	if jr.Status != pb.JobStatus_JOB_STATUS_DENIED {
		return p
	}
	// Clone packet/proto messages to avoid copying embedded mutex fields.
	out, ok := proto.Clone(p).(*pb.BusPacket)
	if !ok || out == nil {
		return p
	}
	sanitized := out.GetJobResult()
	if sanitized == nil {
		return p
	}
	sanitized.ResultPtr = ""
	sanitized.ArtifactPtrs = nil
	return out
}

func (s *server) enqueueWSEvent(data []byte, tenant string, jobID string) {
	if s == nil || len(data) == 0 || s.eventsStopped.Load() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			_ = r // channel closed between flag check and send; safe to ignore
		}
	}()
	select {
	case s.eventsCh <- wsEvent{data: data, tenant: strings.TrimSpace(tenant), jobID: strings.TrimSpace(jobID)}:
	default:
	}
}

func (s *server) handleWorkflowJobResult(ctx context.Context, jr *pb.JobResult) error {
	if s == nil || s.workflowEng == nil || jr == nil || jr.JobId == "" {
		return nil
	}
	runID, stepID := splitWorkflowJobID(jr.JobId)
	if runID == "" {
		return nil
	}

	if s.jobStore != nil {
		lockKey := "cordum:wf:run:lock:" + runID
		// Spin-wait up to 3 seconds for the run lock. The reconciler or
		// cancel handler may hold it briefly. Giving up too quickly causes
		// the message to bounce through NATS redelivery which is slower.
		lockDeadline := time.Now().Add(3 * time.Second)
		var token string
		for {
			var err error
			lockCtx, lockCancel := context.WithTimeout(ctx, 2*time.Second)
			token, err = s.jobStore.TryAcquireLock(lockCtx, lockKey, 30*time.Second)
			lockCancel()
			if err != nil {
				slog.Warn("workflow result: lock acquire error",
					"run_id", runID, "step_id", stepID, "job_id", jr.JobId, "error", err)
				return bus.RetryAfter(err, 1*time.Second)
			}
			if token != "" {
				break // acquired
			}
			if time.Now().After(lockDeadline) {
				// Couldn't acquire after 3s — check if stale before NATS retry
				if s.isStaleJobResult(ctx, runID, stepID, jr.JobId) {
					return nil // ACK — proven stale
				}
				slog.Warn("workflow result: run lock contended, deferring to NATS retry",
					"run_id", runID, "step_id", stepID, "job_id", jr.JobId,
					"lock_key", lockKey)
				return bus.RetryAfter(fmt.Errorf("run lock busy: %s", runID), 500*time.Millisecond)
			}
			time.Sleep(100 * time.Millisecond)
		}
		defer func() {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if rErr := s.jobStore.ReleaseLock(releaseCtx, lockKey, token); rErr != nil {
				slog.Warn("workflow result: lock release failed, will expire via TTL",
					"run_id", runID, "lock_key", lockKey, "error", rErr)
			}
		}()
	}

	if err := s.workflowEng.HandleJobResult(ctx, jr); err != nil {
		if errors.Is(err, wf.ErrRunNotFound) {
			slog.Info("discarding job result for deleted/missing run",
				"component", "gateway", "run_id", runID,
				"step_id", stepID, "job_id", jr.JobId)
			return nil // ACK — run is gone, retrying won't help
		}
		return bus.RetryAfter(err, 1*time.Second)
	}
	return nil
}

// isStaleJobResult checks whether a job result message can be safely discarded
// because the target run is missing or in a terminal state, or the step is
// already terminal. This prevents orphan messages from creating infinite
// lock-contention retry storms against completed/deleted runs.
func (s *server) isStaleJobResult(ctx context.Context, runID, stepID, jobID string) bool {
	if s.workflowStore == nil {
		return false // can't check — keep retrying
	}
	run, err := s.workflowStore.GetRun(ctx, runID)
	if err != nil {
		// Run not found — it's stale. GetRun wraps redis.Nil, and the
		// engine wraps that as ErrRunNotFound. Check both.
		if errors.Is(err, redis.Nil) || errors.Is(err, wf.ErrRunNotFound) {
			slog.Info("discarding stale job result: run not found",
				"component", "gateway", "run_id", runID,
				"step_id", stepID, "job_id", jobID)
			return true
		}
		// Redis error — can't determine staleness, keep retrying
		return false
	}
	// Run is in a terminal state — no further results can affect it
	if wf.IsTerminalRunStatus(run.Status) {
		slog.Info("discarding stale job result: run is terminal",
			"component", "gateway", "run_id", runID,
			"step_id", stepID, "job_id", jobID,
			"run_status", string(run.Status))
		return true
	}
	// Check if the specific step is already terminal
	if stepID != "" && run.Steps != nil {
		if step, ok := run.Steps[stepID]; ok && step != nil {
			if step.Status == wf.StepStatusSucceeded || step.Status == wf.StepStatusFailed ||
				step.Status == wf.StepStatusCancelled || step.Status == wf.StepStatusTimedOut {
				slog.Info("discarding stale job result: step is terminal",
					"component", "gateway", "run_id", runID,
					"step_id", stepID, "job_id", jobID,
					"step_status", string(step.Status))
				return true
			}
		}
	}
	return false // run and step are still active — keep retrying
}

func splitWorkflowJobID(jobID string) (runID, stepID string) {
	parts := strings.SplitN(jobID, ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	runID = parts[0]
	stepID = parts[1]
	if at := strings.LastIndex(stepID, "@"); at > 0 {
		stepID = stepID[:at]
	}
	return runID, stepID
}

func (s *server) handleStream(w http.ResponseWriter, r *http.Request) {
	if s.auth != nil {
		if err := s.requireRole(r, "admin"); err != nil {
			writeForbidden(w, r, err)
			return
		}
	}

	slog.Info("ws connection attempt", "remote", r.RemoteAddr)
	ws, err := upgrader.Upgrade(w, r, negotiateSubprotocol(r))
	if err != nil {
		slog.Error("ws upgrade failed", "error", err)
		return
	}
	defer ws.Close()
	slog.Info("ws connected", "remote", r.RemoteAddr)

	authCtx := authFromRequest(r)
	client := &wsClient{ch: make(chan wsEvent, s.wsClientBufSz)}
	if authCtx != nil {
		client.tenant = strings.TrimSpace(authCtx.Tenant)
		client.allowCrossTenant = authCtx.AllowCrossTenant
		client.apiKey = authCtx.APIKey
	}
	s.clientsMu.Lock()
	s.clients[ws] = client
	s.clientsMu.Unlock()
	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, ws)
		s.clientsMu.Unlock()
		client.closeChannel()
	}()

	revalidate := time.NewTicker(wsRevalidateInterval)
	defer revalidate.Stop()

	for {
		select {
		case msg, ok := <-client.ch:
			if !ok {
				return
			}
			_ = ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
			if err := ws.WriteMessage(websocket.TextMessage, msg.data); err != nil {
				return
			}
		case <-revalidate.C:
			if s.auth != nil && client.apiKey != "" {
				if err := s.revalidateWSAuth(client.apiKey); err != nil {
					slog.Info("ws credential revoked, closing",
						"tenant", client.tenant, "remote", r.RemoteAddr)
					_ = ws.WriteControl(websocket.CloseMessage,
						websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "credential revoked"),
						time.Now().Add(2*time.Second))
					return
				}
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (s *server) handleJobStream(w http.ResponseWriter, r *http.Request) {
	if s.jobStore == nil {
		writeErrorJSON(w, http.StatusServiceUnavailable, "job store unavailable")
		return
	}
	jobID := strings.TrimSpace(r.PathValue("id"))
	if jobID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "job id required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	tenant, err := s.jobStore.GetTenant(ctx, jobID)
	cancel()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			writeErrorJSON(w, http.StatusNotFound, "job not found")
			return
		}
		writeErrorJSON(w, http.StatusInternalServerError, "failed to resolve job tenant")
		return
	}
	if err := s.requireTenantAccess(r, tenant); err != nil {
		writeForbidden(w, r, err)
		return
	}

	slog.Info("job ws connection attempt", "job_id", jobID, "remote", r.RemoteAddr)
	ws, err := upgrader.Upgrade(w, r, negotiateSubprotocol(r))
	if err != nil {
		slog.Error("job ws upgrade failed", "job_id", jobID, "error", err)
		return
	}
	defer ws.Close()
	slog.Info("job ws connected", "job_id", jobID, "remote", r.RemoteAddr)

	authCtx := authFromRequest(r)
	client := &wsClient{ch: make(chan wsEvent, s.wsClientBufSz), tenant: strings.TrimSpace(tenant), jobID: jobID}
	if authCtx != nil {
		client.apiKey = authCtx.APIKey
	}
	s.clientsMu.Lock()
	s.clients[ws] = client
	s.clientsMu.Unlock()
	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, ws)
		s.clientsMu.Unlock()
		client.closeChannel()
	}()

	revalidate := time.NewTicker(wsRevalidateInterval)
	defer revalidate.Stop()

	for {
		select {
		case msg, ok := <-client.ch:
			if !ok {
				return
			}
			_ = ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
			if err := ws.WriteMessage(websocket.TextMessage, msg.data); err != nil {
				return
			}
		case <-revalidate.C:
			if s.auth != nil && client.apiKey != "" {
				if err := s.revalidateWSAuth(client.apiKey); err != nil {
					slog.Info("job ws credential revoked, closing",
						"job_id", jobID, "tenant", client.tenant, "remote", r.RemoteAddr)
					_ = ws.WriteControl(websocket.CloseMessage,
						websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "credential revoked"),
						time.Now().Add(2*time.Second))
					return
				}
			}
		case <-r.Context().Done():
			return
		}
	}
}

// revalidateWSAuth builds a synthetic HTTP request carrying the given API key
// and runs it through the configured auth provider. Returns nil if the key is
// still valid, or an error if revoked / expired.
func (s *server) revalidateWSAuth(apiKey string) error {
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/stream", nil)
	req.Header.Set("X-API-Key", apiKey)
	_, err := s.auth.AuthenticateHTTP(req)
	return err
}
