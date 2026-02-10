package core

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/rigdev/rig/internal/config"
)

// Engine orchestrates the full execution cycle: issue → code → deploy → test → PR.
type Engine struct {
	cfg         *config.Config
	git         GitAdapter
	ai          AIAdapter
	deploy      DeployAdapterIface
	testRunners []TestRunnerIface
	notifiers   []NotifierIface
	statePath   string
	dryRun      bool
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
func (e *Engine) SetDryRun(dryRun bool) {
	e.dryRun = dryRun
}

// Execute runs the 10-step execution cycle for the given issue.
func (e *Engine) Execute(ctx context.Context, issue Issue) error {
	log.Printf("[engine] starting execution for issue %s: %s", issue.ID, issue.Title)

	// Step 1: CreateTask → queued
	state, err := LoadState(e.statePath)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	task := state.CreateTask(issue)
	log.Printf("[engine] created task %s in phase %s", task.ID, task.Status)
	e.notifyPhase(ctx, task, PhaseQueued)

	if e.dryRun {
		log.Printf("[engine] dry-run mode: skipping execution for task %s", task.ID)
		return nil
	}

	// Save initial state.
	if err := SaveState(state, e.statePath); err != nil {
		return fmt.Errorf("save state: %w", err)
	}

	// Build variables map.
	vars := e.buildVars(task)

	// Step 2: AI AnalyzeIssue → planning
	if err := Transition(task, PhasePlanning); err != nil {
		return e.failTask(ctx, state, task, ReasonInfra, err)
	}
	e.notifyPhase(ctx, task, PhasePlanning)

	aiIssue := &AIIssue{
		Title: issue.Title,
		Body:  "",
		URL:   issue.URL,
	}
	projectCtx := strings.Join(e.cfg.AI.Context, "\n")
	plan, err := stepAnalyze(ctx, e.ai, aiIssue, projectCtx)
	if err != nil {
		return e.failTask(ctx, state, task, ReasonAI, err)
	}

	// Step 3: AI GenerateCode → coding
	if err := Transition(task, PhaseCoding); err != nil {
		return e.failTask(ctx, state, task, ReasonInfra, err)
	}
	e.notifyPhase(ctx, task, PhaseCoding)

	// Create first attempt.
	attempt := newAttempt(1)
	attempt.Plan = plan.Summary

	changes, err := stepGenerate(ctx, e.ai, plan, nil)
	if err != nil {
		completeAttempt(&attempt, "failed", ReasonAI)
		task.Attempts = append(task.Attempts, attempt)
		return e.failTask(ctx, state, task, ReasonAI, err)
	}

	filesChanged := make([]string, len(changes))
	for i, c := range changes {
		filesChanged[i] = c.Path
	}
	attempt.FilesChanged = filesChanged

	// Step 4: Git CreateBranch + CommitAndPush → committing
	if err := Transition(task, PhaseCommitting); err != nil {
		completeAttempt(&attempt, "failed", ReasonGit)
		task.Attempts = append(task.Attempts, attempt)
		return e.failTask(ctx, state, task, ReasonInfra, err)
	}
	e.notifyPhase(ctx, task, PhaseCommitting)

	commitSHA, err := stepCommit(ctx, e.git, task.Branch, changes, task.Issue.Title)
	if err != nil {
		completeAttempt(&attempt, "failed", ReasonGit)
		task.Attempts = append(task.Attempts, attempt)
		return e.failTask(ctx, state, task, ReasonGit, err)
	}
	vars["COMMIT_SHA"] = commitSHA

	// Step 5: Approval — auto-approve in Phase 1 (skip).

	// Step 6: Deploy.Deploy → deploying
	if err := Transition(task, PhaseDeploying); err != nil {
		completeAttempt(&attempt, "failed", ReasonDeploy)
		task.Attempts = append(task.Attempts, attempt)
		return e.failTask(ctx, state, task, ReasonInfra, err)
	}
	e.notifyPhase(ctx, task, PhaseDeploying)

	deployResult, err := stepDeploy(ctx, e.deploy, vars)
	if err != nil {
		completeAttempt(&attempt, "failed", ReasonDeploy)
		task.Attempts = append(task.Attempts, attempt)
		return e.failTask(ctx, state, task, ReasonDeploy, err)
	}
	attempt.Deploy = deployResult

	if deployResult.Status != "success" {
		completeAttempt(&attempt, "failed", ReasonDeploy)
		task.Attempts = append(task.Attempts, attempt)
		return e.failTask(ctx, state, task, ReasonDeploy, fmt.Errorf("deploy failed: %s", deployResult.Output))
	}

	// Step 7: TestRunner.Run → testing
	if err := Transition(task, PhaseTesting); err != nil {
		completeAttempt(&attempt, "failed", ReasonTest)
		task.Attempts = append(task.Attempts, attempt)
		return e.failTask(ctx, state, task, ReasonInfra, err)
	}
	e.notifyPhase(ctx, task, PhaseTesting)

	testResults, allPassed := stepTest(ctx, e.testRunners, vars)
	attempt.Tests = testResults

	if allPassed {
		// Step 8: All PASS → Git CreatePR → reporting → completed
		completeAttempt(&attempt, "passed", "")
		task.Attempts = append(task.Attempts, attempt)
		return e.completeTask(ctx, state, task)
	}

	// Step 9: Any FAIL → AI AnalyzeFailure → back to step 3 (max retry)
	completeAttempt(&attempt, "failed", ReasonTest)
	task.Attempts = append(task.Attempts, attempt)

	maxRetry := e.cfg.AI.MaxRetry
	if maxRetry <= 0 {
		maxRetry = 3
	}

	err = retryLoop(ctx, e, task, &attempt, vars, testResults, changes, maxRetry)
	if err != nil {
		// Step 10: Max retry exceeded → Rollback → failed
		log.Printf("[engine] retry loop failed: %v", err)
		return e.rollbackAndFail(ctx, state, task)
	}

	// Retry succeeded — create PR.
	return e.completeTask(ctx, state, task)
}

// completeTask transitions to reporting, creates a PR, then completes.
func (e *Engine) completeTask(ctx context.Context, state *State, task *Task) error {
	// Transition to reporting.
	if err := Transition(task, PhaseReporting); err != nil {
		return e.failTask(ctx, state, task, ReasonInfra, err)
	}
	e.notifyPhase(ctx, task, PhaseReporting)

	// Get the latest attempt for PR body.
	var lastAttempt *Attempt
	if len(task.Attempts) > 0 {
		lastAttempt = &task.Attempts[len(task.Attempts)-1]
	}

	pr, err := stepCreatePR(ctx, e.git, e.cfg.Source.BaseBranch, task.Branch, task.Issue.Title, lastAttempt)
	if err != nil {
		return e.failTask(ctx, state, task, ReasonGit, err)
	}
	task.PR = pr

	// Transition to completed.
	if err := Transition(task, PhaseCompleted); err != nil {
		return fmt.Errorf("transition to completed: %w", err)
	}
	e.notifyPhase(ctx, task, PhaseCompleted)

	log.Printf("[engine] task %s completed with PR %s", task.ID, pr.URL)

	return SaveState(state, e.statePath)
}

// rollbackAndFail rolls back deployment then marks task as failed.
func (e *Engine) rollbackAndFail(ctx context.Context, state *State, task *Task) error {
	// Transition to failed first.
	if err := Transition(task, PhaseFailed); err != nil {
		log.Printf("[engine] failed to transition to failed: %v", err)
	}
	e.notifyPhase(ctx, task, PhaseFailed)

	// Attempt rollback.
	if e.cfg.Deploy.Rollback.Enabled {
		if err := Transition(task, PhaseRollback); err != nil {
			log.Printf("[engine] failed to transition to rollback: %v", err)
		} else {
			e.notifyPhase(ctx, task, PhaseRollback)
			if err := stepRollback(ctx, e.deploy); err != nil {
				log.Printf("[engine] rollback failed: %v", err)
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
	log.Printf("[engine] task %s failed: %v (reason: %s)", task.ID, cause, reason)

	if err := Transition(task, PhaseFailed); err != nil {
		log.Printf("[engine] failed to transition to failed: %v", err)
	}
	e.notifyPhase(ctx, task, PhaseFailed)

	if err := SaveState(state, e.statePath); err != nil {
		log.Printf("[engine] failed to save state: %v", err)
	}

	return fmt.Errorf("task %s failed at %s: %w", task.ID, reason, cause)
}

// buildVars assembles the built-in variables map.
func (e *Engine) buildVars(task *Task) map[string]string {
	// Parse owner/repo from config.
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
	msg := fmt.Sprintf("[rig] Task %s → %s (issue: %s)", task.ID, phase, task.Issue.Title)
	for _, n := range e.notifiers {
		if err := n.Notify(ctx, msg); err != nil {
			log.Printf("[engine] notification failed: %v", err)
		}
	}
}
