package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rigdev/rig/internal/config"
)

var ErrAwaitingApproval = errors.New("task awaiting approval")

// defaultMaxRetry of 0 means unlimited retries (code changes retry until tests pass).
const defaultMaxRetry = 0

type deployFailureAnalysisContextKey struct{}

// LogFunc is an optional callback for per-task logging.
type LogFunc func(taskID, level, message string)

// Engine orchestrates the full execution cycle: issue -> code -> deploy -> test -> PR.
type Engine struct {
	cfg         *config.Config
	git         GitAdapter
	ai          AIAdapter
	deploy      DeployAdapterIface
	testRunners []TestRunnerIface
	notifiers   []NotifierIface
	statePath   string
	dryRun      bool
	logFn       LogFunc
}

// NewEngine creates a new Engine with all adapter dependencies injected.
func NewEngine(
	cfg *config.Config,
	git GitAdapter,
	ai AIAdapter,
	deploy DeployAdapterIface,
	testRunners []TestRunnerIface,
	notifiers []NotifierIface,
	statePath string,
) *Engine {
	return &Engine{
		cfg:         cfg,
		git:         git,
		ai:          ai,
		deploy:      deploy,
		testRunners: testRunners,
		notifiers:   notifiers,
		statePath:   statePath,
	}
}

// SetDryRun enables dry-run mode (no state mutation, no real execution).
// SetLogFunc sets an optional per-task log callback.
func (e *Engine) SetLogFunc(fn LogFunc) {
	e.logFn = fn
}

// taskLog logs a message both to stdout and to the optional log callback.
func (e *Engine) taskLog(taskID, level, msg string) {
	log.Printf("[engine] [%s] %s", level, msg)
	if e.logFn != nil {
		e.logFn(taskID, level, msg)
	}
}

func (e *Engine) SetDryRun(dryRun bool) {
	e.dryRun = dryRun
}

// Execute runs the execution cycle for the given issue.
func (e *Engine) Execute(ctx context.Context, issue Issue) error {
	log.Printf("[engine] starting execution for issue %s: %s", issue.ID, issue.Title)

	state, err := LoadState(e.statePath)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	task := state.CreateTask(issue)
	e.taskLog(task.ID, "info", fmt.Sprintf("Task created for issue #%s: %s", issue.ID, issue.Title))
	task.AddPipelineStep(PhaseQueued, "running")
	e.notifyPhase(ctx, task, PhaseQueued)
	task.CompletePipelineStep(PhaseQueued, "success", "task queued", "")

	if e.dryRun {
		log.Printf("[engine] dry-run mode: skipping execution for task %s", task.ID)
		return nil
	}

	if err := SaveState(state, e.statePath); err != nil {
		return fmt.Errorf("save state: %w", err)
	}

	vars := e.buildVars(task)

	if err := Transition(task, PhasePlanning); err != nil {
		return e.failTask(ctx, state, task, ReasonInfra, err)
	}
	task.AddPipelineStep(PhasePlanning, "running")
	e.notifyPhase(ctx, task, PhasePlanning)

	aiIssue := &AIIssue{
		Title: issue.Title,
		Body:  "",
		URL:   issue.URL,
	}
	projectCtx := strings.Join(e.cfg.AI.Context, "\n")
	e.taskLog(task.ID, "info", "Analyzing issue with AI...")
	plan, err := stepAnalyze(ctx, e.ai, aiIssue, projectCtx)
	if err != nil {
		e.taskLog(task.ID, "error", fmt.Sprintf("Planning failed: %v", err))
		task.CompletePipelineStep(PhasePlanning, "failed", "", err.Error())
		return e.failTask(ctx, state, task, ReasonAI, err)
	}
	e.taskLog(task.ID, "info", fmt.Sprintf("Plan: %s", plan.Summary))
	task.CompletePipelineStep(PhasePlanning, "success", plan.Summary, "")

	if err := Transition(task, PhaseCoding); err != nil {
		return e.failTask(ctx, state, task, ReasonInfra, err)
	}
	task.AddPipelineStep(PhaseCoding, "running")
	e.notifyPhase(ctx, task, PhaseCoding)

	attempt := newAttempt(1)
	attempt.Plan = plan.Summary

	e.taskLog(task.ID, "info", "Generating code with AI...")
	changes, err := stepGenerate(ctx, e.ai, plan, nil)
	if err != nil {
		e.taskLog(task.ID, "error", fmt.Sprintf("Code generation failed: %v", err))
		task.CompletePipelineStep(PhaseCoding, "failed", "", err.Error())
		completeAttempt(&attempt, "failed", ReasonAI)
		task.Attempts = append(task.Attempts, attempt)
		return e.failTask(ctx, state, task, ReasonAI, err)
	}
	filesChanged := make([]string, len(changes))
	for i, c := range changes {
		filesChanged[i] = c.Path
	}
	attempt.FilesChanged = filesChanged
	e.taskLog(task.ID, "info", fmt.Sprintf("Generated %d file(s): %s", len(changes), strings.Join(filesChanged, ", ")))
	task.CompletePipelineStep(PhaseCoding, "success", fmt.Sprintf("generated %d file changes", len(changes)), "")

	if err := Transition(task, PhaseCommitting); err != nil {
		completeAttempt(&attempt, "failed", ReasonGit)
		task.Attempts = append(task.Attempts, attempt)
		return e.failTask(ctx, state, task, ReasonInfra, err)
	}
	task.AddPipelineStep(PhaseCommitting, "running")
	e.notifyPhase(ctx, task, PhaseCommitting)

	// Clone or pull the repo before committing.
	e.taskLog(task.ID, "info", "Cloning repository...")
	owner, repo := parseRepo(e.cfg.Source.Repo)
	if err := e.git.CloneOrPull(ctx, owner, repo, e.cfg.Source.Token); err != nil {
		e.taskLog(task.ID, "error", fmt.Sprintf("Clone failed: %v", err))
		task.CompletePipelineStep(PhaseCommitting, "failed", "", err.Error())
		completeAttempt(&attempt, "failed", ReasonGit)
		task.Attempts = append(task.Attempts, attempt)
		return e.failTask(ctx, state, task, ReasonGit, err)
	}

	e.taskLog(task.ID, "info", fmt.Sprintf("Creating branch %s and committing...", task.Branch))
	commitSHA, err := stepCommit(ctx, e.git, task.Branch, changes, task.Issue.Title)
	if err != nil {
		e.taskLog(task.ID, "error", fmt.Sprintf("Commit failed: %v", err))
		task.CompletePipelineStep(PhaseCommitting, "failed", "", err.Error())
		completeAttempt(&attempt, "failed", ReasonGit)
		task.Attempts = append(task.Attempts, attempt)
		return e.failTask(ctx, state, task, ReasonGit, err)
	}
	e.taskLog(task.ID, "info", fmt.Sprintf("Committed: %s", commitSHA))
	task.CompletePipelineStep(PhaseCommitting, "success", "changes committed", "")
	vars["COMMIT_SHA"] = commitSHA

	task.AddPipelineStep(PhaseApproval, "running")
	task.CompletePipelineStep(PhaseApproval, "skipped", "auto approval step skipped", "")

	if err := Transition(task, PhaseDeploying); err != nil {
		completeAttempt(&attempt, "failed", ReasonDeploy)
		task.Attempts = append(task.Attempts, attempt)
		return e.failTask(ctx, state, task, ReasonInfra, err)
	}
	task.AddPipelineStep(PhaseDeploying, "running")
	e.notifyPhase(ctx, task, PhaseDeploying)

	deployResult, err := stepDeploy(ctx, e.deploy, vars)
	if err != nil {
		task.CompletePipelineStep(PhaseDeploying, "failed", "", err.Error())
		completeAttempt(&attempt, "failed", ReasonDeploy)
		task.Attempts = append(task.Attempts, attempt)
		return e.failTask(ctx, state, task, ReasonDeploy, err)
	}
	attempt.Deploy = deployResult

	if deployResult.Status != "success" {
		task.CompletePipelineStep(PhaseDeploying, "failed", deployResult.Output, "deploy status failed")

		handleErr := e.handleDeployFailure(enableDeployFailureAnalysis(ctx), task, deployResult.Output)
		if errors.Is(handleErr, ErrAwaitingApproval) {
			completeAttempt(&attempt, "failed", ReasonDeploy)
			task.Attempts = append(task.Attempts, attempt)
			if err := SaveState(state, e.statePath); err != nil {
				return fmt.Errorf("save state: %w", err)
			}
			return ErrAwaitingApproval
		}
		if handleErr != nil {
			completeAttempt(&attempt, "failed", ReasonDeploy)
			task.Attempts = append(task.Attempts, attempt)
			return e.failTask(ctx, state, task, ReasonDeploy, handleErr)
		}

		task.AddPipelineStep(PhaseDeploying, "running")
		e.notifyPhase(ctx, task, PhaseDeploying)

		deployResult, err = stepDeploy(ctx, e.deploy, vars)
		if err != nil {
			task.CompletePipelineStep(PhaseDeploying, "failed", "", err.Error())
			completeAttempt(&attempt, "failed", ReasonDeploy)
			task.Attempts = append(task.Attempts, attempt)
			return e.failTask(ctx, state, task, ReasonDeploy, err)
		}
		attempt.Deploy = deployResult

		if deployResult.Status != "success" {
			task.CompletePipelineStep(PhaseDeploying, "failed", deployResult.Output, "deploy failed after auto-apply")
			completeAttempt(&attempt, "failed", ReasonDeploy)
			task.Attempts = append(task.Attempts, attempt)
			return e.failTask(ctx, state, task, ReasonDeploy, fmt.Errorf("deploy failed after auto-apply: %s", deployResult.Output))
		}
	}
	task.CompletePipelineStep(PhaseDeploying, "success", deployResult.Output, "")

	if err := Transition(task, PhaseTesting); err != nil {
		completeAttempt(&attempt, "failed", ReasonTest)
		task.Attempts = append(task.Attempts, attempt)
		return e.failTask(ctx, state, task, ReasonInfra, err)
	}
	task.AddPipelineStep(PhaseTesting, "running")
	e.notifyPhase(ctx, task, PhaseTesting)

	testResults, allPassed := stepTest(ctx, e.testRunners, vars)
	attempt.Tests = testResults

	if allPassed {
		task.CompletePipelineStep(PhaseTesting, "success", "all tests passed", "")
		completeAttempt(&attempt, "passed", "")
		task.Attempts = append(task.Attempts, attempt)
		return e.completeTask(ctx, state, task)
	}

	task.CompletePipelineStep(PhaseTesting, "failed", collectTestOutput(testResults), "test failures detected")
	completeAttempt(&attempt, "failed", ReasonTest)
	task.Attempts = append(task.Attempts, attempt)

	maxRetry := e.cfg.AI.MaxRetry
	if maxRetry < 0 {
		maxRetry = defaultMaxRetry
	}

	err = retryLoop(ctx, e, task, vars, testResults, changes, maxRetry)
	if err != nil {
		if errors.Is(err, ErrAwaitingApproval) {
			if saveErr := SaveState(state, e.statePath); saveErr != nil {
				return fmt.Errorf("save state: %w", saveErr)
			}
			return ErrAwaitingApproval
		}
		log.Printf("[engine] retry loop failed: %v", err)
		return e.rollbackAndFail(ctx, state, task)
	}

	return e.completeTask(ctx, state, task)
}

// Resume continues a task that is currently awaiting approval.
func (e *Engine) Resume(ctx context.Context, taskID string, approved bool) error {
	state, err := LoadState(e.statePath)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	task := state.GetTaskByID(taskID)
	if task == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}
	if task.Status != PhaseAwaitingApproval {
		return fmt.Errorf("task %s is not awaiting approval", taskID)
	}

	proposal := task.GetPendingProposal()

	if !approved {
		if proposal != nil {
			now := time.Now().UTC()
			proposal.Status = ProposalRejected
			proposal.ReviewedAt = &now
		}

		task.AddPipelineStep(PhaseFailed, "running")
		if err := Transition(task, PhaseFailed); err != nil {
			task.CompletePipelineStep(PhaseFailed, "failed", "", err.Error())
			return fmt.Errorf("transition to failed: %w", err)
		}
		e.notifyPhase(ctx, task, PhaseFailed)
		task.CompletePipelineStep(PhaseFailed, "success", "approval rejected", "")

		if err := SaveState(state, e.statePath); err != nil {
			return fmt.Errorf("save state: %w", err)
		}
		return nil
	}

	attempt := newAttempt(len(task.Attempts) + 1)
	attempt.Plan = "Resume after approval"

	if proposal != nil {
		now := time.Now().UTC()
		proposal.Status = ProposalApproved
		proposal.ReviewedAt = &now
		if err := applyProposalChanges(proposal.Changes); err != nil {
			return fmt.Errorf("apply approved proposal: %w", err)
		}
		attempt.FilesChanged = proposedChangePaths(proposal.Changes)
	}

	vars := e.buildVars(task)

	if err := Transition(task, PhaseDeploying); err != nil {
		return fmt.Errorf("transition to deploying: %w", err)
	}
	task.AddPipelineStep(PhaseDeploying, "running")
	e.notifyPhase(ctx, task, PhaseDeploying)

	deployResult, err := stepDeploy(ctx, e.deploy, vars)
	if err != nil {
		task.CompletePipelineStep(PhaseDeploying, "failed", "", err.Error())
		completeAttempt(&attempt, "failed", ReasonDeploy)
		task.Attempts = append(task.Attempts, attempt)
		return e.failTask(ctx, state, task, ReasonDeploy, err)
	}
	attempt.Deploy = deployResult

	if deployResult.Status != "success" {
		task.CompletePipelineStep(PhaseDeploying, "failed", deployResult.Output, "deploy status failed")
		completeAttempt(&attempt, "failed", ReasonDeploy)
		task.Attempts = append(task.Attempts, attempt)
		return e.failTask(ctx, state, task, ReasonDeploy, fmt.Errorf("deploy failed: %s", deployResult.Output))
	}
	task.CompletePipelineStep(PhaseDeploying, "success", deployResult.Output, "")

	if err := Transition(task, PhaseTesting); err != nil {
		completeAttempt(&attempt, "failed", ReasonTest)
		task.Attempts = append(task.Attempts, attempt)
		return e.failTask(ctx, state, task, ReasonInfra, err)
	}
	task.AddPipelineStep(PhaseTesting, "running")
	e.notifyPhase(ctx, task, PhaseTesting)

	testResults, allPassed := stepTest(ctx, e.testRunners, vars)
	attempt.Tests = testResults

	if allPassed {
		task.CompletePipelineStep(PhaseTesting, "success", "all tests passed", "")
		completeAttempt(&attempt, "passed", "")
		task.Attempts = append(task.Attempts, attempt)
		return e.completeTask(ctx, state, task)
	}

	task.CompletePipelineStep(PhaseTesting, "failed", collectTestOutput(testResults), "test failures detected")
	completeAttempt(&attempt, "failed", ReasonTest)
	task.Attempts = append(task.Attempts, attempt)

	maxRetry := e.cfg.AI.MaxRetry
	if maxRetry < 0 {
		maxRetry = defaultMaxRetry
	}

	retryChanges := []AIFileChange{}
	if proposal != nil {
		retryChanges = proposedChangesToAIFileChanges(proposal.Changes)
	}

	err = retryLoop(ctx, e, task, vars, testResults, retryChanges, maxRetry)
	if err != nil {
		if errors.Is(err, ErrAwaitingApproval) {
			if saveErr := SaveState(state, e.statePath); saveErr != nil {
				return fmt.Errorf("save state: %w", saveErr)
			}
			return ErrAwaitingApproval
		}
		return e.rollbackAndFail(ctx, state, task)
	}

	return e.completeTask(ctx, state, task)
}

func (e *Engine) handleDeployFailure(ctx context.Context, task *Task, deployLogs string) error {
	if !deployFailureAnalysisEnabled(ctx) {
		return fmt.Errorf("deploy failed: %s", deployLogs)
	}

	infraFiles := loadInfraFiles(e.cfg.Deploy.InfraFiles)
	proposedFix, err := e.ai.AnalyzeDeployFailure(ctx, deployLogs, infraFiles)
	if err != nil {
		return fmt.Errorf("analyze deploy failure: %w", err)
	}
	if proposedFix == nil {
		return errors.New("analyze deploy failure returned nil fix")
	}

	changes := convertDeployFixChanges(proposedFix, infraFiles)
	task.AddProposal(ProposalDeployFix, proposedFix.Summary, proposedFix.Reason, changes)

	// Infrastructure changes always require human approval via web dashboard.
	// AI proposes the fix, but only proceeds after explicit human approval.
	log.Printf("[engine] infra fix proposed for task %s: %s (reason: %s, files: %d) — awaiting human approval",
		task.ID, proposedFix.Summary, proposedFix.Reason, len(changes))

	task.AddPipelineStep(PhaseAwaitingApproval, "running")
	if err := Transition(task, PhaseAwaitingApproval); err != nil {
		task.CompletePipelineStep(PhaseAwaitingApproval, "failed", "", err.Error())
		return fmt.Errorf("transition to awaiting approval: %w", err)
	}
	e.notifyPhase(ctx, task, PhaseAwaitingApproval)
	task.CompletePipelineStep(PhaseAwaitingApproval, "success", "deploy fix proposal waiting for approval", "")
	return ErrAwaitingApproval
}

func enableDeployFailureAnalysis(ctx context.Context) context.Context {
	return context.WithValue(ctx, deployFailureAnalysisContextKey{}, true)
}

func deployFailureAnalysisEnabled(ctx context.Context) bool {
	enabled, ok := ctx.Value(deployFailureAnalysisContextKey{}).(bool)
	return ok && enabled
}

func convertDeployFixChanges(proposedFix *AIProposedFix, beforeFiles map[string]string) []ProposedChange {
	changes := make([]ProposedChange, len(proposedFix.Changes))
	for i, c := range proposedFix.Changes {
		changes[i] = ProposedChange{
			Path:   c.Path,
			Action: c.Action,
			Reason: c.Reason,
			After:  c.Content,
		}
		if before, ok := beforeFiles[c.Path]; ok {
			changes[i].Before = before
		}
	}
	return changes
}

func applyProposalChanges(changes []ProposedChange) error {
	for _, change := range changes {
		switch change.Action {
		case "create", "modify":
			dir := filepath.Dir(change.Path)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("create directory for %s: %w", change.Path, err)
			}
			if err := os.WriteFile(change.Path, []byte(change.After), 0644); err != nil {
				return fmt.Errorf("write file %s: %w", change.Path, err)
			}
		case "delete":
			if err := os.Remove(change.Path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("delete file %s: %w", change.Path, err)
			}
		default:
			return fmt.Errorf("unsupported proposed change action %q for %s", change.Action, change.Path)
		}
	}
	return nil
}

func proposedChangePaths(changes []ProposedChange) []string {
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		paths = append(paths, change.Path)
	}
	return paths
}

func proposedChangesToAIFileChanges(changes []ProposedChange) []AIFileChange {
	out := make([]AIFileChange, 0, len(changes))
	for _, change := range changes {
		out = append(out, AIFileChange{
			Path:    change.Path,
			Content: change.After,
			Action:  change.Action,
		})
	}
	return out
}


// completeTask transitions to reporting, creates a PR, then completes.
func (e *Engine) completeTask(ctx context.Context, state *State, task *Task) error {
	task.AddPipelineStep(PhaseReporting, "running")
	if err := Transition(task, PhaseReporting); err != nil {
		task.CompletePipelineStep(PhaseReporting, "failed", "", err.Error())
		return e.failTask(ctx, state, task, ReasonInfra, err)
	}
	e.notifyPhase(ctx, task, PhaseReporting)

	var lastAttempt *Attempt
	if len(task.Attempts) > 0 {
		lastAttempt = &task.Attempts[len(task.Attempts)-1]
	}

	pr, err := stepCreatePR(ctx, e.git, e.cfg.Source.BaseBranch, task.Branch, task.Issue.Title, lastAttempt)
	if err != nil {
		task.CompletePipelineStep(PhaseReporting, "failed", "", err.Error())
		return e.failTask(ctx, state, task, ReasonGit, err)
	}
	task.PR = pr
	task.CompletePipelineStep(PhaseReporting, "success", pr.URL, "")

	task.AddPipelineStep(PhaseCompleted, "running")
	if err := Transition(task, PhaseCompleted); err != nil {
		task.CompletePipelineStep(PhaseCompleted, "failed", "", err.Error())
		return fmt.Errorf("transition to completed: %w", err)
	}
	e.notifyPhase(ctx, task, PhaseCompleted)
	task.CompletePipelineStep(PhaseCompleted, "success", "task completed", "")

	e.taskLog(task.ID, "info", fmt.Sprintf("Task completed with PR %s", pr.URL))

	if err := e.git.Cleanup(); err != nil {
		log.Printf("[engine] cleanup workspace: %v", err)
	}

	return SaveState(state, e.statePath)
}

// rollbackAndFail rolls back deployment then marks task as failed.
func (e *Engine) rollbackAndFail(ctx context.Context, state *State, task *Task) error {
	task.AddPipelineStep(PhaseFailed, "running")
	if err := Transition(task, PhaseFailed); err != nil {
		log.Printf("[engine] failed to transition to failed: %v", err)
		task.CompletePipelineStep(PhaseFailed, "failed", "", err.Error())
	} else {
		e.notifyPhase(ctx, task, PhaseFailed)
		task.CompletePipelineStep(PhaseFailed, "success", "max retries exceeded", "")
	}

	if e.cfg.Deploy.Rollback.Enabled {
		task.AddPipelineStep(PhaseRollback, "running")
		if err := Transition(task, PhaseRollback); err != nil {
			log.Printf("[engine] failed to transition to rollback: %v", err)
			task.CompletePipelineStep(PhaseRollback, "failed", "", err.Error())
		} else {
			e.notifyPhase(ctx, task, PhaseRollback)
			if err := stepRollback(ctx, e.deploy); err != nil {
				log.Printf("[engine] rollback failed: %v", err)
				task.CompletePipelineStep(PhaseRollback, "failed", "", err.Error())
			} else {
				task.CompletePipelineStep(PhaseRollback, "success", "rollback completed", "")
			}
		}
	}

	if err := SaveState(state, e.statePath); err != nil {
		log.Printf("[engine] failed to save state after rollback: %v", err)
	}

	return fmt.Errorf("task %s failed after max retries", task.ID)
}

// failTask transitions task to failed and saves state.
func (e *Engine) failTask(ctx context.Context, state *State, task *Task, reason FailReason, cause error) error {
	e.taskLog(task.ID, "error", fmt.Sprintf("Task failed: %v (reason: %s)", cause, reason))

	// Clean up remote branch if it was created during this run.
	branchName := fmt.Sprintf("rig/issue-%s", task.ID)
	e.git.CleanupBranch(ctx, branchName)

	if err := e.git.Cleanup(); err != nil {
		log.Printf("[engine] cleanup workspace: %v", err)
	}

	task.AddPipelineStep(PhaseFailed, "running")
	if err := Transition(task, PhaseFailed); err != nil {
		log.Printf("[engine] failed to transition to failed: %v", err)
		task.CompletePipelineStep(PhaseFailed, "failed", "", err.Error())
	} else {
		e.notifyPhase(ctx, task, PhaseFailed)
		task.CompletePipelineStep(PhaseFailed, "success", cause.Error(), "")
	}

	if err := SaveState(state, e.statePath); err != nil {
		log.Printf("[engine] failed to save state: %v", err)
	}

	return fmt.Errorf("task %s failed at %s: %w", task.ID, reason, cause)
}

// buildVars assembles the built-in variables map.
func (e *Engine) buildVars(task *Task) map[string]string {
	owner, repo := parseRepo(e.cfg.Source.Repo)

	return map[string]string{
		"BRANCH_NAME":  task.Branch,
		"COMMIT_SHA":   "",
		"ISSUE_ID":     task.Issue.ID,
		"ISSUE_NUMBER": task.Issue.ID,
		"ISSUE_TITLE":  task.Issue.Title,
		"REPO_OWNER":   owner,
		"REPO_NAME":    repo,
	}
}

// parseRepo splits "owner/repo" into owner and repo.
func parseRepo(fullName string) (string, string) {
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 {
		return fullName, ""
	}
	return parts[0], parts[1]
}

// notifyPhase sends a notification about a phase transition.
func (e *Engine) notifyPhase(ctx context.Context, task *Task, phase TaskPhase) {
	msg := fmt.Sprintf("[rig] Task %s -> %s (issue: %s)", task.ID, phase, task.Issue.Title)
	for _, n := range e.notifiers {
		if err := n.Notify(ctx, msg); err != nil {
			log.Printf("[engine] notification failed: %v", err)
		}
	}
}
