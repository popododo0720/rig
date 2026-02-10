package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rigdev/rig/internal/config"
)

// ─── E2E helpers ────────────────────────────────────────────────────

// e2eConfig returns a config suitable for E2E tests.
// The caller can override fields after creation.
func e2eConfig() *config.Config {
	return &config.Config{
		Project: config.ProjectConfig{
			Name:        "e2e-test",
			Language:    "go",
			Description: "E2E integration test project",
		},
		Source: config.SourceConfig{
			Platform:   "github",
			Repo:       "e2e-org/e2e-repo",
			BaseBranch: "main",
		},
		AI: config.AIConfig{
			Provider: "anthropic",
			Model:    "claude-3-5-sonnet-20241022",
			MaxRetry: 3,
			Context:  []string{"E2E test project context"},
		},
		Deploy: config.DeployConfig{
			Method: "custom",
			Config: config.DeployMethodConfig{
				Commands: []config.CustomCommand{
					{
						Name:    "build",
						Run:     "echo building",
						Workdir: ".",
						Timeout: 30 * time.Second,
						Transport: config.TransportConfig{
							Type: "local",
						},
					},
				},
			},
			Timeout:  60 * time.Second,
			Rollback: config.RollbackConfig{Enabled: false},
		},
		Test: []config.TestConfig{
			{Type: "command", Name: "unit-test", Run: "echo ok", Timeout: 30 * time.Second},
		},
		Workflow: config.WorkflowConfig{
			Trigger: []config.TriggerConfig{
				{Event: "issue.opened", Labels: []string{"auto"}},
			},
			Steps:    []string{"code", "deploy", "test", "report"},
			Approval: config.ApprovalConfig{BeforeDeploy: false},
		},
	}
}

// e2eIssue returns a test issue for E2E scenarios.
func e2eIssue() Issue {
	return Issue{
		Platform: "github",
		Repo:     "e2e-org/e2e-repo",
		ID:       "100",
		Title:    "Add user authentication endpoint",
		URL:      "https://github.com/e2e-org/e2e-repo/issues/100",
	}
}

// verifyStateFile loads the state file and returns the state for assertions.
func verifyStateFile(t *testing.T, path string) *State {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read state file: %v", err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("failed to unmarshal state: %v", err)
	}
	return &state
}

// ─── TestE2EHappyPath ──────────────────────────────────────────────
// Full cycle: issue → analyze → generate → commit → deploy → test PASS → PR → completed

func TestE2EHappyPath(t *testing.T) {
	cfg := e2eConfig()
	statePath := filepath.Join(t.TempDir(), "state.json")

	// Track adapter calls for verification.
	aiAnalyzeCalls := 0
	aiGenerateCalls := 0

	aiMock := &mockAI{
		analyzeFunc: func(ctx context.Context, issue *AIIssue, projectCtx string) (*AIPlan, error) {
			aiAnalyzeCalls++
			// Verify issue data was passed through.
			if issue.Title != "Add user authentication endpoint" {
				t.Errorf("AI received wrong issue title: %q", issue.Title)
			}
			if projectCtx == "" {
				t.Error("AI received empty project context")
			}
			return &AIPlan{
				Summary: "Add auth endpoint with JWT",
				Steps:   []string{"Create handler", "Add middleware", "Write tests"},
			}, nil
		},
		generateFunc: func(ctx context.Context, plan *AIPlan, repoFiles map[string]string) ([]AIFileChange, error) {
			aiGenerateCalls++
			return []AIFileChange{
				{Path: "internal/auth/handler.go", Content: "package auth\n\nfunc LoginHandler() {}", Action: "create"},
				{Path: "internal/auth/middleware.go", Content: "package auth\n\nfunc AuthMiddleware() {}", Action: "create"},
			}, nil
		},
	}

	gitMock := &mockGit{}
	deployMock := &mockDeploy{deploySuccess: true}

	testRunner := &mockTestRunner{
		results: []*TestResult{
			{Name: "unit-test", Type: "command", Passed: true, Output: "ok\tall tests passed", Duration: 2 * time.Second},
		},
	}

	notifier := &mockNotifier{}

	engine := NewEngine(cfg, gitMock, aiMock, deployMock,
		[]TestRunnerIface{testRunner},
		[]NotifierIface{notifier},
		statePath,
	)

	// Execute the full cycle.
	err := engine.Execute(context.Background(), e2eIssue())
	if err != nil {
		t.Fatalf("E2E happy path failed: %v", err)
	}

	// ── Verify state file ──
	state := verifyStateFile(t, statePath)

	if len(state.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(state.Tasks))
	}
	task := state.Tasks[0]

	// Task should be completed.
	if task.Status != PhaseCompleted {
		t.Fatalf("expected status %q, got %q", PhaseCompleted, task.Status)
	}

	// PR should be created.
	if task.PR == nil {
		t.Fatal("expected PR to be set")
	}
	if task.PR.URL != "https://github.com/test/repo/pull/1" {
		t.Errorf("unexpected PR URL: %s", task.PR.URL)
	}

	// Should have exactly 1 attempt (no retries needed).
	if len(task.Attempts) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(task.Attempts))
	}
	if task.Attempts[0].Status != "passed" {
		t.Errorf("expected attempt status 'passed', got %q", task.Attempts[0].Status)
	}

	// Files changed should be recorded.
	if len(task.Attempts[0].FilesChanged) != 2 {
		t.Errorf("expected 2 files changed, got %d", len(task.Attempts[0].FilesChanged))
	}

	// Deploy result should be recorded.
	if task.Attempts[0].Deploy == nil {
		t.Error("expected deploy result in attempt")
	} else if task.Attempts[0].Deploy.Status != "success" {
		t.Errorf("expected deploy status 'success', got %q", task.Attempts[0].Deploy.Status)
	}

	// Test results should be recorded.
	if len(task.Attempts[0].Tests) != 1 {
		t.Errorf("expected 1 test result, got %d", len(task.Attempts[0].Tests))
	} else if !task.Attempts[0].Tests[0].Passed {
		t.Error("expected test to be passed")
	}

	// ── Verify adapter call counts ──
	if aiAnalyzeCalls != 1 {
		t.Errorf("expected 1 AI analyze call, got %d", aiAnalyzeCalls)
	}
	if aiGenerateCalls != 1 {
		t.Errorf("expected 1 AI generate call, got %d", aiGenerateCalls)
	}
	if gitMock.createBranchCalls != 1 {
		t.Errorf("expected 1 createBranch call, got %d", gitMock.createBranchCalls)
	}
	if gitMock.commitAndPushCalls != 1 {
		t.Errorf("expected 1 commitAndPush call, got %d", gitMock.commitAndPushCalls)
	}
	if gitMock.createPRCalls != 1 {
		t.Errorf("expected 1 createPR call, got %d", gitMock.createPRCalls)
	}
	if deployMock.deployCalls != 1 {
		t.Errorf("expected 1 deploy call, got %d", deployMock.deployCalls)
	}
	if deployMock.rollbackCalls != 0 {
		t.Errorf("expected 0 rollback calls, got %d", deployMock.rollbackCalls)
	}

	// ── Verify notifications were sent for each phase ──
	// Expected phases: queued, planning, coding, committing, deploying, testing, reporting, completed
	expectedPhaseCount := 8
	if len(notifier.messages) < expectedPhaseCount {
		t.Errorf("expected at least %d notifications, got %d: %v",
			expectedPhaseCount, len(notifier.messages), notifier.messages)
	}

	// Branch name should follow convention.
	if task.Branch != "rig/issue-100" {
		t.Errorf("expected branch 'rig/issue-100', got %q", task.Branch)
	}

	// Issue should be stored correctly.
	if task.Issue.ID != "100" {
		t.Errorf("expected issue ID '100', got %q", task.Issue.ID)
	}
	if task.Issue.Title != "Add user authentication endpoint" {
		t.Errorf("expected issue title mismatch, got %q", task.Issue.Title)
	}

	// CompletedAt should be set.
	if task.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}
}

// ─── TestE2ERetryPath ──────────────────────────────────────────────
// test FAIL → AI analyzeFailure → fix → commit → deploy → test PASS → PR → completed

func TestE2ERetryPath(t *testing.T) {
	cfg := e2eConfig()
	cfg.AI.MaxRetry = 3
	statePath := filepath.Join(t.TempDir(), "state.json")

	aiFailureCalls := 0

	aiMock := &mockAI{
		analyzeFunc: func(ctx context.Context, issue *AIIssue, projectCtx string) (*AIPlan, error) {
			return &AIPlan{
				Summary: "Add validation to input handler",
				Steps:   []string{"Add input validation", "Return proper errors"},
			}, nil
		},
		generateFunc: func(ctx context.Context, plan *AIPlan, repoFiles map[string]string) ([]AIFileChange, error) {
			return []AIFileChange{
				{Path: "handler.go", Content: "package main\n// initial code without validation", Action: "modify"},
			}, nil
		},
		failureFunc: func(ctx context.Context, logs string, currentCode map[string]string) ([]AIFileChange, error) {
			aiFailureCalls++
			// AI analyzes the test failure and produces a fix.
			return []AIFileChange{
				{Path: "handler.go", Content: "package main\n// fixed: added input validation", Action: "modify"},
			}, nil
		},
	}

	gitMock := &mockGit{}
	deployMock := &mockDeploy{deploySuccess: true}

	// First test run: FAIL. Second test run (after retry): PASS.
	testRunner := &mockTestRunner{
		results: []*TestResult{
			{Name: "unit-test", Type: "command", Passed: false, Output: "FAIL: TestValidation - missing input check", Duration: time.Second},
			{Name: "unit-test", Type: "command", Passed: true, Output: "PASS: all tests passed", Duration: time.Second},
		},
	}

	notifier := &mockNotifier{}

	engine := NewEngine(cfg, gitMock, aiMock, deployMock,
		[]TestRunnerIface{testRunner},
		[]NotifierIface{notifier},
		statePath,
	)

	err := engine.Execute(context.Background(), e2eIssue())
	if err != nil {
		t.Fatalf("E2E retry path failed: %v", err)
	}

	// ── Verify state ──
	state := verifyStateFile(t, statePath)
	if len(state.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(state.Tasks))
	}
	task := state.Tasks[0]

	// Final status should be completed.
	if task.Status != PhaseCompleted {
		t.Fatalf("expected status %q, got %q", PhaseCompleted, task.Status)
	}

	// Should have 2 attempts: first failed, second passed.
	if len(task.Attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(task.Attempts))
	}
	if task.Attempts[0].Status != "failed" {
		t.Errorf("expected 1st attempt status 'failed', got %q", task.Attempts[0].Status)
	}
	if task.Attempts[0].FailReason != ReasonTest {
		t.Errorf("expected 1st attempt fail reason %q, got %q", ReasonTest, task.Attempts[0].FailReason)
	}
	if task.Attempts[1].Status != "passed" {
		t.Errorf("expected 2nd attempt status 'passed', got %q", task.Attempts[1].Status)
	}

	// AI AnalyzeFailure should have been called once (for the retry).
	if aiFailureCalls != 1 {
		t.Errorf("expected 1 AI failure analysis call, got %d", aiFailureCalls)
	}

	// PR should have been created.
	if task.PR == nil {
		t.Fatal("expected PR to be set after retry success")
	}

	// Git should have 2 commits: initial + retry fix.
	// createBranch is called once initially, and once in retry.
	if gitMock.commitAndPushCalls != 2 {
		t.Errorf("expected 2 commitAndPush calls (initial + retry), got %d", gitMock.commitAndPushCalls)
	}

	// Deploy should have been called twice: initial + retry.
	if deployMock.deployCalls != 2 {
		t.Errorf("expected 2 deploy calls (initial + retry), got %d", deployMock.deployCalls)
	}

	// No rollback should have occurred.
	if deployMock.rollbackCalls != 0 {
		t.Errorf("expected 0 rollback calls, got %d", deployMock.rollbackCalls)
	}
}

// ─── TestE2EMaxRetry ────────────────────────────────────────────────
// All attempts fail → max retry exceeded → rollback → failed

func TestE2EMaxRetry(t *testing.T) {
	cfg := e2eConfig()
	cfg.AI.MaxRetry = 2
	cfg.Deploy.Rollback.Enabled = true
	cfg.Deploy.Rollback.Method = "custom"
	cfg.Deploy.Rollback.Config = config.DeployMethodConfig{
		Commands: []config.CustomCommand{
			{Name: "rollback", Run: "echo rolling back", Workdir: ".", Transport: config.TransportConfig{Type: "local"}},
		},
	}
	statePath := filepath.Join(t.TempDir(), "state.json")

	aiFailureCalls := 0

	aiMock := &mockAI{
		analyzeFunc: func(ctx context.Context, issue *AIIssue, projectCtx string) (*AIPlan, error) {
			return &AIPlan{
				Summary: "Refactor database layer",
				Steps:   []string{"Update queries", "Add connection pooling"},
			}, nil
		},
		generateFunc: func(ctx context.Context, plan *AIPlan, repoFiles map[string]string) ([]AIFileChange, error) {
			return []AIFileChange{
				{Path: "db/pool.go", Content: "package db\n// pool impl", Action: "create"},
			}, nil
		},
		failureFunc: func(ctx context.Context, logs string, currentCode map[string]string) ([]AIFileChange, error) {
			aiFailureCalls++
			// AI tries to fix but the fix still doesn't pass.
			return []AIFileChange{
				{Path: "db/pool.go", Content: fmt.Sprintf("package db\n// fix attempt %d", aiFailureCalls), Action: "modify"},
			}, nil
		},
	}

	gitMock := &mockGit{}
	deployMock := &mockDeploy{deploySuccess: true}

	// All test runs fail — 1 initial + 2 retries = 3 total test executions.
	testRunner := &mockTestRunner{
		results: []*TestResult{
			{Name: "integration-test", Type: "command", Passed: false, Output: "FAIL: connection timeout", Duration: 2 * time.Second},
			{Name: "integration-test", Type: "command", Passed: false, Output: "FAIL: pool exhausted", Duration: 2 * time.Second},
			{Name: "integration-test", Type: "command", Passed: false, Output: "FAIL: still broken", Duration: 2 * time.Second},
		},
	}

	notifier := &mockNotifier{}

	engine := NewEngine(cfg, gitMock, aiMock, deployMock,
		[]TestRunnerIface{testRunner},
		[]NotifierIface{notifier},
		statePath,
	)

	err := engine.Execute(context.Background(), e2eIssue())
	if err == nil {
		t.Fatal("expected error after max retries, got nil")
	}

	// ── Verify state ──
	state := verifyStateFile(t, statePath)
	if len(state.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(state.Tasks))
	}
	task := state.Tasks[0]

	// Task should be in rollback state (rollback is enabled).
	if task.Status != PhaseRollback {
		t.Fatalf("expected status %q, got %q", PhaseRollback, task.Status)
	}

	// Should have 3 attempts: 1 initial + 2 retries, all failed.
	if len(task.Attempts) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(task.Attempts))
	}
	for i, attempt := range task.Attempts {
		if attempt.Status != "failed" {
			t.Errorf("expected attempt[%d] status 'failed', got %q", i, attempt.Status)
		}
	}

	// AI AnalyzeFailure should have been called max_retry times.
	if aiFailureCalls != 2 {
		t.Errorf("expected 2 AI failure analysis calls (max_retry=2), got %d", aiFailureCalls)
	}

	// Rollback should have been called once.
	if deployMock.rollbackCalls != 1 {
		t.Errorf("expected 1 rollback call, got %d", deployMock.rollbackCalls)
	}

	// No PR should have been created.
	if task.PR != nil {
		t.Error("expected no PR to be created after max retry failure")
	}

	// Deploy should have been called 3 times: initial + 2 retries.
	if deployMock.deployCalls != 3 {
		t.Errorf("expected 3 deploy calls, got %d", deployMock.deployCalls)
	}

	// CompletedAt should be set (terminal state).
	if task.CompletedAt == nil {
		t.Error("expected CompletedAt to be set for terminal state")
	}
}

// ─── TestE2EConfigInvalid ──────────────────────────────────────────
// Config validation fails before execution starts.

func TestE2EConfigInvalid(t *testing.T) {
	// Test that an invalid config produces validation errors.
	// This uses config.Validate directly since the engine expects a valid config.
	invalidConfigs := []struct {
		name    string
		cfg     *config.Config
		wantErr string
	}{
		{
			name: "missing project name",
			cfg: &config.Config{
				Project: config.ProjectConfig{Name: ""},
				Source:  config.SourceConfig{Platform: "github", Repo: "org/repo"},
				AI:      config.AIConfig{Provider: "anthropic", Model: "test"},
				Deploy:  config.DeployConfig{Method: "custom", Config: config.DeployMethodConfig{Commands: []config.CustomCommand{{Name: "build", Run: "echo ok"}}}},
			},
			wantErr: "project.name is required",
		},
		{
			name: "invalid platform",
			cfg: &config.Config{
				Project: config.ProjectConfig{Name: "test"},
				Source:  config.SourceConfig{Platform: "invalid-platform", Repo: "org/repo"},
				AI:      config.AIConfig{Provider: "anthropic", Model: "test"},
				Deploy:  config.DeployConfig{Method: "custom", Config: config.DeployMethodConfig{Commands: []config.CustomCommand{{Name: "build", Run: "echo ok"}}}},
			},
			wantErr: "source.platform 'invalid-platform' is invalid",
		},
		{
			name: "missing AI provider",
			cfg: &config.Config{
				Project: config.ProjectConfig{Name: "test"},
				Source:  config.SourceConfig{Platform: "github", Repo: "org/repo"},
				AI:      config.AIConfig{Provider: "", Model: "test"},
				Deploy:  config.DeployConfig{Method: "custom", Config: config.DeployMethodConfig{Commands: []config.CustomCommand{{Name: "build", Run: "echo ok"}}}},
			},
			wantErr: "ai.provider is required",
		},
		{
			name: "missing deploy method",
			cfg: &config.Config{
				Project: config.ProjectConfig{Name: "test"},
				Source:  config.SourceConfig{Platform: "github", Repo: "org/repo"},
				AI:      config.AIConfig{Provider: "anthropic", Model: "test"},
				Deploy:  config.DeployConfig{Method: ""},
			},
			wantErr: "deploy.method is required",
		},
		{
			name: "max retry out of range",
			cfg: &config.Config{
				Project: config.ProjectConfig{Name: "test"},
				Source:  config.SourceConfig{Platform: "github", Repo: "org/repo"},
				AI:      config.AIConfig{Provider: "anthropic", Model: "test", MaxRetry: 99},
				Deploy:  config.DeployConfig{Method: "custom", Config: config.DeployMethodConfig{Commands: []config.CustomCommand{{Name: "build", Run: "echo ok"}}}},
			},
			wantErr: "ai.max_retry must be between 1 and 10",
		},
		{
			name: "custom deploy without commands",
			cfg: &config.Config{
				Project: config.ProjectConfig{Name: "test"},
				Source:  config.SourceConfig{Platform: "github", Repo: "org/repo"},
				AI:      config.AIConfig{Provider: "anthropic", Model: "test"},
				Deploy:  config.DeployConfig{Method: "custom", Config: config.DeployMethodConfig{Commands: nil}},
			},
			wantErr: "requires at least one command",
		},
	}

	for _, tt := range invalidConfigs {
		t.Run(tt.name, func(t *testing.T) {
			err := config.Validate(tt.cfg)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if tt.wantErr != "" {
				errMsg := err.Error()
				found := false
				// Check if the error contains the expected substring.
				if len(errMsg) >= len(tt.wantErr) {
					for i := 0; i <= len(errMsg)-len(tt.wantErr); i++ {
						if errMsg[i:i+len(tt.wantErr)] == tt.wantErr {
							found = true
							break
						}
					}
				}
				if !found {
					t.Errorf("expected error to contain %q, got: %v", tt.wantErr, err)
				}
			}
		})
	}

	// Also verify that an engine with bad config would fail on validation
	// if LoadConfig were used (e.g., from a fixture file).
	_, err := config.LoadConfig(filepath.Join("testdata", "e2e", "invalid_config.yaml"))
	if err == nil {
		t.Fatal("expected LoadConfig to fail on invalid config")
	}
}

// ─── TestE2EStateTransitions ────────────────────────────────────────
// Verify that phase transitions are recorded correctly.

func TestE2EStateTransitions(t *testing.T) {
	cfg := e2eConfig()
	statePath := filepath.Join(t.TempDir(), "state.json")

	// Track transitions via notifications.
	notifier := &mockNotifier{}
	aiMock := &mockAI{}
	gitMock := &mockGit{}
	deployMock := &mockDeploy{deploySuccess: true}
	testRunner := &mockTestRunner{
		results: []*TestResult{
			{Name: "test", Type: "command", Passed: true, Duration: time.Second},
		},
	}

	engine := NewEngine(cfg, gitMock, aiMock, deployMock,
		[]TestRunnerIface{testRunner},
		[]NotifierIface{notifier},
		statePath,
	)

	err := engine.Execute(context.Background(), e2eIssue())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify all expected phase transitions happened via notifications.
	expectedPhases := []TaskPhase{
		PhaseQueued,
		PhasePlanning,
		PhaseCoding,
		PhaseCommitting,
		PhaseDeploying,
		PhaseTesting,
		PhaseReporting,
		PhaseCompleted,
	}

	if len(notifier.messages) != len(expectedPhases) {
		t.Fatalf("expected %d notifications, got %d:\n%v",
			len(expectedPhases), len(notifier.messages), notifier.messages)
	}

	for i, phase := range expectedPhases {
		msg := notifier.messages[i]
		phaseStr := string(phase)
		found := false
		for j := 0; j <= len(msg)-len(phaseStr); j++ {
			if msg[j:j+len(phaseStr)] == phaseStr {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("notification[%d] should contain phase %q, got: %q", i, phase, msg)
		}
	}
}

// ─── TestE2EMultipleTestRunners ─────────────────────────────────────
// Verify behavior with multiple test runners.

func TestE2EMultipleTestRunners(t *testing.T) {
	cfg := e2eConfig()
	statePath := filepath.Join(t.TempDir(), "state.json")

	aiMock := &mockAI{}
	gitMock := &mockGit{}
	deployMock := &mockDeploy{deploySuccess: true}
	notifier := &mockNotifier{}

	// Two test runners, both pass.
	runner1 := &mockTestRunner{
		results: []*TestResult{
			{Name: "unit-test", Type: "command", Passed: true, Output: "PASS", Duration: time.Second},
		},
	}
	runner2 := &mockTestRunner{
		results: []*TestResult{
			{Name: "integration-test", Type: "command", Passed: true, Output: "PASS", Duration: 2 * time.Second},
		},
	}

	engine := NewEngine(cfg, gitMock, aiMock, deployMock,
		[]TestRunnerIface{runner1, runner2},
		[]NotifierIface{notifier},
		statePath,
	)

	err := engine.Execute(context.Background(), e2eIssue())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state := verifyStateFile(t, statePath)
	task := state.Tasks[0]

	if task.Status != PhaseCompleted {
		t.Fatalf("expected completed, got %s", task.Status)
	}

	// Should have test results from both runners.
	if len(task.Attempts[0].Tests) != 2 {
		t.Fatalf("expected 2 test results, got %d", len(task.Attempts[0].Tests))
	}
	if task.Attempts[0].Tests[0].Name != "unit-test" {
		t.Errorf("expected test[0] name 'unit-test', got %q", task.Attempts[0].Tests[0].Name)
	}
	if task.Attempts[0].Tests[1].Name != "integration-test" {
		t.Errorf("expected test[1] name 'integration-test', got %q", task.Attempts[0].Tests[1].Name)
	}
}

// ─── TestE2EDryRun ─────────────────────────────────────────────────
// Verify dry-run mode doesn't execute or persist.

func TestE2EDryRun(t *testing.T) {
	cfg := e2eConfig()
	statePath := filepath.Join(t.TempDir(), "state.json")

	aiMock := &mockAI{}
	gitMock := &mockGit{}
	deployMock := &mockDeploy{deploySuccess: true}
	notifier := &mockNotifier{}

	engine := NewEngine(cfg, gitMock, aiMock, deployMock,
		[]TestRunnerIface{&mockTestRunner{results: []*TestResult{{Passed: true, Duration: time.Second}}}},
		[]NotifierIface{notifier},
		statePath,
	)
	engine.SetDryRun(true)

	err := engine.Execute(context.Background(), e2eIssue())
	if err != nil {
		t.Fatalf("dry-run should succeed, got: %v", err)
	}

	// State file should NOT be created.
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatal("state file should not exist in dry-run mode")
	}

	// No adapter calls should have been made.
	if gitMock.createBranchCalls != 0 {
		t.Errorf("expected 0 git calls in dry-run, got %d", gitMock.createBranchCalls)
	}
	if deployMock.deployCalls != 0 {
		t.Errorf("expected 0 deploy calls in dry-run, got %d", deployMock.deployCalls)
	}

	// Only queued notification should have been sent.
	if len(notifier.messages) != 1 {
		t.Errorf("expected 1 notification (queued only) in dry-run, got %d", len(notifier.messages))
	}
}
