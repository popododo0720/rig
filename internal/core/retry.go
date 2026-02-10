package core

import (
	"context"
	"fmt"
	"log"
)

// retryLoop implements the self-correction cycle:
// test fail → AI AnalyzeFailure → GenerateCode → redeploy → retest.
// It returns nil when tests pass, or an error when max retries are exceeded.
func retryLoop(
	ctx context.Context,
	e *Engine,
	task *Task,
	attempt *Attempt,
	vars map[string]string,
	testResults []TestResult,
	changes []AIFileChange,
	maxRetry int,
) error {
	retryCount := 0

	for {
		retryCount++
		if retryCount > maxRetry {
			return fmt.Errorf("max retry count (%d) exceeded", maxRetry)
		}

		log.Printf("[engine] retry %d/%d for task %s", retryCount, maxRetry, task.ID)

		// Collect test failure logs.
		failureLogs := collectTestOutput(testResults)

		// Build current code map from changes.
		currentCode := make(map[string]string, len(changes))
		for _, c := range changes {
			currentCode[c.Path] = c.Content
		}

		// Transition back to coding for the fix.
		if err := Transition(task, PhaseCoding); err != nil {
			return fmt.Errorf("transition to coding for retry: %w", err)
		}
		e.notifyPhase(ctx, task, PhaseCoding)

		// AI analyzes the failure and produces fixes.
		fixChanges, err := e.ai.AnalyzeFailure(ctx, failureLogs, currentCode)
		if err != nil {
			return fmt.Errorf("analyze failure: %w", err)
		}

		// Start a new attempt for this retry.
		newAttemptNum := len(task.Attempts) + 1
		retryAttempt := newAttempt(newAttemptNum)
		retryAttempt.Plan = fmt.Sprintf("Retry #%d: fix based on test failures", retryCount)

		filesChanged := make([]string, len(fixChanges))
		for i, c := range fixChanges {
			filesChanged[i] = c.Path
		}
		retryAttempt.FilesChanged = filesChanged

		// Transition to committing.
		if err := Transition(task, PhaseCommitting); err != nil {
			completeAttempt(&retryAttempt, "failed", ReasonGit)
			task.Attempts = append(task.Attempts, retryAttempt)
			return fmt.Errorf("transition to committing for retry: %w", err)
		}
		e.notifyPhase(ctx, task, PhaseCommitting)

		// Commit and push fixes.
		_, err = stepCommit(ctx, e.git, task.Branch, fixChanges, task.Issue.Title)
		if err != nil {
			completeAttempt(&retryAttempt, "failed", ReasonGit)
			task.Attempts = append(task.Attempts, retryAttempt)
			return fmt.Errorf("commit retry changes: %w", err)
		}

		// Skip approval (auto-approve in Phase 1).

		// Transition to deploying.
		if err := Transition(task, PhaseDeploying); err != nil {
			completeAttempt(&retryAttempt, "failed", ReasonDeploy)
			task.Attempts = append(task.Attempts, retryAttempt)
			return fmt.Errorf("transition to deploying for retry: %w", err)
		}
		e.notifyPhase(ctx, task, PhaseDeploying)

		deployResult, err := stepDeploy(ctx, e.deploy, vars)
		if err != nil {
			completeAttempt(&retryAttempt, "failed", ReasonDeploy)
			task.Attempts = append(task.Attempts, retryAttempt)
			return fmt.Errorf("deploy retry: %w", err)
		}
		retryAttempt.Deploy = deployResult

		if deployResult.Status != "success" {
			completeAttempt(&retryAttempt, "failed", ReasonDeploy)
			task.Attempts = append(task.Attempts, retryAttempt)
			return fmt.Errorf("deploy failed during retry")
		}

		// Transition to testing.
		if err := Transition(task, PhaseTesting); err != nil {
			completeAttempt(&retryAttempt, "failed", ReasonTest)
			task.Attempts = append(task.Attempts, retryAttempt)
			return fmt.Errorf("transition to testing for retry: %w", err)
		}
		e.notifyPhase(ctx, task, PhaseTesting)

		results, allPassed := stepTest(ctx, e.testRunners, vars)
		retryAttempt.Tests = results

		if allPassed {
			completeAttempt(&retryAttempt, "passed", "")
			task.Attempts = append(task.Attempts, retryAttempt)
			log.Printf("[engine] retry %d succeeded for task %s", retryCount, task.ID)
			return nil
		}

		// Update for next iteration.
		completeAttempt(&retryAttempt, "failed", ReasonTest)
		task.Attempts = append(task.Attempts, retryAttempt)
		testResults = results
		changes = fixChanges
	}
}
