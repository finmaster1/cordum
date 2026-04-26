//go:build eval
// +build eval

// llmchat_tool_eval.go — phase-11 eval harness. Walks
// tests/eval/cases/<category>/*.yaml, drives each case through a
// running cordum-llm-chat service, scores tool-call accuracy + arg
// validity + summary substring hits, writes per-case JSON +
// aggregate summary.
//
// Build tag `eval` keeps it out of the default `go test ./...`. The
// CI workflow at .github/workflows/llmchat-eval.yml is the only
// thing that runs it; locally a contributor invokes:
//
//	EVAL_VLLM_URL=http://127.0.0.1:8000/v1 \
//	EVAL_LLMCHAT_URL=https://127.0.0.1:8081 \
//	EVAL_API_KEY=$(cat .env | grep CORDUM_API_KEY | cut -d= -f2) \
//	go test -tags=eval -run TestLLMChatToolEval ./tests/eval/...
//
// The harness is deliberately read-only against the cordum stack —
// it issues POST /api/v1/chat one-shot requests and parses the
// returned chatPostResponse. No WS connections, no audit-chain
// mutation. The chat service's PreapprovedMutatingTools=[submit_job]
// determines which mutations actually execute; the eval scores the
// MODEL'S behavior, not the gateway's.
package eval

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	envVLLMURL    = "EVAL_VLLM_URL"
	envLLMChatURL = "EVAL_LLMCHAT_URL"
	envAPIKey     = "EVAL_API_KEY"
	envBaseline   = "EVAL_BASELINE"
	envResultsDir = "EVAL_RESULTS_DIR"
	envModelName  = "EVAL_MODEL_NAME"

	defaultLLMChatURL = "https://127.0.0.1:8081"
	defaultModelName  = "qwen3_coder_30b_fp8"

	perCaseTimeout = 60 * time.Second
)

// chatPostResponse mirrors core/llmchat.chatPostResponse. We redeclare
// the relevant fields here so the eval package doesn't depend on the
// internal core/llmchat package.
type chatPostResponse struct {
	SessionID string           `json:"session_id"`
	Assistant string           `json:"assistant"`
	ToolCalls []chatToolDetail `json:"tool_calls,omitempty"`
	Frames    []chatFrame      `json:"frames"`
}

type chatToolDetail struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type chatFrame struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ToolCall *chatToolDetail `json:"tool_call,omitempty"`
}

type chatPostRequest struct {
	SessionID string `json:"session_id,omitempty"`
	Message   string `json:"message"`
}

// TestLLMChatToolEval is the eval-tag entrypoint. Skips when
// EVAL_VLLM_URL / EVAL_LLMCHAT_URL / EVAL_API_KEY are unset (matches
// the demo-mock-bank skip-if-not-CORDUM_INTEGRATION convention).
func TestLLMChatToolEval(t *testing.T) {
	llmChatURL := os.Getenv(envLLMChatURL)
	if llmChatURL == "" {
		llmChatURL = defaultLLMChatURL
	}
	apiKey := os.Getenv(envAPIKey)
	if apiKey == "" {
		t.Skipf("%s required to drive POST /api/v1/chat; skipping", envAPIKey)
	}
	vllmURL := os.Getenv(envVLLMURL)
	if vllmURL == "" {
		t.Skipf("%s required to identify the active model; skipping", envVLLMURL)
	}

	model := os.Getenv(envModelName)
	if model == "" {
		model = defaultModelName
	}

	cases, err := LoadAllCases("cases")
	if err != nil {
		t.Fatalf("load cases: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("no cases under tests/eval/cases/; check working dir")
	}

	startedAt := time.Now().UTC()
	runID := startedAt.Format("20060102T150405Z")

	resultsRoot := os.Getenv(envResultsDir)
	if resultsRoot == "" {
		resultsRoot = filepath.Join("results", runID)
	}
	if err := os.MkdirAll(filepath.Join(resultsRoot, "cases"), 0o755); err != nil {
		t.Fatalf("mkdir results: %v", err)
	}

	client := &http.Client{
		Timeout: perCaseTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // demo cert
		},
	}

	summary := &EvalSummary{
		RunID:          runID,
		ModelName:      model,
		VLLMURL:        vllmURL,
		StartedAt:      startedAt.Format(time.RFC3339),
		TotalCases:     len(cases),
		CategoryScores: map[string]float64{},
		Cases:          make([]CaseResult, 0, len(cases)),
	}

	categoryHits := map[string]int{}
	categoryTotals := map[string]int{}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			result := runCase(t, client, llmChatURL, apiKey, c)
			perCasePath := filepath.Join(resultsRoot, "cases", c.Name+".json")
			if err := writeJSON(perCasePath, result); err != nil {
				t.Errorf("write per-case json: %v", err)
			}
			summary.Cases = append(summary.Cases, result)
			categoryTotals[c.Category]++
			if result.Pass {
				categoryHits[c.Category]++
				summary.PassedCases++
			}
		})
	}

	for cat, total := range categoryTotals {
		summary.CategoryScores[cat] = float64(categoryHits[cat]) / float64(total)
	}
	summary.FinishedAt = time.Now().UTC().Format(time.RFC3339)

	summaryPath := filepath.Join(resultsRoot, "summary.json")
	if err := writeJSON(summaryPath, summary); err != nil {
		t.Fatalf("write summary: %v", err)
	}
	t.Logf("eval summary written to %s (passed %d/%d)", summaryPath, summary.PassedCases, summary.TotalCases)

	// Optional baseline comparison + diff.md.
	if baselinePath := os.Getenv(envBaseline); baselinePath != "" {
		baseline, err := LoadSummary(baselinePath)
		if err != nil {
			t.Errorf("load baseline %s: %v", baselinePath, err)
		} else if baseline != nil {
			report, severe := CompareSummaries(baseline, summary, DefaultDeltaThreshold)
			diffPath := filepath.Join(resultsRoot, "diff.md")
			if err := os.WriteFile(diffPath, []byte(report), 0o644); err != nil {
				t.Errorf("write diff: %v", err)
			}
			t.Logf("baseline diff at %s (severe regressions: %d)", diffPath, severe)
			if severe > 0 {
				t.Errorf("baseline regression: %d cases degraded by more than %.0f%% — see %s",
					severe, DefaultDeltaThreshold*100, diffPath)
			}
		}
	}
}

// runCase issues the chat request, parses the response, scores it,
// and returns a CaseResult.
func runCase(t *testing.T, client *http.Client, base, apiKey string, c Case) CaseResult {
	t.Helper()

	result := CaseResult{
		Name:                    c.Name,
		Category:                c.Category,
		SummaryContainsExpected: len(c.ExpectedSummaryContains),
		OrderedMatchExpected:    c.ExpectedToolCallsOrdered,
	}

	ctx, cancel := context.WithTimeout(context.Background(), perCaseTimeout)
	defer cancel()

	sessionID, allFrames, assistantText, err := drivedialog(ctx, client, base, apiKey, c)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result
	}
	_ = sessionID

	// Extract actual tool calls in order from frames.
	var actual []ActualToolCall
	for _, f := range allFrames {
		if f.Type == "tool_call" && f.ToolCall != nil {
			actual = append(actual, ActualToolCall{
				Name: f.ToolCall.Name,
				Args: f.ToolCall.Arguments,
			})
			result.ActualToolCalls = append(result.ActualToolCalls, f.ToolCall.Name)
		}
	}

	// Budget assertion.
	if len(actual) > c.MaxToolCalls {
		result.BudgetExceeded = true
		result.Errors = append(result.Errors,
			fmt.Sprintf("budget exceeded: %d tool calls vs max %d", len(actual), c.MaxToolCalls))
	}

	// Forbidden violations.
	forbidSet := stringSet(c.ForbiddenToolCalls)
	for _, ac := range actual {
		if _, hit := forbidSet[ac.Name]; hit {
			result.ForbiddenViolations++
			result.Errors = append(result.Errors,
				fmt.Sprintf("forbidden tool fired: %s", ac.Name))
		}
	}

	// Tool-call accuracy: set-equality on tool names.
	expectedNames := stringSet(toolNames(c.ExpectedToolCalls))
	actualNames := stringSet(toolNames2(actual))
	if len(c.ExpectedToolCalls) == 0 && len(actual) == 0 {
		result.ToolCallAccuracy = 1
	} else if len(c.ExpectedToolCalls) == 0 {
		result.ToolCallAccuracy = 0
	} else {
		matched := 0
		for k := range expectedNames {
			if _, ok := actualNames[k]; ok {
				matched++
			}
		}
		result.ToolCallAccuracy = float64(matched) / float64(len(expectedNames))
	}

	// Ordered match (opt-in via expected_tool_calls_ordered: true).
	if c.ExpectedToolCallsOrdered {
		ordered := orderedMatch(c.ExpectedToolCalls, actual)
		result.OrderedMatchPassed = ordered
		if !ordered {
			result.Errors = append(result.Errors, "expected_tool_calls_ordered: actual order does not match")
		}
	}

	// Arg validity per expected tool call.
	if len(c.ExpectedToolCalls) > 0 {
		matchHits := 0
		for _, exp := range c.ExpectedToolCalls {
			if exp.Args == nil {
				matchHits++
				continue
			}
			for _, ac := range actual {
				if ac.Name != exp.ToolName {
					continue
				}
				if err := MatchArgs(exp.Args, ac.Args); err == nil {
					matchHits++
					break
				}
			}
		}
		result.ArgValidity = float64(matchHits) / float64(len(c.ExpectedToolCalls))
	} else {
		result.ArgValidity = 1
	}

	// Summary substring hits.
	lowered := strings.ToLower(assistantText)
	for _, sub := range c.ExpectedSummaryContains {
		if strings.Contains(lowered, strings.ToLower(sub)) {
			result.SummaryContainsHits++
		}
	}

	// Pass = (no forbidden + within budget + tool accuracy 100% + arg validity 100% + summary all hit).
	allSummaryHit := result.SummaryContainsExpected == 0 || result.SummaryContainsHits == result.SummaryContainsExpected
	result.Pass = result.ForbiddenViolations == 0 &&
		!result.BudgetExceeded &&
		result.ToolCallAccuracy >= 1.0 &&
		result.ArgValidity >= 1.0 &&
		allSummaryHit
	if c.ExpectedToolCallsOrdered && !result.OrderedMatchPassed {
		result.Pass = false
	}

	return result
}

// drivedialog issues the request series. For single-message cases it
// POSTs once. For Turns-based cases it replays user turns sequentially
// against the same session id so the LLM sees the conversation state.
func drivedialog(ctx context.Context, client *http.Client, base, apiKey string, c Case) (string, []chatFrame, string, error) {
	endpoint := strings.TrimRight(base, "/") + "/api/v1/chat"

	post := func(sessionID, message string) (chatPostResponse, error) {
		body, _ := json.Marshal(chatPostRequest{SessionID: sessionID, Message: message})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return chatPostResponse{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", apiKey)
		resp, err := client.Do(req)
		if err != nil {
			return chatPostResponse{}, err
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return chatPostResponse{}, fmt.Errorf("status=%d body=%s", resp.StatusCode, raw)
		}
		var out chatPostResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			return chatPostResponse{}, fmt.Errorf("decode: %w body=%s", err, raw)
		}
		return out, nil
	}

	if len(c.Turns) == 0 {
		got, err := post("", c.UserMessage)
		if err != nil {
			return "", nil, "", err
		}
		return got.SessionID, got.Frames, got.Assistant, nil
	}

	var sessionID string
	var lastFrames []chatFrame
	var lastAssistant string
	for _, t := range c.Turns {
		if t.Role != "user" {
			continue
		}
		got, err := post(sessionID, t.Content)
		if err != nil {
			return "", nil, "", err
		}
		sessionID = got.SessionID
		lastFrames = got.Frames
		lastAssistant = got.Assistant
	}
	return sessionID, lastFrames, lastAssistant, nil
}

func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func toolNames(in []ExpectedToolCall) []string {
	out := make([]string, len(in))
	for i, e := range in {
		out[i] = e.ToolName
	}
	return out
}

func toolNames2(in []ActualToolCall) []string {
	out := make([]string, len(in))
	for i, a := range in {
		out[i] = a.Name
	}
	return out
}

func stringSet(in []string) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for _, s := range in {
		out[s] = struct{}{}
	}
	return out
}

func orderedMatch(expected []ExpectedToolCall, actual []ActualToolCall) bool {
	if len(expected) != len(actual) {
		return false
	}
	for i := range expected {
		if expected[i].ToolName != actual[i].Name {
			return false
		}
	}
	return true
}
