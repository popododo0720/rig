package test

import (
	"context"
	"testing"
	"time"

	"github.com/rigdev/rig/internal/config"
)

func TestCommandRunner_Success(t *testing.T) {
	runner := NewCommandRunner(config.TestConfig{
		Type:    "command",
		Name:    "echo-test",
		Run:     "echo hello",
		Timeout: 10 * time.Second,
	})

	result, err := runner.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected test to pass, got fail: %s", result.Output)
	}
	if result.Name != "echo-test" {
		t.Fatalf("expected name 'echo-test', got %q", result.Name)
	}
	if result.Duration <= 0 {
		t.Fatal("expected positive duration")
	}
}

func TestCommandRunner_Failure(t *testing.T) {
	runner := NewCommandRunner(config.TestConfig{
		Type:    "command",
		Name:    "fail-test",
		Run:     "exit 1",
		Timeout: 10 * time.Second,
	})

	result, err := runner.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Fatal("expected test to fail")
	}
}

func TestCommandRunner_Timeout(t *testing.T) {
	runner := NewCommandRunner(config.TestConfig{
		Type:    "command",
		Name:    "timeout-test",
		Run:     "sleep 30",
		Timeout: 500 * time.Millisecond,
	})

	result, err := runner.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Fatal("expected test to fail on timeout")
	}
	if result.Duration > 5*time.Second {
		t.Fatalf("test took too long: %v", result.Duration)
	}
}

func TestCommandRunner_VariableResolution(t *testing.T) {
	runner := NewCommandRunner(config.TestConfig{
		Type:    "command",
		Name:    "var-test",
		Run:     "echo ${BRANCH_NAME}",
		Timeout: 10 * time.Second,
	})

	vars := map[string]string{
		"BRANCH_NAME": "rig/issue-42",
	}

	result, err := runner.Run(context.Background(), vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected test to pass: %s", result.Output)
	}
}

func TestCommandRunner_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	runner := NewCommandRunner(config.TestConfig{
		Type: "command",
		Name: "cancel-test",
		Run:  "echo hello",
	})

	result, err := runner.Run(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// When context is already cancelled, the command should fail.
	if result.Passed {
		t.Fatal("expected test to fail with cancelled context")
	}
}
