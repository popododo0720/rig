package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rigdev/rig/internal/config"
)

// --- Mock adapters ---

type mockAI struct {
	analyzeFunc       func(ctx context.Context, issue *AIIssue, projectCtx string) (*AIPlan, error)
	generateFunc      func(ctx context.Context, plan *AIPlan, repoFiles map[string]string) ([]AIFileChange, error)
	failureFunc       func(ctx context.Context, logs string, currentCode map[string]string) ([]AIFileChange, error)
	deployFailureFunc func(ctx context.Context, deployLogs string, infraFiles map[string]string) (*AIProposedFix, error)
}

func (m *mockAI) AnalyzeIssue(ctx context.Context, issue *AIIssue, projectContext string) (*AIPlan, error) {
	if m.analyzeFunc != nil {
		return m.analyzeFunc(ctx, issue, projectContext)
	}
	return &AIPlan{Summary: "test plan", Steps: []string{"step1"}}, nil
}

func (m *mockAI) GenerateCode(ctx context.Context, plan *AIPlan, repoFiles map[string]string) ([]AIFileChange, error) {
	if m.generateFunc != nil {
		return m.generateFunc(ctx, plan, repoFiles)
	}
	return []AIFileChange{{Path: "main.go", Content: "package main", Action: "modify"}}, nil
}

func (m *mockAI) AnalyzeFailure(ctx context.Context, logs string, currentCode map[string]string) ([]AIFileChange, error) {
	if m.failureFunc != nil {
		return m.failureFunc(ctx, logs, currentCode)
	}
	return []AIFileChange{{Path: "main.go", Content: "package main // fixed", Action: "modify"}}, nil
}

func (m *mockAI) AnalyzeDeployFailure(ctx context.Context, deployLogs string, infraFiles map[string]string) (*AIProposedFix, error) {
	if m.deployFailureFunc != nil {
		return m.deployFailureFunc(ctx, deployLogs, infraFiles)
	}
	return &AIProposedFix{
		Summary: "deploy fix",
		Reason:  "default deploy fix reason",
		Changes: []AIProposedFile{
			{Path: "deploy.yaml", Action: "modify", Reason: "fix config", Content: "apiVersion: v1"},
		},
	}, nil
}

type mockGit struct {
	createBranchErr    error
	commitAndPushErr   error
	createPRErr        error
	createBranchCalls  int
	commitAndPushCalls int
	createPRCalls      int
}

func (m *mockGit) CreateBranch(ctx context.Context, branchName string) error {
	m.createBranchCalls++
	return m.createBranchErr
}

func (m *mockGit) CommitAndPush(ctx context.Context, changes []GitFileChange, message string) error {
	m.commitAndPushCalls++
	return m.commitAndPushErr
}

func (m *mockGit) CreatePR(ctx context.Context, base, head, title, body string) (*GitPullRequest, error) {
	m.createPRCalls++
	if m.createPRErr != nil {
		return nil, m.createPRErr
	}
	return &GitPullRequest{Number: 1, URL: "https://github.com/test/repo/pull/1", Title: title}, nil
}

func (m *mockGit) CloneOrPull(ctx context.Context, owner, repo, token string) error {
	return nil
}

type mockDeploy struct {
	deploySuccess bool
	deployErr     error
	rollbackErr   error
	deployCalls   int
	rollbackCalls int
}

func (m *mockDeploy) Validate() error { return nil }

func (m *mockDeploy) Deploy(ctx context.Context, vars map[string]string) (*AdapterDeployResult, error) {
	m.deployCalls++
	if m.deployErr != nil {
		return nil, m.deployErr
	}
	return &AdapterDeployResult{
		Success:  m.deploySuccess,
		Output:   "deploy output",
		Duration: 1 * time.Second,
	}, nil
}

func (m *mockDeploy) Rollback(ctx context.Context) error {
	m.rollbackCalls++
	return m.rollbackErr
}

type mockTestRunner struct {
	results []*TestResult
	callIdx int
}

func (m *mockTestRunner) Run(ctx context.Context, vars map[string]string) (*TestResult, error) {
	if m.callIdx < len(m.results) {
		r := m.results[m.callIdx]
		m.callIdx++
		return r, nil
	}
	return &TestResult{Name: "default", Type: "command", Passed: true, Duration: time.Second}, nil
}

type mockNotifier struct {
	messages []string
}

func (m *mockNotifier) Notify(ctx context.Context, message string) error {
	m.messages = append(m.messages, message)
	return nil
}

// --- Helpers ---

func testConfig() *config.Config {
	return &config.Config{
		Project: config.ProjectConfig{Name: "test", Language: "go"},
		Source: config.SourceConfig{
			Platform:   "github",
			Repo:       "test/repo",
			BaseBranch: "main",
		},
		AI: config.AIConfig{
			Provider: "anthropic",
			Model:    "test-model",
			MaxRetry: 3,
			Context:  []string{"test project"},
		},
		Deploy: config.DeployConfig{
			Method:   "custom",
			Rollback: config.RollbackConfig{Enabled: false},
		},
		Test: []config.TestConfig{
			{Type: "command", Name: "unit-test", Run: "echo ok"},
		},
	}
}

func testIssue() Issue {
	return Issue{
		Platform: "github",
		Repo:     "test/repo",
		ID:       "42",
		Title:    "Fix the bug",
		URL:      "https://github.com/test/repo/issues/42",
	}
}

func tempStatePath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "state.json")
}

// --- Tests ---

func TestEngine_SuccessPath(t *testing.T) {
	cfg := testConfig()
	gitMock := &mockGit{}
	aiMock := &mockAI{}
	deployMock := &mockDeploy{deploySuccess: true}
	testRunner := &mockTestRunner{
		results: []*TestResult{
			{Name: "unit-test", Type: "command", Passed: true, Duration: time.Second},
		},
	}
	notifier := &mockNotifier{}
	statePath := tempStatePath(t)

	engine := NewEngine(cfg, gitMock, aiMock, deployMock, []TestRunnerIface{testRunner}, []NotifierIface{notifier}, statePath)

	err := engine.Execute(context.Background(), testIssue())
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	// Verify state was saved.
	state, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	if len(state.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(state.Tasks))
	}
	task := state.Tasks[0]
	if task.Status != PhaseCompleted {
		t.Fatalf("expected completed status, got %s", task.Status)
	}
	if task.PR == nil {
		t.Fatal("expected PR to be set")
	}
	if task.PR.URL != "https://github.com/test/repo/pull/1" {
		t.Fatalf("unexpected PR URL: %s", task.PR.URL)
	}

	// Verify adapter calls.
	if gitMock.createBranchCalls != 1 {
		t.Fatalf("expected 1 createBranch call, got %d", gitMock.createBranchCalls)
	}
	if gitMock.commitAndPushCalls != 1 {
		t.Fatalf("expected 1 commitAndPush call, got %d", gitMock.commitAndPushCalls)
	}
	if gitMock.createPRCalls != 1 {
		t.Fatalf("expected 1 createPR call, got %d", gitMock.createPRCalls)
	}
	if deployMock.deployCalls != 1 {
		t.Fatalf("expected 1 deploy call, got %d", deployMock.deployCalls)
	}

	// Verify notifications were sent.
	if len(notifier.messages) == 0 {
		t.Fatal("expected notifications to be sent")
	}
}

func TestEngine_RetrySuccess(t *testing.T) {
	cfg := testConfig()
	cfg.AI.MaxRetry = 3

	gitMock := &mockGit{}
	aiMock := &mockAI{}
	deployMock := &mockDeploy{deploySuccess: true}

	// First call: test fails. Second call: test passes.
	callCount := 0
	testRunner := &mockTestRunner{
		results: []*TestResult{
			{Name: "unit-test", Type: "command", Passed: false, Output: "FAIL", Duration: time.Second},
			{Name: "unit-test", Type: "command", Passed: true, Output: "PASS", Duration: time.Second},
		},
	}
	_ = callCount
	notifier := &mockNotifier{}
	statePath := tempStatePath(t)

	engine := NewEngine(cfg, gitMock, aiMock, deployMock, []TestRunnerIface{testRunner}, []NotifierIface{notifier}, statePath)

	err := engine.Execute(context.Background(), testIssue())
	if err != nil {
		t.Fatalf("expected success after retry, got error: %v", err)
	}

	// Verify state.
	state, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	if len(state.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(state.Tasks))
	}
	task := state.Tasks[0]
	if task.Status != PhaseCompleted {
		t.Fatalf("expected completed, got %s", task.Status)
	}

	// Should have 2 attempts: first failed, second passed.
	if len(task.Attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(task.Attempts))
	}
	if task.Attempts[0].Status != "failed" {
		t.Fatalf("expected first attempt to be failed, got %s", task.Attempts[0].Status)
	}
	if task.Attempts[1].Status != "passed" {
		t.Fatalf("expected second attempt to be passed, got %s", task.Attempts[1].Status)
	}
}

func TestEngine_MaxRetry(t *testing.T) {
	cfg := testConfig()
	cfg.AI.MaxRetry = 2
	cfg.Deploy.Rollback.Enabled = true

	gitMock := &mockGit{}
	aiMock := &mockAI{}
	deployMock := &mockDeploy{deploySuccess: true}

	// All tests fail.
	testRunner := &mockTestRunner{
		results: []*TestResult{
			{Name: "unit-test", Type: "command", Passed: false, Output: "FAIL 1", Duration: time.Second},
			{Name: "unit-test", Type: "command", Passed: false, Output: "FAIL 2", Duration: time.Second},
			{Name: "unit-test", Type: "command", Passed: false, Output: "FAIL 3", Duration: time.Second},
		},
	}
	notifier := &mockNotifier{}
	statePath := tempStatePath(t)

	engine := NewEngine(cfg, gitMock, aiMock, deployMock, []TestRunnerIface{testRunner}, []NotifierIface{notifier}, statePath)

	err := engine.Execute(context.Background(), testIssue())
	if err == nil {
		t.Fatal("expected error after max retries")
	}

	// Verify state.
	state, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	if len(state.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(state.Tasks))
	}
	task := state.Tasks[0]

	// Should be in rollback state (since rollback is enabled).
	if task.Status != PhaseRollback {
		t.Fatalf("expected rollback status, got %s", task.Status)
	}

	// Rollback should have been called.
	if deployMock.rollbackCalls != 1 {
		t.Fatalf("expected 1 rollback call, got %d", deployMock.rollbackCalls)
	}
}

func TestEngine_DryRun(t *testing.T) {
	cfg := testConfig()
	gitMock := &mockGit{}
	aiMock := &mockAI{}
	deployMock := &mockDeploy{deploySuccess: true}
	notifier := &mockNotifier{}
	statePath := tempStatePath(t)

	engine := NewEngine(cfg, gitMock, aiMock, deployMock, nil, []NotifierIface{notifier}, statePath)
	engine.SetDryRun(true)

	err := engine.Execute(context.Background(), testIssue())
	if err != nil {
		t.Fatalf("expected success in dry-run, got error: %v", err)
	}

	// State file should NOT exist in dry-run (no SaveState called).
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatal("expected state file to not exist in dry-run mode")
	}

	// No adapter calls should have been made.
	if gitMock.createBranchCalls != 0 {
		t.Fatalf("expected 0 createBranch calls in dry-run, got %d", gitMock.createBranchCalls)
	}
	if deployMock.deployCalls != 0 {
		t.Fatalf("expected 0 deploy calls in dry-run, got %d", deployMock.deployCalls)
	}
}

func TestEngine_AIAnalyzeError(t *testing.T) {
	cfg := testConfig()
	aiMock := &mockAI{
		analyzeFunc: func(ctx context.Context, issue *AIIssue, projectCtx string) (*AIPlan, error) {
			return nil, fmt.Errorf("AI service unavailable")
		},
	}
	deployMock := &mockDeploy{deploySuccess: true}
	gitMock := &mockGit{}
	notifier := &mockNotifier{}
	statePath := tempStatePath(t)

	engine := NewEngine(cfg, gitMock, aiMock, deployMock, nil, []NotifierIface{notifier}, statePath)

	err := engine.Execute(context.Background(), testIssue())
	if err == nil {
		t.Fatal("expected error when AI fails")
	}

	// Verify task is in failed state.
	state, _ := LoadState(statePath)
	if len(state.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(state.Tasks))
	}
	if state.Tasks[0].Status != PhaseFailed {
		t.Fatalf("expected failed status, got %s", state.Tasks[0].Status)
	}
}

func TestEngine_DeployFailure(t *testing.T) {
	cfg := testConfig()
	aiMock := &mockAI{}
	deployMock := &mockDeploy{deploySuccess: false}
	gitMock := &mockGit{}
	notifier := &mockNotifier{}
	statePath := tempStatePath(t)

	engine := NewEngine(cfg, gitMock, aiMock, deployMock, nil, []NotifierIface{notifier}, statePath)

	err := engine.Execute(context.Background(), testIssue())
	if err == nil {
		t.Fatal("expected error when deploy fails")
	}

	// With the proposal system, deploy failure triggers AI analysis and
	// transitions to awaiting_approval (default manual mode).
	if !errors.Is(err, ErrAwaitingApproval) {
		t.Fatalf("expected ErrAwaitingApproval, got: %v", err)
	}

	state, _ := LoadState(statePath)
	if state.Tasks[0].Status != PhaseAwaitingApproval {
		t.Fatalf("expected awaiting_approval status, got %s", state.Tasks[0].Status)
	}

	// Should have a pending proposal.
	if len(state.Tasks[0].Proposals) == 0 {
		t.Fatal("expected at least one proposal")
	}
	if state.Tasks[0].Proposals[0].Status != ProposalPending {
		t.Fatalf("expected pending proposal, got %s", state.Tasks[0].Proposals[0].Status)
	}
}

func TestBuildVars(t *testing.T) {
	cfg := testConfig()
	engine := NewEngine(cfg, nil, nil, nil, nil, nil, "")

	task := &Task{
		ID:     "task-001",
		Branch: "rig/issue-42",
		Issue:  Issue{ID: "42", Title: "Fix bug"},
	}

	vars := engine.buildVars(task)

	expected := map[string]string{
		"BRANCH_NAME":  "rig/issue-42",
		"ISSUE_ID":     "42",
		"ISSUE_NUMBER": "42",
		"ISSUE_TITLE":  "Fix bug",
		"REPO_OWNER":   "test",
		"REPO_NAME":    "repo",
	}

	for k, v := range expected {
		if vars[k] != v {
			t.Errorf("expected vars[%s]=%q, got %q", k, v, vars[k])
		}
	}
}

func TestParseRepo(t *testing.T) {
	tests := []struct {
		input     string
		wantOwner string
		wantRepo  string
	}{
		{"owner/repo", "owner", "repo"},
		{"org/my-project", "org", "my-project"},
		{"noslash", "noslash", ""},
	}

	for _, tt := range tests {
		owner, repo := parseRepo(tt.input)
		if owner != tt.wantOwner || repo != tt.wantRepo {
			t.Errorf("parseRepo(%q) = (%q, %q), want (%q, %q)",
				tt.input, owner, repo, tt.wantOwner, tt.wantRepo)
		}
	}
}
