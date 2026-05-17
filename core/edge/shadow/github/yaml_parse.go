package github

import (
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// workflowSpec is the §8.1-relevant projection of a GitHub Actions
// workflow YAML. We parse only the structural fields the detector
// cares about (env keys, action `uses:` values, runner labels) so the
// data minimization contract holds even if someone smuggles secrets
// into adjacent unmodelled fields.
type workflowSpec struct {
	Env  map[string]yaml.Node       `yaml:"env"`
	Jobs map[string]workflowSpecJob `yaml:"jobs"`
}

type workflowSpecJob struct {
	Env    map[string]yaml.Node `yaml:"env"`
	RunsOn yaml.Node            `yaml:"runs-on"`
	Steps  []workflowSpecStep   `yaml:"steps"`
}

type workflowSpecStep struct {
	Uses string               `yaml:"uses"`
	Run  string               `yaml:"run"`
	Env  map[string]yaml.Node `yaml:"env"`
}

// parseWorkflowYAML is a tolerant unmarshal — malformed YAML returns
// nil so the rest of the scan keeps going. We deliberately do NOT
// surface parse errors to the detector loop because GitHub will
// happily store half-typed workflows that go-actions itself refuses,
// and the scanner shouldn't fan-out audit noise for that.
func parseWorkflowYAML(content string) *workflowSpec {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	spec := &workflowSpec{}
	if err := yaml.Unmarshal([]byte(content), spec); err != nil {
		return nil
	}
	return spec
}

// AllUses returns every step `uses:` value across every job in the
// workflow. Used by the missing_cordum_attach + agent_action_used
// extractors. Empty `uses:` (i.e. `run:`-only steps) are skipped.
func (s *workflowSpec) AllUses() []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, 4)
	for _, job := range s.Jobs {
		for _, step := range job.Steps {
			if u := strings.TrimSpace(step.Uses); u != "" {
				out = append(out, u)
			}
		}
	}
	return out
}

// AllEnvKeys returns the SORTED, DEDUPED set of env-var NAMES used
// across workflow/job/step scopes. VALUES ARE NEVER READ — only keys.
// The §5.2 data-minimization contract depends on this method never
// touching map values, so any future change to inline value handling
// must update the contract comment in detector.go.
func (s *workflowSpec) AllEnvKeys() []string {
	if s == nil {
		return nil
	}
	seen := map[string]struct{}{}
	for k := range s.Env {
		seen[k] = struct{}{}
	}
	for _, job := range s.Jobs {
		for k := range job.Env {
			seen[k] = struct{}{}
		}
		for _, step := range job.Steps {
			for k := range step.Env {
				seen[k] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
