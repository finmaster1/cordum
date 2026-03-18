package workflow

import "errors"

// ErrRunNotFound is returned when a workflow run does not exist in the store.
// Callers should treat this as permanent — retrying will not help. Used to
// detect orphaned job results for deleted runs.
var ErrRunNotFound = errors.New("workflow run not found")
