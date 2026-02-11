package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rigdev/rig/internal/config"
)

func TestEngine_PolicyBlockStopsBeforeCommit(t *testing.T) {
	cfg := testConfig()
	cfg.Policies = []config.PolicyConfig{{
		Name:   "block-main",
		Rule:   "blocked_paths",
		Value:  "main.go",
		Action: "block",
	}}

	gitMock := &mockGit{}
	aiMock := &mockAI{}
	deployMock := &mockDeploy{deploySuccess: true}
	testRunner := &mockTestRunner{results: []*TestResult{{Name: "unit-test", Type: "command", Passed: true, Duration: time.Second}}}
	statePath := tempStatePath(t)

	engine := NewEngine(cfg, gitMock, aiMock, deployMock, []TestRunnerIface{testRunner}, nil, statePath)
	err := engine.Execute(context.Background(), testIssue())
	if err == nil {
		t.Fatal("expected policy violation error")
	}
	if !strings.Contains(err.Error(), "policy violation") {
		t.Fatalf("expected policy violation error, got: %v", err)
	}

	if gitMock.createBranchCalls != 0 {
		t.Fatalf("expected no branch creation when policy blocks, got %d", gitMock.createBranchCalls)
	}
	if gitMock.commitAndPushCalls != 0 {
		t.Fatalf("expected no commit when policy blocks, got %d", gitMock.commitAndPushCalls)
	}

	state, loadErr := LoadState(statePath)
	if loadErr != nil {
		t.Fatalf("failed to load state: %v", loadErr)
	}
	if len(state.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(state.Tasks))
	}
	if state.Tasks[0].Status != PhaseFailed {
		t.Fatalf("expected failed task status, got %s", state.Tasks[0].Status)
	}
	if len(state.Tasks[0].Attempts) != 1 {
		t.Fatalf("expected one failed attempt, got %d", len(state.Tasks[0].Attempts))
	}
	if state.Tasks[0].Attempts[0].FailReason != ReasonConfig {
		t.Fatalf("expected fail reason %s, got %s", ReasonConfig, state.Tasks[0].Attempts[0].FailReason)
	}
}
