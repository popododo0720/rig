package policy_test

import (
	"testing"

	"github.com/rigdev/rig/internal/config"
	"github.com/rigdev/rig/internal/core"
	"github.com/rigdev/rig/internal/policy"
)

func TestEvaluate_MaxFileChanges(t *testing.T) {
	policies := []config.PolicyConfig{{Name: "limit", Rule: "max_file_changes", Value: "1", Action: "block"}}
	changes := []core.AIFileChange{{Path: "a.go"}, {Path: "b.go"}}

	violations := policy.Evaluate(policies, &core.Task{}, changes)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].Action != "block" {
		t.Fatalf("expected block action, got %s", violations[0].Action)
	}
}

func TestEvaluate_BlockedPaths(t *testing.T) {
	policies := []config.PolicyConfig{{Name: "blocked", Rule: "blocked_paths", Value: "*.env,secrets/", Action: "block"}}
	changes := []core.AIFileChange{{Path: "secrets/key.txt"}, {Path: "app/main.go"}}

	violations := policy.Evaluate(policies, &core.Task{}, changes)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
}

func TestEvaluate_RequireTests(t *testing.T) {
	policies := []config.PolicyConfig{{Name: "require", Rule: "require_tests", Action: "block"}}

	violations := policy.Evaluate(policies, &core.Task{}, nil)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	taskWithTesting := &core.Task{Pipeline: []core.PipelineStep{{Phase: core.PhaseTesting}}}
	violations = policy.Evaluate(policies, taskWithTesting, nil)
	if len(violations) != 0 {
		t.Fatalf("expected no violations when testing is configured, got %d", len(violations))
	}
}

func TestEvaluate_MaxRetriesWarn(t *testing.T) {
	policies := []config.PolicyConfig{{Name: "retry-warn", Rule: "max_retries", Value: "1", Action: "warn"}}
	task := &core.Task{Attempts: []core.Attempt{{Number: 1}, {Number: 2}}}

	violations := policy.Evaluate(policies, task, nil)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].Action != "warn" {
		t.Fatalf("expected warn action, got %s", violations[0].Action)
	}
}
