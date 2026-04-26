package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLLMChatEvalWorkflowRequiresEntitledExternalVLLM(t *testing.T) {
	t.Parallel()

	path := filepath.Join(repoRoot(t), ".github", "workflows", "llmchat-eval.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}

	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse workflow YAML: %v", err)
	}

	triggers, ok := mapAt(t, doc, "on")
	if !ok {
		t.Fatal("workflow missing on block")
	}
	if _, hasSchedule := triggers["schedule"]; hasSchedule {
		t.Fatal("llmchat-eval v1 workflow must not schedule GPU evals on ubuntu-latest")
	}

	workflowDispatch, ok := mapAt(t, triggers, "workflow_dispatch")
	if !ok {
		t.Fatal("workflow missing workflow_dispatch trigger")
	}
	inputs, ok := mapAt(t, workflowDispatch, "inputs")
	if !ok {
		t.Fatal("workflow_dispatch missing inputs")
	}
	vllmURL, ok := mapAt(t, inputs, "vllm_url")
	if !ok {
		t.Fatal("workflow_dispatch missing vllm_url input")
	}
	if required, _ := vllmURL["required"].(bool); !required {
		t.Fatal("vllm_url must be required so github-hosted runs use an external/staging vLLM")
	}
	if _, hasDefault := vllmURL["default"]; hasDefault {
		t.Fatal("vllm_url must not default to local 127.0.0.1 GPU vLLM on ubuntu-latest")
	}

	text := string(raw)
	requiredSnippets := []string{
		"CORDUM_LLMCHAT_EVAL_LICENSE_TOKEN",
		"CORDUM_LLMCHAT_EVAL_LICENSE_PUBLIC_KEY",
		"Smoke-test llm-chat entitlement gate",
		"feature_unavailable",
		"empty_message",
		"Local GPU vLLM unavailable",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(text, snippet) {
			t.Fatalf("workflow missing required snippet %q", snippet)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			t.Fatalf("could not locate repo root from %s", wd)
		}
		wd = parent
	}
}

func mapAt(t *testing.T, m map[string]any, key string) (map[string]any, bool) {
	t.Helper()
	raw, ok := m[key]
	if !ok {
		return nil, false
	}
	nested, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("%s is %T, want map[string]any", key, raw)
	}
	return nested, true
}
