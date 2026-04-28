package llmchat

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestNewMetrics_ExportsRequiredFamilies(t *testing.T) {
	reg := prometheus.NewRegistry()
	_ = NewMetrics(reg)

	families := gatherMetricFamilies(t, reg)
	required := []string{
		"chat_sessions_active",
		"chat_vllm_latency_seconds",
		"chat_token_budget_used_total",
		"chat_errors_total",
	}
	for _, name := range required {
		if _, ok := families[name]; !ok {
			t.Fatalf("missing metric family %q; got %v", name, metricFamilyNames(families))
		}
	}
}

func TestNewMetrics_LabelNamesBoundedAndSafe(t *testing.T) {
	reg := prometheus.NewRegistry()
	_ = NewMetrics(reg)

	forbiddenLabels := map[string]bool{
		"session_id": true,
		"principal":  true,
		"tenant":     true,
		"token":      true,
		"prompt":     true,
		"user_id":    true,
	}
	families := gatherMetricFamilies(t, reg)
	for _, family := range families {
		if len(family.GetMetric()) == 0 {
			t.Fatalf("%s has no samples; tests cannot verify label safety", family.GetName())
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if forbiddenLabels[label.GetName()] {
					t.Fatalf("%s exposes forbidden label name %q", family.GetName(), label.GetName())
				}
			}

			switch family.GetName() {
			case "chat_errors_total":
				assertLabelNames(t, family.GetName(), metric.GetLabel(), "kind")
			default:
				assertLabelNames(t, family.GetName(), metric.GetLabel())
			}
		}
	}
}

func TestMetrics_RejectsUnknownErrorKinds(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.IncError("weird")

	family := gatherMetricFamilies(t, reg)["chat_errors_total"]
	metric := findMetricByLabelValue(t, family, "kind", "other")
	if got := metric.GetCounter().GetValue(); got != 1 {
		t.Fatalf("other error counter = %v, want 1", got)
	}
}

func TestMetrics_GaugeIncDec(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.IncSessions()
	m.IncSessions()
	m.IncSessions()
	m.DecSessions()

	family := gatherMetricFamilies(t, reg)["chat_sessions_active"]
	if family == nil || len(family.GetMetric()) != 1 {
		t.Fatalf("chat_sessions_active samples = %d, want 1", len(family.GetMetric()))
	}
	if got := family.GetMetric()[0].GetGauge().GetValue(); got != 2 {
		t.Fatalf("chat_sessions_active = %v, want 2", got)
	}
}

func gatherMetricFamilies(t *testing.T, reg *prometheus.Registry) map[string]*dto.MetricFamily {
	t.Helper()

	gathered, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	families := make(map[string]*dto.MetricFamily, len(gathered))
	for _, family := range gathered {
		families[family.GetName()] = family
	}
	return families
}

func metricFamilyNames(families map[string]*dto.MetricFamily) []string {
	names := make([]string, 0, len(families))
	for name := range families {
		names = append(names, name)
	}
	return names
}

func assertLabelNames(t *testing.T, metricName string, labels []*dto.LabelPair, want ...string) {
	t.Helper()

	got := make([]string, 0, len(labels))
	for _, label := range labels {
		got = append(got, label.GetName())
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%s label names = %v, want %v", metricName, got, want)
	}
}

func findMetricByLabelValue(t *testing.T, family *dto.MetricFamily, labelName, labelValue string) *dto.Metric {
	t.Helper()

	if family == nil {
		t.Fatalf("metric family is nil; want label %s=%s", labelName, labelValue)
	}
	for _, metric := range family.GetMetric() {
		for _, label := range metric.GetLabel() {
			if label.GetName() == labelName && label.GetValue() == labelValue {
				return metric
			}
		}
	}
	t.Fatalf("%s has no metric with label %s=%s", family.GetName(), labelName, labelValue)
	return nil
}
