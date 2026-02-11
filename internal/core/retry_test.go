package core

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func newRetryTestHarness(t *testing.T, maxRetry int, aiMock *mockAI, gitMock *mockGit, deployMock *mockDeploy, testRunner *mockTestRunner) (*Engine, *Task, Attempt, map[string]string, []TestResult, []AIFileChange) {
	t.Helper()

	cfg := testConfig()
	cfg.AI.MaxRetry = maxRetry
	statePath := tempStatePath(t)
	notifier := &mockNotifier{}

	engine := NewEngine(
		cfg,
		gitMock,
		aiMock,
		deployMock,
		[]TestRunnerIface{testRunner},
		[]NotifierIface{notifier},
		statePath,
	)

	initialAttempt := newAttempt(1)
	completeAttempt(&initialAttempt, "failed", ReasonTest)

	task := &Task{
		ID:       "test-task",
		Issue:    testIssue(),
		Branch:   "rig/issue-42",
		Status:   PhaseTesting,
		Attempts: []Attempt{initialAttempt},
	}

	vars := map[string]string{"BRANCH_NAME": "rig/issue-42"}
	initialResults := []TestResult{{Name: "unit-test", Type: "command", Passed: false, Output: "FAIL"}}
	initialChanges := []AIFileChange{{Path: "main.go", Content: "package main", Action: "modify"}}

	return engine, task, initialAttempt, vars, initialResults, initialChanges
}

func TestRetryLoop_ImmediateSuccess(t *testing.T) {
	aiMock := &mockAI{}
	gitMock := &mockGit{}
	deployMock := &mockDeploy{deploySuccess: true}
	testRunner := &mockTestRunner{results: []*TestResult{{Name: "unit-test", Type: "command", Passed: true, Output: "PASS"}}}

	engine, task, _, vars, initialResults, initialChanges := newRetryTestHarness(t, 3, aiMock, gitMock, deployMock, testRunner)

	err := retryLoop(context.Background(), engine, task, vars, initialResults, initialChanges, 3)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	if len(task.Attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(task.Attempts))
	}
	last := task.Attempts[1]
	if last.Status != "passed" {
		t.Fatalf("expected last attempt status passed, got %s", last.Status)
	}
	if last.FailReason != "" {
		t.Fatalf("expected no fail reason for passed attempt, got %s", last.FailReason)
	}
	if gitMock.commitAndPushCalls != 1 {
		t.Fatalf("expected 1 commit call, got %d", gitMock.commitAndPushCalls)
	}
	if deployMock.deployCalls != 1 {
		t.Fatalf("expected 1 deploy call, got %d", deployMock.deployCalls)
	}
}

func TestRetryLoop_SuccessAfterMultipleRetries(t *testing.T) {
	aiMock := &mockAI{}
	gitMock := &mockGit{}
	deployMock := &mockDeploy{deploySuccess: true}
	testRunner := &mockTestRunner{
		results: []*TestResult{
			{Name: "unit-test", Type: "command", Passed: false, Output: "FAIL 1"},
			{Name: "unit-test", Type: "command", Passed: false, Output: "FAIL 2"},
			{Name: "unit-test", Type: "command", Passed: true, Output: "PASS"},
		},
	}

	engine, task, _, vars, initialResults, initialChanges := newRetryTestHarness(t, 3, aiMock, gitMock, deployMock, testRunner)

	err := retryLoop(context.Background(), engine, task, vars, initialResults, initialChanges, 3)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	if len(task.Attempts) != 4 {
		t.Fatalf("expected 4 attempts, got %d", len(task.Attempts))
	}
	if task.Attempts[1].Status != "failed" || task.Attempts[2].Status != "failed" || task.Attempts[3].Status != "passed" {
		t.Fatalf("unexpected retry statuses: %s, %s, %s", task.Attempts[1].Status, task.Attempts[2].Status, task.Attempts[3].Status)
	}
}

func TestRetryLoop_MaxRetryExceeded(t *testing.T) {
	aiMock := &mockAI{}
	gitMock := &mockGit{}
	deployMock := &mockDeploy{deploySuccess: true}
	testRunner := &mockTestRunner{
		results: []*TestResult{
			{Name: "unit-test", Type: "command", Passed: false, Output: "FAIL 1"},
			{Name: "unit-test", Type: "command", Passed: false, Output: "FAIL 2"},
		},
	}

	engine, task, _, vars, initialResults, initialChanges := newRetryTestHarness(t, 2, aiMock, gitMock, deployMock, testRunner)

	err := retryLoop(context.Background(), engine, task, vars, initialResults, initialChanges, 2)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "max retry count (2) exceeded") {
		t.Fatalf("expected max retry error, got: %v", err)
	}
	if len(task.Attempts) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(task.Attempts))
	}
}

func TestRetryLoop_MaxRetryZero_Unlimited(t *testing.T) {
	// max_retry=0 means unlimited retries. Test that it retries until tests pass.
	analyzeCalls := 0
	aiMock := &mockAI{
		failureFunc: func(ctx context.Context, logs string, currentCode map[string]string) ([]AIFileChange, error) {
			analyzeCalls++
			return []AIFileChange{{Path: "main.go", Content: "package main // fixed", Action: "modify"}}, nil
		},
	}
	gitMock := &mockGit{}
	deployMock := &mockDeploy{deploySuccess: true}
	testRunner := &mockTestRunner{results: []*TestResult{{Name: "unit-test", Type: "command", Passed: true, Output: "PASS"}}}

	engine, task, _, vars, initialResults, initialChanges := newRetryTestHarness(t, 0, aiMock, gitMock, deployMock, testRunner)

	err := retryLoop(context.Background(), engine, task, vars, initialResults, initialChanges, 0)
	if err != nil {
		t.Fatalf("expected nil error (unlimited retry should succeed), got: %v", err)
	}
	if analyzeCalls != 1 {
		t.Fatalf("expected AnalyzeFailure called once, got %d", analyzeCalls)
	}
	if len(task.Attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(task.Attempts))
	}
	last := task.Attempts[1]
	if last.Status != "passed" {
		t.Fatalf("expected last attempt status passed, got %s", last.Status)
	}
}

func TestRetryLoop_AnalyzeFailureError(t *testing.T) {
	aiMock := &mockAI{
		failureFunc: func(ctx context.Context, logs string, currentCode map[string]string) ([]AIFileChange, error) {
			return nil, errors.New("analysis failed")
		},
	}
	gitMock := &mockGit{}
	deployMock := &mockDeploy{deploySuccess: true}
	testRunner := &mockTestRunner{results: []*TestResult{{Name: "unit-test", Type: "command", Passed: true, Output: "PASS"}}}

	engine, task, _, vars, initialResults, initialChanges := newRetryTestHarness(t, 3, aiMock, gitMock, deployMock, testRunner)

	err := retryLoop(context.Background(), engine, task, vars, initialResults, initialChanges, 3)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "analyze failure") {
		t.Fatalf("expected analyze failure error, got: %v", err)
	}
	if len(task.Attempts) != 1 {
		t.Fatalf("expected attempts to remain 1, got %d", len(task.Attempts))
	}
}

func TestRetryLoop_CommitError(t *testing.T) {
	aiMock := &mockAI{}
	gitMock := &mockGit{commitAndPushErr: errors.New("commit failed")}
	deployMock := &mockDeploy{deploySuccess: true}
	testRunner := &mockTestRunner{results: []*TestResult{{Name: "unit-test", Type: "command", Passed: true, Output: "PASS"}}}

	engine, task, _, vars, initialResults, initialChanges := newRetryTestHarness(t, 3, aiMock, gitMock, deployMock, testRunner)

	err := retryLoop(context.Background(), engine, task, vars, initialResults, initialChanges, 3)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "commit retry changes") {
		t.Fatalf("expected commit retry error, got: %v", err)
	}
	if len(task.Attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(task.Attempts))
	}
	last := task.Attempts[1]
	if last.Status != "failed" {
		t.Fatalf("expected failed status, got %s", last.Status)
	}
	if last.FailReason != ReasonGit {
		t.Fatalf("expected fail reason %s, got %s", ReasonGit, last.FailReason)
	}
}

func TestRetryLoop_DeployError(t *testing.T) {
	aiMock := &mockAI{}
	gitMock := &mockGit{}
	deployMock := &mockDeploy{deployErr: errors.New("deploy adapter error")}
	testRunner := &mockTestRunner{results: []*TestResult{{Name: "unit-test", Type: "command", Passed: true, Output: "PASS"}}}

	engine, task, _, vars, initialResults, initialChanges := newRetryTestHarness(t, 3, aiMock, gitMock, deployMock, testRunner)

	err := retryLoop(context.Background(), engine, task, vars, initialResults, initialChanges, 3)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "deploy retry") {
		t.Fatalf("expected deploy retry error, got: %v", err)
	}
	if len(task.Attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(task.Attempts))
	}
	last := task.Attempts[1]
	if last.Status != "failed" {
		t.Fatalf("expected failed status, got %s", last.Status)
	}
	if last.FailReason != ReasonDeploy {
		t.Fatalf("expected fail reason %s, got %s", ReasonDeploy, last.FailReason)
	}
}

func TestRetryLoop_DeployNotSuccess(t *testing.T) {
	aiMock := &mockAI{}
	gitMock := &mockGit{}
	deployMock := &mockDeploy{deploySuccess: false}
	testRunner := &mockTestRunner{results: []*TestResult{{Name: "unit-test", Type: "command", Passed: true, Output: "PASS"}}}

	engine, task, _, vars, initialResults, initialChanges := newRetryTestHarness(t, 3, aiMock, gitMock, deployMock, testRunner)

	err := retryLoop(context.Background(), engine, task, vars, initialResults, initialChanges, 3)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "deploy failed during retry") {
		t.Fatalf("expected deploy failed during retry error, got: %v", err)
	}
	if len(task.Attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(task.Attempts))
	}
	last := task.Attempts[1]
	if last.Status != "failed" {
		t.Fatalf("expected failed status, got %s", last.Status)
	}
	if last.FailReason != ReasonDeploy {
		t.Fatalf("expected fail reason %s, got %s", ReasonDeploy, last.FailReason)
	}
}

func TestRetryLoop_AttemptsAppended(t *testing.T) {
	call := 0
	aiMock := &mockAI{
		failureFunc: func(ctx context.Context, logs string, currentCode map[string]string) ([]AIFileChange, error) {
			call++
			if call == 1 {
				return []AIFileChange{{Path: "retry1.go", Content: "package retry", Action: "modify"}}, nil
			}
			return []AIFileChange{
				{Path: "retry2.go", Content: "package retry", Action: "modify"},
				{Path: "shared.go", Content: "package retry", Action: "modify"},
			}, nil
		},
	}
	gitMock := &mockGit{}
	deployMock := &mockDeploy{deploySuccess: true}
	testRunner := &mockTestRunner{
		results: []*TestResult{
			{Name: "unit-test", Type: "command", Passed: false, Output: "FAIL 1"},
			{Name: "unit-test", Type: "command", Passed: true, Output: "PASS"},
		},
	}

	engine, task, _, vars, initialResults, initialChanges := newRetryTestHarness(t, 3, aiMock, gitMock, deployMock, testRunner)

	err := retryLoop(context.Background(), engine, task, vars, initialResults, initialChanges, 3)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	if len(task.Attempts) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(task.Attempts))
	}

	firstRetry := task.Attempts[1]
	if firstRetry.Number != 2 {
		t.Fatalf("expected first retry number 2, got %d", firstRetry.Number)
	}
	if firstRetry.Plan != "Retry #1: fix based on test failures" {
		t.Fatalf("unexpected first retry plan: %s", firstRetry.Plan)
	}
	if len(firstRetry.FilesChanged) != 1 || firstRetry.FilesChanged[0] != "retry1.go" {
		t.Fatalf("unexpected first retry files changed: %#v", firstRetry.FilesChanged)
	}
	if firstRetry.Status != "failed" {
		t.Fatalf("expected first retry status failed, got %s", firstRetry.Status)
	}

	secondRetry := task.Attempts[2]
	if secondRetry.Number != 3 {
		t.Fatalf("expected second retry number 3, got %d", secondRetry.Number)
	}
	if secondRetry.Plan != "Retry #2: fix based on test failures" {
		t.Fatalf("unexpected second retry plan: %s", secondRetry.Plan)
	}
	if len(secondRetry.FilesChanged) != 2 || secondRetry.FilesChanged[0] != "retry2.go" || secondRetry.FilesChanged[1] != "shared.go" {
		t.Fatalf("unexpected second retry files changed: %#v", secondRetry.FilesChanged)
	}
	if secondRetry.Status != "passed" {
		t.Fatalf("expected second retry status passed, got %s", secondRetry.Status)
	}
}

func TestRetryLoop_ContextCancellation(t *testing.T) {
	aiMock := &mockAI{
		failureFunc: func(ctx context.Context, logs string, currentCode map[string]string) ([]AIFileChange, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return []AIFileChange{{Path: "main.go", Content: "package main // fixed", Action: "modify"}}, nil
		},
	}
	gitMock := &mockGit{}
	deployMock := &mockDeploy{deploySuccess: true}
	testRunner := &mockTestRunner{results: []*TestResult{{Name: "unit-test", Type: "command", Passed: true, Output: "PASS"}}}

	engine, task, _, vars, initialResults, initialChanges := newRetryTestHarness(t, 3, aiMock, gitMock, deployMock, testRunner)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := retryLoop(ctx, engine, task, vars, initialResults, initialChanges, 3)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("expected context canceled error, got: %v", err)
	}
	if len(task.Attempts) != 1 {
		t.Fatalf("expected attempts to remain 1, got %d", len(task.Attempts))
	}
}
