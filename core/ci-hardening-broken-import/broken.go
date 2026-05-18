// Package brokenimport is a TEMPORARY validation fixture for task-5411c0b9.
// It deliberately imports a non-existent package so that the new
// `Repo-root build` and `Repo-root vet` steps in the Lint workflow fail
// the CI run, proving the gate works. This file will be removed by an
// immediate `git revert --no-edit` once the CI failure is captured.
//
// DO NOT KEEP THIS FILE. If you see it on a non-validation commit, file
// a Moe bug.
package brokenimport

import _ "github.com/cordum/cordum/core/nonexistent"
