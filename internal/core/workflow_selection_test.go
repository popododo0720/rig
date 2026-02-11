package core

import (
	"context"
	"testing"
	"time"

	"github.com/rigdev/rig/internal/config"
)

type selectionRunner struct {
	name   string
	called int
}

func (r *selectionRunner) Run(ctx context.Context, vars map[string]string) (*TestResult, error) {
	r.called++
	return &TestResult{Name: r.name, Type: "command", Passed: true, Duration: time.Millisecond}, nil
}

func TestStepTest_AffectedPathsFiltersRunners(t *testing.T) {
	runAPI := &selectionRunner{name: "api"}
	runWeb := &selectionRunner{name: "web"}

	runners := []TestRunnerIface{runAPI, runWeb}
	testCfgs := []config.TestConfig{
		{Name: "api", Type: "command", AffectedPaths: []string{"api/", "internal/api/"}},
		{Name: "web", Type: "command", AffectedPaths: []string{"web/"}},
	}

	results, allPassed := stepTest(context.Background(), runners, testCfgs, []string{"api/handler.go"}, map[string]string{})
	if !allPassed {
		t.Fatal("expected allPassed=true")
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if runAPI.called != 1 {
		t.Fatalf("expected api runner to run once, got %d", runAPI.called)
	}
	if runWeb.called != 0 {
		t.Fatalf("expected web runner to be skipped, got %d", runWeb.called)
	}
}

func TestStepTest_AffectedPathsGlobMatch(t *testing.T) {
	runner := &selectionRunner{name: "env-test"}
	runners := []TestRunnerIface{runner}
	testCfgs := []config.TestConfig{{Name: "env-test", Type: "command", AffectedPaths: []string{"**/*.env"}}}

	results, allPassed := stepTest(context.Background(), runners, testCfgs, []string{"configs/prod.env"}, map[string]string{})
	if !allPassed {
		t.Fatal("expected allPassed=true")
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if runner.called != 1 {
		t.Fatalf("expected runner to be called, got %d", runner.called)
	}
}

func TestStepTest_NoAffectedPathsRunsAll(t *testing.T) {
	runner := &selectionRunner{name: "unit"}
	runners := []TestRunnerIface{runner}
	testCfgs := []config.TestConfig{{Name: "unit", Type: "command"}}

	results, allPassed := stepTest(context.Background(), runners, testCfgs, nil, map[string]string{})
	if !allPassed {
		t.Fatal("expected allPassed=true")
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if runner.called != 1 {
		t.Fatalf("expected runner to run, got %d", runner.called)
	}
}
