// Package brokenimport is a TEMPORARY validation fixture for task-5411c0b9.
// It deliberately introduces a compile-error that go mod tidy CANNOT detect
// (the import path is valid; only the symbol reference is wrong) so that
// the failure surfaces at the new `Repo-root build` Lint step rather than
// at the earlier `go mod tidy` step. This file will be removed by an
// immediate `git revert --no-edit` once CI captures the expected failure.
//
// DO NOT KEEP THIS FILE. If you see it on a non-validation commit, file
// a Moe bug.
package brokenimport

import "fmt"

// nonexistentSymbol is intentionally referenced but never defined; this
// makes `go build` and `go vet` fail with "undefined: nonexistentSymbol".
var _ = fmt.Sprint(nonexistentSymbol)
