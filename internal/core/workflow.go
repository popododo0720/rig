package core

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// --- Adapter interfaces defined in core to avoid import cycles ---

// AIIssue is a minimal issue for AI analysis.
type AIIssue struct {
	Title string
	Body  string
	URL   string
}

// AIPlan holds the AI-generated plan for resolving an issue.
type AIPlan struct {
	Summary string
	Steps   []string
}

// AIFileChange represents a single file modification from AI.
type AIFileChange struct {
	Path    string
	Content string
	Action  string // "create", "modify", or "delete"
}

// AIProposedFix is the AI's structured response for deploy/infra failures.
type AIProposedFix struct {
	Summary string           `json:"summary"`
	Reason  string           `json:"reason"`
	Changes []AIProposedFile `json:"changes"`
}

// AIProposedFile is a single file change within a proposed fix.
type AIProposedFile struct {
	Path    string `json:"path"`
	Action  string `json:"action"` // create | modify | delete
	Reason  string `json:"reason"` // explanation for this specific change
	Content string `json:"content"`
}

// AIAdapter defines the interface for AI-assisted code generation.
type AIAdapter interface {
	AnalyzeIssue(ctx context.Context, issue *AIIssue, projectContext string) (*AIPlan, error)
	GenerateCode(ctx context.Context, plan *AIPlan, repoFiles map[string]string) ([]AIFileChange, error)
	AnalyzeFailure(ctx context.Context, logs string, currentCode map[string]string) ([]AIFileChange, error)
	AnalyzeDeployFailure(ctx context.Context, deployLogs string, infraFiles map[string]string) (*AIProposedFix, error)
}

// GitFileChange represents a file modification to be committed.
type GitFileChange struct {
	Path    string
	Content string
	Action  string
}

// GitPullRequest represents a created pull request.
type GitPullRequest struct {
	Number int
	URL    string
	Title  string
}

// GitAdapter defines the interface for source code management operations.
type GitAdapter interface {
	CreateBranch(ctx context.Context, branchName string) error
	CommitAndPush(ctx context.Context, changes []GitFileChange, message string) error
	CreatePR(ctx context.Context, base, head, title, body string) (*GitPullRequest, error)
	CloneOrPull(ctx context.Context, owner, repo, token string) error
	Cleanup() error
	CleanupBranch(ctx context.Context, branchName string)
}

// AdapterDeployResult holds the outcome of a deploy operation.
type AdapterDeployResult struct {
	Success  bool
	Output   string
	Duration time.Duration
}

// DeployAdapterIface defines the interface for deploy operations.
type DeployAdapterIface interface {
	Validate() error
	Deploy(ctx context.Context, vars map[string]string) (*AdapterDeployResult, error)
	Rollback(ctx context.Context) error
}

// TestRunnerIface defines the interface for running tests.
type TestRunnerIface interface {
	Run(ctx context.Context, vars map[string]string) (*TestResult, error)
}

// NotifierIface defines the interface for sending notifications.
type NotifierIface interface {
	Notify(ctx context.Context, message string) error
}

// --- Workflow step functions ---

// stepAnalyze calls AI to analyze the issue and produce a plan.
func stepAnalyze(ctx context.Context, aiAdapter AIAdapter, issue *AIIssue, projectCtx string) (*AIPlan, error) {
	plan, err := aiAdapter.AnalyzeIssue(ctx, issue, projectCtx)
	if err != nil {
		return nil, fmt.Errorf("analyze issue: %w", err)
	}
	return plan, nil
}

// stepGenerate calls AI to generate code from a plan.
func stepGenerate(ctx context.Context, aiAdapter AIAdapter, plan *AIPlan, repoFiles map[string]string) ([]AIFileChange, error) {
	changes, err := aiAdapter.GenerateCode(ctx, plan, repoFiles)
	if err != nil {
		return nil, fmt.Errorf("generate code: %w", err)
	}
	return changes, nil
}

// stepCommit creates a branch, commits, and pushes changes.
func stepCommit(ctx context.Context, gitAdapter GitAdapter, branch string, changes []AIFileChange, issueTitle string) (string, error) {
	if err := gitAdapter.CreateBranch(ctx, branch); err != nil {
		return "", fmt.Errorf("create branch: %w", err)
	}

	// Convert AI file changes to git file changes.
	gitChanges := make([]GitFileChange, len(changes))
	for i, c := range changes {
		gitChanges[i] = GitFileChange{
			Path:    c.Path,
			Content: c.Content,
			Action:  c.Action,
		}
	}

	commitMsg := fmt.Sprintf("rig: auto-fix %s", issueTitle)
	if err := gitAdapter.CommitAndPush(ctx, gitChanges, commitMsg); err != nil {
		return "", fmt.Errorf("commit and push: %w", err)
	}

	// Return a placeholder commit SHA — the real SHA would come from git.
	return "HEAD", nil
}

// stepDeploy triggers deployment with the given variables.
func stepDeploy(ctx context.Context, deployAdapter DeployAdapterIface, vars map[string]string) (*DeployResult, error) {
	result, err := deployAdapter.Deploy(ctx, vars)
	if err != nil {
		return nil, fmt.Errorf("deploy: %w", err)
	}

	status := "success"
	if !result.Success {
		status = "failed"
	}

	return &DeployResult{
		Status:   status,
		Duration: result.Duration,
		Output:   result.Output,
	}, nil
}

// stepTest runs all test runners and returns combined results.
func stepTest(ctx context.Context, runners []TestRunnerIface, vars map[string]string) ([]TestResult, bool) {
	var results []TestResult
	allPassed := true

	for _, runner := range runners {
		result, err := runner.Run(ctx, vars)
		if err != nil {
			results = append(results, TestResult{
				Name:     "unknown",
				Type:     "command",
				Passed:   false,
				Output:   fmt.Sprintf("runner error: %v", err),
				Duration: 0,
			})
			allPassed = false
			continue
		}
		results = append(results, *result)
		if !result.Passed {
			allPassed = false
		}
	}

	return results, allPassed
}

// stepCreatePR creates a pull request for the task.
func stepCreatePR(ctx context.Context, gitAdapter GitAdapter, baseBranch, branch, issueTitle string, attempt *Attempt) (*PullRequest, error) {
	body := buildPRBody(attempt)
	pr, err := gitAdapter.CreatePR(ctx, baseBranch, branch, fmt.Sprintf("rig: %s", issueTitle), body)
	if err != nil {
		return nil, fmt.Errorf("create PR: %w", err)
	}
	return &PullRequest{
		ID:  fmt.Sprintf("%d", pr.Number),
		URL: pr.URL,
	}, nil
}

// stepRollback reverses a deployment.
func stepRollback(ctx context.Context, deployAdapter DeployAdapterIface) error {
	if err := deployAdapter.Rollback(ctx); err != nil {
		return fmt.Errorf("rollback: %w", err)
	}
	return nil
}

// buildPRBody generates the PR description from attempt details.
func buildPRBody(attempt *Attempt) string {
	var b strings.Builder
	b.WriteString("## Automated Fix by Rig\n\n")

	if attempt == nil {
		b.WriteString("_No attempt details available._\n")
		return b.String()
	}

	b.WriteString(fmt.Sprintf("**Plan:** %s\n\n", attempt.Plan))

	if len(attempt.FilesChanged) > 0 {
		b.WriteString("### Files Changed\n")
		for _, f := range attempt.FilesChanged {
			b.WriteString(fmt.Sprintf("- `%s`\n", f))
		}
		b.WriteString("\n")
	}

	if len(attempt.Tests) > 0 {
		b.WriteString("### Test Results\n")
		for _, t := range attempt.Tests {
			status := "PASS"
			if !t.Passed {
				status = "FAIL"
			}
			b.WriteString(fmt.Sprintf("- %s %s (%s)\n", status, t.Name, t.Duration))
		}
	}

	return b.String()
}

// collectTestOutput gathers all test outputs into a single log string.
func collectTestOutput(results []TestResult) string {
	var parts []string
	for _, r := range results {
		status := "PASS"
		if !r.Passed {
			status = "FAIL"
		}
		parts = append(parts, fmt.Sprintf("[%s] %s:\n%s", status, r.Name, r.Output))
	}
	return strings.Join(parts, "\n\n")
}

// newAttempt creates a new Attempt struct.
func newAttempt(number int) Attempt {
	return Attempt{
		Number:    number,
		Status:    "running",
		StartedAt: time.Now().UTC(),
	}
}

// completeAttempt marks an attempt as finished.
func completeAttempt(a *Attempt, status string, reason FailReason) {
	a.Status = status
	a.FailReason = reason
	now := time.Now().UTC()
	a.CompletedAt = &now
}
