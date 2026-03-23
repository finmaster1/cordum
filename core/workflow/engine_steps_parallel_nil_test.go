package workflow

import (
	"testing"
	"time"
)

func TestSummarizeParallelChildrenNilMap(t *testing.T) {
	parent := &StepRun{Children: nil}
	childIDs := []string{"a", "b", "c"}
	s, f, r := summarizeParallelChildren(parent, childIDs)
	if s != 0 || f != 0 || r != 0 {
		t.Fatalf("expected (0,0,0) for nil Children, got (%d,%d,%d)", s, f, r)
	}
}

func TestSummarizeParallelChildrenNilParent(t *testing.T) {
	s, f, r := summarizeParallelChildren(nil, []string{"a"})
	if s != 0 || f != 0 || r != 0 {
		t.Fatalf("expected (0,0,0) for nil parent, got (%d,%d,%d)", s, f, r)
	}
}

func TestSummarizeParallelChildrenPartialMap(t *testing.T) {
	parent := &StepRun{
		Children: map[string]*StepRun{
			"a": {Status: StepStatusSucceeded},
			// "b" missing
		},
	}
	s, f, r := summarizeParallelChildren(parent, []string{"a", "b"})
	if s != 1 {
		t.Fatalf("expected 1 succeeded, got %d", s)
	}
	// missing child "b" is skipped (nil child -> continue)
	if f != 0 || r != 0 {
		t.Fatalf("expected (0,0) for missing child, got f=%d r=%d", f, r)
	}
}

func TestCancelParallelChildrenNilMap(t *testing.T) {
	parent := &StepRun{Children: nil}
	run := &WorkflowRun{Steps: nil}
	now := time.Now()

	// Should not panic and should initialize Children
	e := &Engine{}
	cancelled := e.cancelParallelChildren(parent, run, []string{"a"}, now)
	if cancelled != 0 {
		t.Fatalf("expected 0 cancelled for nil Children, got %d", cancelled)
	}
}

func TestCancelParallelChildrenEmptyMap(t *testing.T) {
	parent := &StepRun{Children: make(map[string]*StepRun)}
	run := &WorkflowRun{Steps: make(map[string]*StepRun)}
	now := time.Now()

	e := &Engine{}
	cancelled := e.cancelParallelChildren(parent, run, []string{"a", "b"}, now)
	if cancelled != 0 {
		t.Fatalf("expected 0 cancelled for empty map, got %d", cancelled)
	}
}

func TestAggregateParallelOutputsNilContext(t *testing.T) {
	run := &WorkflowRun{Context: nil}
	outputs := aggregateParallelOutputs(run, []string{"a"})
	if outputs == nil {
		t.Fatal("expected non-nil outputs map")
	}
	if len(outputs) != 0 {
		t.Fatalf("expected empty outputs, got %d", len(outputs))
	}
}

func TestAggregateParallelOutputsNilRun(t *testing.T) {
	outputs := aggregateParallelOutputs(nil, []string{"a"})
	if outputs == nil {
		t.Fatal("expected non-nil outputs map")
	}
}
