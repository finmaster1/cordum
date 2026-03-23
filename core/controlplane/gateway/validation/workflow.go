package validation

import (
	"errors"
	"fmt"
	"regexp"
)

// MaxWorkflowStepIDLen is the maximum allowed length for a workflow step ID.
const MaxWorkflowStepIDLen = 64

// WorkflowStepIDPattern defines the valid characters for a workflow step ID.
var WorkflowStepIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// TruncateForError truncates s to max characters for safe inclusion in error
// messages. Prevents user-supplied input from inflating error message size.
func TruncateForError(s string, max int) string {
	if max <= 0 {
		max = 256
	}
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// WorkflowStepID validates a single workflow step ID.
func WorkflowStepID(stepID string) error {
	if stepID == "" {
		return errors.New("workflow step id required")
	}
	if len(stepID) > MaxWorkflowStepIDLen {
		return fmt.Errorf("workflow step id %q exceeds %d characters", TruncateForError(stepID, 256), MaxWorkflowStepIDLen)
	}
	if !WorkflowStepIDPattern.MatchString(stepID) {
		return fmt.Errorf("workflow step id %q must match %s", TruncateForError(stepID, 256), WorkflowStepIDPattern.String())
	}
	return nil
}

// WorkflowStepMap validates all keys in a step map as valid step IDs.
func WorkflowStepMap(steps map[string]any) error {
	for id := range steps {
		if err := WorkflowStepID(id); err != nil {
			return err
		}
	}
	return nil
}
