package policy

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rigdev/rig/internal/config"
	"github.com/rigdev/rig/internal/core"
)

// PolicyViolation represents a failed policy check.
type PolicyViolation struct {
	Name    string `json:"name"`
	Rule    string `json:"rule"`
	Action  string `json:"action"`
	Message string `json:"message"`
}

// Evaluate applies policy checks to the current task and generated changes.
func Evaluate(policies []config.PolicyConfig, task *core.Task, changes []core.AIFileChange) []PolicyViolation {
	violations := make([]PolicyViolation, 0)

	for _, p := range policies {
		action := normalizeAction(p.Action)
		rule := strings.TrimSpace(strings.ToLower(p.Rule))

		switch rule {
		case "max_file_changes":
			limit, err := strconv.Atoi(strings.TrimSpace(p.Value))
			if err != nil || limit < 0 {
				continue
			}
			if len(changes) > limit {
				violations = append(violations, PolicyViolation{
					Name:    p.Name,
					Rule:    p.Rule,
					Action:  action,
					Message: "generated file changes exceed configured limit",
				})
			}

		case "require_tests":
			if !taskHasTestsConfigured(task) {
				violations = append(violations, PolicyViolation{
					Name:    p.Name,
					Rule:    p.Rule,
					Action:  action,
					Message: "tests are required but no test configuration is available",
				})
			}

		case "blocked_paths":
			patterns := splitPolicyValue(p.Value)
			if len(patterns) == 0 {
				continue
			}
			for _, change := range changes {
				if pathMatchesAnyPattern(change.Path, patterns) {
					violations = append(violations, PolicyViolation{
						Name:    p.Name,
						Rule:    p.Rule,
						Action:  action,
						Message: "generated change touches a blocked path: " + change.Path,
					})
					break
				}
			}

		case "max_retries":
			limit, err := strconv.Atoi(strings.TrimSpace(p.Value))
			if err != nil || limit < 0 || task == nil {
				continue
			}
			retries := len(task.Attempts)
			if retries > limit {
				violations = append(violations, PolicyViolation{
					Name:    p.Name,
					Rule:    p.Rule,
					Action:  action,
					Message: "task retries exceed configured limit",
				})
			}
		}
	}

	return violations
}

func normalizeAction(action string) string {
	v := strings.TrimSpace(strings.ToLower(action))
	if v == "warn" {
		return "warn"
	}
	return "block"
}

func splitPolicyValue(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		norm := strings.TrimSpace(p)
		if norm == "" {
			continue
		}
		out = append(out, filepath.ToSlash(norm))
	}
	return out
}

func pathMatchesAnyPattern(path string, patterns []string) bool {
	normPath := filepath.ToSlash(path)
	for _, pattern := range patterns {
		if strings.HasSuffix(pattern, "/") && strings.HasPrefix(normPath, pattern) {
			return true
		}
		if strings.HasPrefix(normPath, pattern) || normPath == pattern {
			return true
		}
		if ok, err := filepath.Match(pattern, normPath); err == nil && ok {
			return true
		}
	}
	return false
}

func taskHasTestsConfigured(task *core.Task) bool {
	if task == nil {
		return false
	}
	for _, step := range task.Pipeline {
		if step.Phase == core.PhaseTesting {
			return true
		}
	}
	for _, attempt := range task.Attempts {
		if len(attempt.Tests) > 0 {
			return true
		}
	}
	return false
}
