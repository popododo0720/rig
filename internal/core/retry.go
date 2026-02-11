package core

import (
	"context"
	"errors"
	"fmt"
	"log"
)

// retryLoop implements the self-correction cycle:
// test fail -> AI AnalyzeFailure -> GenerateCode -> redeploy -> retest.
// It returns nil when tests pass, or an error when max retries are exceeded.
func retryLoop(
	ctx context.Context,
	e *Engine,
	task *Task,
	vars map[string]string,
	testResults []TestResult,
	changes []AIFileChange,
	maxRetry int,
) error {
	retryCount := 0

	for {
		retryCount++
		if maxRetry > 0 && retryCount > maxRetry {
			return fmt.Errorf("max retry count (%d) exceeded", maxRetry)
		}

		if maxRetry > 0 {
			log.Printf("[engine] retry %d/%d for task %s", retryCount, maxRetry, task.ID)
		} else {
			log.Printf("[engine] retry %d (unlimited) for task %s", retryCount, task.ID)
		}

		failureLogs := collectTestOutput(testResults)

		currentCode := make(map[string]string, len(changes))
		for _, c := range changes {
			currentCode[c.Path] = c.Content
		}

		if err := Transition(task, PhaseCoding); err != nil {
			return fmt.Errorf("transition to coding for retry: %w", err)
		}
		e.notifyPhase(ctx, task, PhaseCoding)
		task.AddPipelineStep(PhaseCoding, "running")

		fixChanges, err := e.ai.AnalyzeFailure(ctx, failureLogs, currentCode)
		if err != nil {
			task.CompletePipelineStep(PhaseCoding, "failed", "", err.Error())
			return fmt.Errorf("analyze failure: %w", err)
		}
		task.CompletePipelineStep(PhaseCoding, "success", fmt.Sprintf("generated %d retry file changes", len(fixChanges)), "")

		newAttemptNum := len(task.Attempts) + 1
		retryAttempt := newAttempt(newAttemptNum)
		retryAttempt.Plan = fmt.Sprintf("Retry #%d: fix based on test failures", retryCount)

		filesChanged := make([]string, len(fixChanges))
		for i, c := range fixChanges {
			filesChanged[i] = c.Path
		}
		retryAttempt.FilesChanged = filesChanged

		if err := Transition(task, PhaseCommitting); err != nil {
			completeAttempt(&retryAttempt, "failed", ReasonGit)
			task.Attempts = append(task.Attempts, retryAttempt)
			return fmt.Errorf("transition to committing for retry: %w", err)
		}
		e.notifyPhase(ctx, task, PhaseCommitting)
		task.AddPipelineStep(PhaseCommitting, "running")

		_, err = stepCommit(ctx, e.git, task.Branch, fixChanges, task.Issue.Title)
		if err != nil {
			task.CompletePipelineStep(PhaseCommitting, "failed", "", err.Error())
			completeAttempt(&retryAttempt, "failed", ReasonGit)
			task.Attempts = append(task.Attempts, retryAttempt)
			return fmt.Errorf("commit retry changes: %w", err)
		}
		task.CompletePipelineStep(PhaseCommitting, "success", "retry changes committed", "")

		task.AddPipelineStep(PhaseApproval, "running")
		task.CompletePipelineStep(PhaseApproval, "skipped", "auto approval step skipped", "")

		if err := Transition(task, PhaseDeploying); err != nil {
			completeAttempt(&retryAttempt, "failed", ReasonDeploy)
			task.Attempts = append(task.Attempts, retryAttempt)
			return fmt.Errorf("transition to deploying for retry: %w", err)
		}
		e.notifyPhase(ctx, task, PhaseDeploying)
		task.AddPipelineStep(PhaseDeploying, "running")

		deployResult, err := stepDeploy(ctx, e.deploy, vars)
		if err != nil {
			task.CompletePipelineStep(PhaseDeploying, "failed", "", err.Error())
			completeAttempt(&retryAttempt, "failed", ReasonDeploy)
			task.Attempts = append(task.Attempts, retryAttempt)
			return fmt.Errorf("deploy retry: %w", err)
		}
		retryAttempt.Deploy = deployResult

		if deployResult.Status != "success" {
			task.CompletePipelineStep(PhaseDeploying, "failed", deployResult.Output, "deploy failed during retry")
			completeAttempt(&retryAttempt, "failed", ReasonDeploy)
			task.Attempts = append(task.Attempts, retryAttempt)

			err = e.handleDeployFailure(ctx, task, deployResult.Output)
			if err != nil {
				if errors.Is(err, ErrAwaitingApproval) {
					return ErrAwaitingApproval
				}
				return fmt.Errorf("deploy failed during retry: %w", err)
			}

			if err := Transition(task, PhaseDeploying); err != nil {
				return fmt.Errorf("transition to deploying after auto fix: %w", err)
			}
			e.notifyPhase(ctx, task, PhaseDeploying)
			task.AddPipelineStep(PhaseDeploying, "running")

			deployResult, err = stepDeploy(ctx, e.deploy, vars)
			if err != nil {
				task.CompletePipelineStep(PhaseDeploying, "failed", "", err.Error())
				return fmt.Errorf("deploy retry after auto fix: %w", err)
			}
			retryAttempt.Deploy = deployResult
			if deployResult.Status != "success" {
				task.CompletePipelineStep(PhaseDeploying, "failed", deployResult.Output, "deploy failed after auto-apply")
				return fmt.Errorf("deploy failed during retry after auto-apply")
			}
		}
		task.CompletePipelineStep(PhaseDeploying, "success", deployResult.Output, "")

		if err := Transition(task, PhaseTesting); err != nil {
			completeAttempt(&retryAttempt, "failed", ReasonTest)
			task.Attempts = append(task.Attempts, retryAttempt)
			return fmt.Errorf("transition to testing for retry: %w", err)
		}
		e.notifyPhase(ctx, task, PhaseTesting)
		task.AddPipelineStep(PhaseTesting, "running")

		results, allPassed := stepTest(ctx, e.testRunners, vars)
		retryAttempt.Tests = results

		if allPassed {
			task.CompletePipelineStep(PhaseTesting, "success", "all tests passed", "")
			completeAttempt(&retryAttempt, "passed", "")
			task.Attempts = append(task.Attempts, retryAttempt)
			log.Printf("[engine] retry %d succeeded for task %s", retryCount, task.ID)
			return nil
		}

		task.CompletePipelineStep(PhaseTesting, "failed", collectTestOutput(results), "test failures detected")
		completeAttempt(&retryAttempt, "failed", ReasonTest)
		task.Attempts = append(task.Attempts, retryAttempt)
		testResults = results
		changes = fixChanges
	}
}
