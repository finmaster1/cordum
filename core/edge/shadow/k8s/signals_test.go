package k8s_test

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/cordum/cordum/core/edge/shadow"
	"github.com/cordum/cordum/core/edge/shadow/k8s"
)

func TestK8sDetector_Signals_HeartbeatMissing(t *testing.T) {
	// Pod whose image matches a known agent but the cordum heartbeat
	// label is absent. §14: must take N=3 consecutive polls before
	// promotion — assert that signal fires on poll 3, not earlier.
	pod := podWith("agent-pod", "default", "anthropic/claude-code:v1",
		map[string]string{testTenantLabel: testTenantA},
		nil)
	ns := nsWith("default", map[string]string{testTenantLabel: testTenantA})
	f := newFixture(t, k8s.Config{HeartbeatMissedThreshold: 3}, pod, ns)

	for i := 1; i <= 2; i++ {
		if err := f.detector.Scan(context.Background()); err != nil {
			t.Fatalf("scan #%d: %v", i, err)
		}
		for _, fnd := range f.listAll(t, testTenantA) {
			if fnd.EvidenceType == "k8s_heartbeat_missing" {
				t.Fatalf("heartbeat_missing fired on scan #%d; want only on #3", i)
			}
		}
	}
	if err := f.detector.Scan(context.Background()); err != nil {
		t.Fatalf("scan #3: %v", err)
	}
	var got *shadow.ShadowAgentFinding
	for _, fnd := range f.listAll(t, testTenantA) {
		fnd := fnd
		if fnd.EvidenceType == "k8s_heartbeat_missing" {
			got = &fnd
			break
		}
	}
	if got == nil {
		t.Fatalf("heartbeat_missing finding did not fire on scan #3")
	}
	if got.Risk != shadow.FindingRiskMedium {
		t.Errorf("Risk = %q, want medium", got.Risk)
	}
	if !containsSignal(got.SignalSet, "heartbeat_missing") {
		t.Errorf("SignalSet = %v, want contains heartbeat_missing", got.SignalSet)
	}
}

func TestK8sDetector_Signals_UnmanagedProcess(t *testing.T) {
	pod := podWith("rogue", "experiments", "ubuntu:22.04",
		map[string]string{testTenantLabel: testTenantA}, nil)
	pod.Spec.Containers[0].Command = []string{"claude", "--prompt", "secret-prompt"}
	ns := nsWith("experiments", map[string]string{testTenantLabel: testTenantA})
	f := newFixture(t, k8s.Config{}, pod, ns)

	if err := f.detector.Scan(context.Background()); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	got := findByType(t, f, testTenantA, "k8s_unmanaged_process")
	if got.AgentProduct != "claude-code" && got.AgentProduct != "claude" {
		t.Errorf("AgentProduct = %q, want resolved from leading token 'claude'", got.AgentProduct)
	}
	// §5.2 MUST NOT: --prompt value must NEVER appear in evidence.
	if got.EvidenceSummary != "" && containsAny(got.EvidenceSummary, []string{"secret-prompt"}) != "" {
		t.Errorf("EvidenceSummary leaked --prompt value: %q", got.EvidenceSummary)
	}
}

func TestK8sDetector_Signals_UnmanagedMCPService(t *testing.T) {
	svc := mcpSvc("mcp-rogue", "experiments", "mcp-sse", nil) // missing gateway label
	ns := nsWith("experiments", map[string]string{testTenantLabel: testTenantA})
	f := newFixture(t, k8s.Config{}, svc, ns)

	if err := f.detector.Scan(context.Background()); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	got := findByType(t, f, testTenantA, "k8s_unmanaged_mcp_service")
	if got.WorkloadKind != "Service" {
		t.Errorf("WorkloadKind = %q, want Service", got.WorkloadKind)
	}
	if got.WorkloadName != "mcp-rogue" {
		t.Errorf("WorkloadName = %q, want mcp-rogue", got.WorkloadName)
	}
}

func TestK8sDetector_Signals_UnmanagedWorkload(t *testing.T) {
	// Pod owned by Deployment "rogue-deploy" which is NOT on the
	// operator's WorkloadAllowlist; pod image matches an agent.
	owner := metav1.OwnerReference{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Name:       "rogue-deploy",
		UID:        "owner-uid-1",
	}
	pod := podWith("rogue-deploy-1234", "agents", "anthropic/claude-code:v2",
		map[string]string{testTenantLabel: testTenantA}, nil)
	pod.OwnerReferences = []metav1.OwnerReference{owner}
	ns := nsWith("agents", map[string]string{testTenantLabel: testTenantA})
	f := newFixture(t, k8s.Config{WorkloadAllowlist: []string{"approved-deploy"}}, pod, ns)

	if err := f.detector.Scan(context.Background()); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	got := findByType(t, f, testTenantA, "k8s_unmanaged_workload")
	if got.WorkloadKind != "Deployment" {
		t.Errorf("WorkloadKind = %q, want Deployment (owner kind)", got.WorkloadKind)
	}
	if got.WorkloadName != "rogue-deploy" {
		t.Errorf("WorkloadName = %q, want owner name rogue-deploy", got.WorkloadName)
	}
}

func TestK8sDetector_Signals_UntrustedAgentImage(t *testing.T) {
	pod := podWith("agent-1", "agents", "evil.example.com/claude-agent:latest",
		map[string]string{testTenantLabel: testTenantA}, nil)
	ns := nsWith("agents", map[string]string{testTenantLabel: testTenantA})
	f := newFixture(t, k8s.Config{}, pod, ns)

	if err := f.detector.Scan(context.Background()); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	got := findByType(t, f, testTenantA, "k8s_untrusted_agent_image")
	if got.Risk != shadow.FindingRiskLow {
		t.Errorf("Risk = %q, want low (per §7.1 default)", got.Risk)
	}
}

func TestK8sDetector_Signals_NamespaceUntenanted(t *testing.T) {
	// Namespace missing tenant label AND contains a shadow indicator pod.
	ns := nsWith("unowned", nil) // no tenant label
	pod := podWith("p1", "unowned", "anthropic/claude-code:v1", nil, nil)
	f := newFixture(t, k8s.Config{}, ns, pod)

	if err := f.detector.Scan(context.Background()); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	// Untenanted namespace findings route to the quarantine tenant
	// (no tenant label exists to map them anywhere else).
	got := findByType(t, f, testQuarantineTen, "k8s_namespace_untenanted")
	if got.TenantSource != "quarantine" {
		t.Errorf("TenantSource = %q, want quarantine", got.TenantSource)
	}
}

func TestK8sDetector_Signals_AdmissionObserved(t *testing.T) {
	// Observe-mode: detector never installs a webhook. The signal is
	// "observed only if the operator's existing admission log was fed in
	// via config." With zero admission log entries, signal must NOT fire.
	f := newFixture(t, k8s.Config{})
	if err := f.detector.Scan(context.Background()); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, fnd := range f.listAll(t, testTenantA) {
		if fnd.EvidenceType == "k8s_admission_observed" {
			t.Fatalf("admission_observed fired without admission log input: %+v", fnd)
		}
	}
}

func TestK8sDetector_Signals_EgressBypass(t *testing.T) {
	// NetworkPolicy explicitly allows egress to api.anthropic.com from
	// a pod whose identity is NOT on the LLM proxy allowlist.
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: "allow-anthropic", Namespace: "agents",
			Labels: map[string]string{testTenantLabel: testTenantA},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "rogue"}},
			Egress: []networkingv1.NetworkPolicyEgressRule{{
				To: []networkingv1.NetworkPolicyPeer{{
					IPBlock: &networkingv1.IPBlock{CIDR: "0.0.0.0/0"},
				}},
				Ports: []networkingv1.NetworkPolicyPort{{
					Port: ptrIntOrString(443),
				}},
			}},
		},
	}
	ns := nsWith("agents", map[string]string{testTenantLabel: testTenantA})
	f := newFixture(t, k8s.Config{LLMProxyEndpoints: []string{"llm-proxy.cordum.svc"}}, np, ns)

	if err := f.detector.Scan(context.Background()); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	got := findByType(t, f, testTenantA, "k8s_egress_bypass")
	if got.Risk != shadow.FindingRiskHigh {
		t.Errorf("Risk = %q, want high (per §7.1 egress_bypass default)", got.Risk)
	}
}

// --- tenant + principal mapping precedence (§6.1 / §6.2) ---

func TestK8sDetector_TenantMapping_Precedence(t *testing.T) {
	t.Run("pod_label_tier1", func(t *testing.T) {
		pod := podWith("p", "ns1", "evil.example.com/claude:latest",
			map[string]string{testTenantLabel: "tenant-pod"}, nil)
		ns := nsWith("ns1", map[string]string{testTenantLabel: "tenant-ns"})
		f := newFixture(t, k8s.Config{}, pod, ns)
		_ = f.detector.Scan(context.Background())
		got := f.listAll(t, "tenant-pod")
		if len(got) == 0 || got[0].TenantSource != "pod_label" {
			t.Fatalf("tier1 resolution failed: findings=%d source=%q",
				len(got), tenantSourceOf(got))
		}
	})
	t.Run("namespace_label_tier2", func(t *testing.T) {
		pod := podWith("p", "ns2", "evil.example.com/claude:latest", nil, nil)
		ns := nsWith("ns2", map[string]string{testTenantLabel: "tenant-ns"})
		f := newFixture(t, k8s.Config{}, pod, ns)
		_ = f.detector.Scan(context.Background())
		got := f.listAll(t, "tenant-ns")
		if len(got) == 0 || got[0].TenantSource != "namespace_label" {
			t.Fatalf("tier2 resolution failed: findings=%d source=%q",
				len(got), tenantSourceOf(got))
		}
	})
	t.Run("cluster_config_tier3", func(t *testing.T) {
		pod := podWith("p", "ns3", "evil.example.com/claude:latest", nil, nil)
		ns := nsWith("ns3", nil)
		f := newFixture(t, k8s.Config{
			ClusterTenantMap: map[string]string{testClusterID: "tenant-cluster"},
		}, pod, ns)
		_ = f.detector.Scan(context.Background())
		got := f.listAll(t, "tenant-cluster")
		if len(got) == 0 || got[0].TenantSource != "cluster_config" {
			t.Fatalf("tier3 resolution failed: findings=%d source=%q",
				len(got), tenantSourceOf(got))
		}
	})
	t.Run("sa_config_tier4", func(t *testing.T) {
		pod := podWith("p", "ns4", "evil.example.com/claude:latest", nil, nil)
		pod.Spec.ServiceAccountName = "sa-with-tenant"
		ns := nsWith("ns4", nil)
		sa := saWith("sa-with-tenant", "ns4",
			map[string]string{testTenantLabel: "tenant-sa"})
		f := newFixture(t, k8s.Config{}, pod, ns, sa)
		_ = f.detector.Scan(context.Background())
		got := f.listAll(t, "tenant-sa")
		if len(got) == 0 || got[0].TenantSource != "sa_config" {
			t.Fatalf("tier4 resolution failed: findings=%d source=%q",
				len(got), tenantSourceOf(got))
		}
	})
	t.Run("quarantine_tier5", func(t *testing.T) {
		pod := podWith("p", "ns5", "evil.example.com/claude:latest", nil, nil)
		ns := nsWith("ns5", nil)
		f := newFixture(t, k8s.Config{}, pod, ns)
		_ = f.detector.Scan(context.Background())
		got := f.listAll(t, testQuarantineTen)
		if len(got) == 0 || got[0].TenantSource != "quarantine" {
			t.Fatalf("tier5 resolution failed: findings=%d source=%q",
				len(got), tenantSourceOf(got))
		}
	})
}

// --- data minimization: extraction-time canary defense ---

func TestK8sDetector_DataMinimization_NeverCapturesSecrets(t *testing.T) {
	canaries := []string{
		"sk-ant-LEAKEDCANARY1234567890",
		"sk-LEAKEDOPENAI1234567890",
		"OPENSSH PRIVATE KEY",
		"secret-prompt-content-CANARY",
		"ghp_LEAKEDGITHUBTOKEN1234",
	}
	pod := podWith("agent", "agents", "evil.example.com/claude-agent:v1",
		map[string]string{testTenantLabel: testTenantA}, nil)
	pod.Spec.Containers[0].Env = []corev1.EnvVar{
		{Name: "ANTHROPIC_API_KEY", Value: canaries[0]},
		{Name: "OPENAI_API_KEY", Value: canaries[1]},
		{Name: "SSH_KEY_BODY", Value: "-----BEGIN " + canaries[2] + "-----xxx"},
	}
	pod.Spec.Containers[0].Args = []string{"--prompt", canaries[3], "--token", canaries[4]}
	pod.Spec.Volumes = []corev1.Volume{{
		Name: "creds",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{SecretName: "agent-secrets"},
		},
	}}
	pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{
		Name: "creds", MountPath: "/secrets",
	}}
	ns := nsWith("agents", map[string]string{testTenantLabel: testTenantA})
	f := newFixture(t, k8s.Config{}, pod, ns)

	if err := f.detector.Scan(context.Background()); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, fnd := range f.listAll(t, testTenantA) {
		fields := []string{
			fnd.EvidenceSummary, fnd.RedactedPath, fnd.AgentID,
			fnd.WorkloadName, fnd.Namespace, fnd.Hostname,
			fnd.FalsePositiveReason, fnd.ExceptionID,
		}
		for k, v := range fnd.Metadata {
			fields = append(fields, k, v)
		}
		fields = append(fields, fnd.SignalSet...)
		for _, field := range fields {
			if leaked := containsAny(field, canaries); leaked != "" {
				t.Errorf("finding %q field leaked canary %q: %q",
					fnd.FindingID, leaked, field)
			}
		}
	}
}

// --- helpers ---

func findByType(t *testing.T, f *detectorFixture, tenant, evType string) shadow.ShadowAgentFinding {
	t.Helper()
	for _, fnd := range f.listAll(t, tenant) {
		if fnd.EvidenceType == evType {
			return fnd
		}
	}
	t.Fatalf("no finding with EvidenceType=%q under tenant=%q", evType, tenant)
	return shadow.ShadowAgentFinding{}
}

func containsSignal(set []string, want string) bool {
	for _, s := range set {
		if s == want {
			return true
		}
	}
	return false
}

func tenantSourceOf(in []shadow.ShadowAgentFinding) string {
	if len(in) == 0 {
		return "<no findings>"
	}
	return in[0].TenantSource
}

func ptrIntOrString(p int) *intstr.IntOrString {
	v := intstr.FromInt(p)
	return &v
}
