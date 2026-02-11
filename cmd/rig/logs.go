package main

import (
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/rigdev/rig/internal/core"
	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs <task-id>",
	Short: "Show task logs and attempt details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID := args[0]
		statePath := ".rig/state.json"
		follow, _ := cmd.Flags().GetBool("follow")

		state, err := core.LoadState(statePath)
		if err != nil {
			return fmt.Errorf("load state: %w", err)
		}

		task := state.GetTaskByID(taskID)
		if task == nil {
			return fmt.Errorf("task %q not found", taskID)
		}

		// Print task header.
		fmt.Fprintf(os.Stdout, "Task: %s\n", task.ID)
		fmt.Fprintf(os.Stdout, "Status: %s\n", task.Status)
		fmt.Fprintf(os.Stdout, "Issue: %s (%s)\n", task.Issue.Title, task.Issue.URL)
		fmt.Fprintf(os.Stdout, "Branch: %s\n", task.Branch)
		fmt.Fprintf(os.Stdout, "Created: %s\n", task.CreatedAt.Format("2006-01-02 15:04:05"))
		if task.CompletedAt != nil {
			fmt.Fprintf(os.Stdout, "Completed: %s\n", task.CompletedAt.Format("2006-01-02 15:04:05"))
		}
		if task.PR != nil {
			fmt.Fprintf(os.Stdout, "PR: %s\n", task.PR.URL)
		}
		fmt.Println()

		// Print pipeline steps.
		if len(task.Pipeline) > 0 {
			fmt.Println("Pipeline:")
			for _, step := range task.Pipeline {
				marker := "●"
				switch step.Status {
				case "success":
					marker = "✓"
				case "failed":
					marker = "✗"
				case "skipped":
					marker = "–"
				}
				ts := step.StartedAt.Format("15:04:05")
				fmt.Fprintf(os.Stdout, "  [%s] %s %s", ts, marker, step.Phase)
				if step.Output != "" {
					fmt.Fprintf(os.Stdout, " — %s", truncateOutput(step.Output, 120))
				}
				if step.Error != "" {
					fmt.Fprintf(os.Stdout, " (error: %s)", truncateOutput(step.Error, 80))
				}
				fmt.Println()
			}
			fmt.Println()
		}

		// Print attempts.
		if len(task.Attempts) > 0 {
			for _, a := range task.Attempts {
				fmt.Fprintf(os.Stdout, "--- Attempt #%d ---\n", a.Number)
				fmt.Fprintf(os.Stdout, "  Status: %s\n", a.Status)
				if a.Plan != "" {
					fmt.Fprintf(os.Stdout, "  Plan: %s\n", a.Plan)
				}
				if a.FailReason != "" {
					fmt.Fprintf(os.Stdout, "  Fail Reason: %s\n", a.FailReason)
				}
				if len(a.FilesChanged) > 0 {
					fmt.Fprintf(os.Stdout, "  Files Changed: %v\n", a.FilesChanged)
				}
				if a.Deploy != nil {
					fmt.Fprintf(os.Stdout, "  Deploy: %s (%s)\n", a.Deploy.Status, a.Deploy.Duration)
				}
				for _, t := range a.Tests {
					status := "PASS"
					if !t.Passed {
						status = "FAIL"
					}
					fmt.Fprintf(os.Stdout, "  Test [%s] %s: %s (%s)\n", status, t.Name, t.Type, t.Duration)
					if t.Output != "" {
						fmt.Fprintf(os.Stdout, "    Output: %s\n", truncateOutput(t.Output, 200))
					}
				}
				fmt.Println()
			}
		} else {
			fmt.Println("No attempts recorded.")
		}

		if !follow {
			return nil
		}

		// Follow mode: poll state every 2s and print new pipeline steps.
		fmt.Println("--- following (Ctrl+C to stop) ---")
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt)

		lastStepCount := 0
		lastAttemptCount := 0
		lastStatus := ""

		// Initialize counts from current state.
		if state, err := core.LoadState(statePath); err == nil {
			if task := state.GetTaskByID(taskID); task != nil {
				lastStepCount = len(task.Pipeline)
				lastAttemptCount = len(task.Attempts)
				lastStatus = string(task.Status)
			}
		}

		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-sigCh:
				fmt.Println("\nstopped.")
				return nil
			case <-ticker.C:
				state, err := core.LoadState(statePath)
				if err != nil {
					continue
				}
				task := state.GetTaskByID(taskID)
				if task == nil {
					continue
				}

				// Print new pipeline steps.
				if len(task.Pipeline) > lastStepCount {
					for _, step := range task.Pipeline[lastStepCount:] {
						marker := "●"
						switch step.Status {
						case "success":
							marker = "✓"
						case "failed":
							marker = "✗"
						case "skipped":
							marker = "–"
						}
						ts := step.StartedAt.Format("15:04:05")
						fmt.Fprintf(os.Stdout, "[%s] %s %s", ts, marker, step.Phase)
						if step.Output != "" {
							fmt.Fprintf(os.Stdout, " — %s", truncateOutput(step.Output, 120))
						}
						if step.Error != "" {
							fmt.Fprintf(os.Stdout, " (error: %s)", truncateOutput(step.Error, 80))
						}
						fmt.Println()
					}
					lastStepCount = len(task.Pipeline)
				}

				// Print new attempts.
				if len(task.Attempts) > lastAttemptCount {
					for _, a := range task.Attempts[lastAttemptCount:] {
						fmt.Fprintf(os.Stdout, "  → Attempt #%d: %s", a.Number, a.Status)
						if a.FailReason != "" {
							fmt.Fprintf(os.Stdout, " (%s)", a.FailReason)
						}
						fmt.Println()
					}
					lastAttemptCount = len(task.Attempts)
				}

				// Status change.
				if string(task.Status) != lastStatus {
					fmt.Fprintf(os.Stdout, "  ⟶ Status: %s\n", task.Status)
					lastStatus = string(task.Status)

					// Stop following on terminal states.
					if task.Status == core.PhaseCompleted || task.Status == core.PhaseFailed {
						fmt.Println("task finished.")
						return nil
					}
				}
			}
		}
	},
}
