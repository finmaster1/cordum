package llmchat

import (
	"errors"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	ErrorKindVLLMCallFailed = "vllm_call_failed"
	ErrorKindRedisFailed    = "redis_failed"
	ErrorKindAuthRejected   = "auth_rejected"
	ErrorKindOther          = "other"
)

var allowedErrorKinds = map[string]struct{}{
	ErrorKindVLLMCallFailed: {},
	ErrorKindRedisFailed:    {},
	ErrorKindAuthRejected:   {},
	ErrorKindOther:          {},
}

// Metrics owns llm-chat's domain-level Prometheus instrumentation. Labels are
// intentionally tiny allowlists: free-form session IDs, principals, tenant IDs,
// tokens, prompts, and error messages must never become metric labels.
type Metrics struct {
	SessionsActive  prometheus.Gauge
	VLLMLatency     prometheus.Histogram
	TokenBudgetUsed prometheus.Counter
	Errors          *prometheus.CounterVec

	noop bool
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}

	metrics := &Metrics{
		SessionsActive: registerGauge(reg, prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "chat_sessions_active",
			Help: "Number of currently active llm-chat sessions.",
		})),
		VLLMLatency: registerHistogram(reg, prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "chat_vllm_latency_seconds",
			Help:    "Latency of the llm-chat provider call to the local OpenAI-compatible inference endpoint.",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60},
		})),
		TokenBudgetUsed: registerCounter(reg, prometheus.NewCounter(prometheus.CounterOpts{
			Name: "chat_token_budget_used_total",
			Help: "Total token-budget units consumed by llm-chat sessions.",
		})),
		Errors: registerCounterVec(reg, prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "chat_errors_total",
			Help: "Total llm-chat errors bucketed by bounded error kind.",
		}, []string{"kind"})),
	}

	metrics.prewarmLabelValues()
	return metrics
}

func NewNopMetrics() *Metrics {
	return &Metrics{noop: true}
}

func (m *Metrics) IncSessions() {
	if m == nil || m.noop || m.SessionsActive == nil {
		return
	}
	m.SessionsActive.Inc()
}

func (m *Metrics) DecSessions() {
	if m == nil || m.noop || m.SessionsActive == nil {
		return
	}
	m.SessionsActive.Dec()
}

func (m *Metrics) ObserveVLLMLatency(d time.Duration) {
	if m == nil || m.noop || m.VLLMLatency == nil {
		return
	}
	if d < 0 {
		d = 0
	}
	m.VLLMLatency.Observe(d.Seconds())
}

func (m *Metrics) IncTokenBudgetUsed(n float64) {
	if m == nil || m.noop || m.TokenBudgetUsed == nil || n <= 0 {
		return
	}
	m.TokenBudgetUsed.Add(n)
}

func (m *Metrics) IncError(kind string) {
	if m == nil || m.noop || m.Errors == nil {
		return
	}
	m.Errors.WithLabelValues(normalizeErrorKind(kind)).Inc()
}

func (m *Metrics) prewarmLabelValues() {
	if m.Errors != nil {
		for kind := range allowedErrorKinds {
			m.Errors.WithLabelValues(kind)
		}
	}
}

func normalizeErrorKind(kind string) string {
	kind = strings.TrimSpace(kind)
	if _, ok := allowedErrorKinds[kind]; ok {
		return kind
	}
	return ErrorKindOther
}

func registerGauge(reg prometheus.Registerer, gauge prometheus.Gauge) prometheus.Gauge {
	if err := reg.Register(gauge); err != nil {
		var already prometheus.AlreadyRegisteredError
		if errors.As(err, &already) {
			if existing, ok := already.ExistingCollector.(prometheus.Gauge); ok {
				return existing
			}
		}
		panic(err)
	}
	return gauge
}

func registerCounter(reg prometheus.Registerer, counter prometheus.Counter) prometheus.Counter {
	if err := reg.Register(counter); err != nil {
		var already prometheus.AlreadyRegisteredError
		if errors.As(err, &already) {
			if existing, ok := already.ExistingCollector.(prometheus.Counter); ok {
				return existing
			}
		}
		panic(err)
	}
	return counter
}

func registerCounterVec(reg prometheus.Registerer, counter *prometheus.CounterVec) *prometheus.CounterVec {
	if err := reg.Register(counter); err != nil {
		var already prometheus.AlreadyRegisteredError
		if errors.As(err, &already) {
			if existing, ok := already.ExistingCollector.(*prometheus.CounterVec); ok {
				return existing
			}
		}
		panic(err)
	}
	return counter
}

func registerHistogram(reg prometheus.Registerer, histogram prometheus.Histogram) prometheus.Histogram {
	if err := reg.Register(histogram); err != nil {
		var already prometheus.AlreadyRegisteredError
		if errors.As(err, &already) {
			if existing, ok := already.ExistingCollector.(prometheus.Histogram); ok {
				return existing
			}
		}
		panic(err)
	}
	return histogram
}
