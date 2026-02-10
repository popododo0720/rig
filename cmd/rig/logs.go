package main

import (
	"fmt"
	"os"

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

		state, err := core.LoadState(statePath)
		if err != nil {
			return fmt.Errorf("load state: %w", err)
		}

		// Find the task by ID.
		var task *core.Task
		for i := range state.Tasks {
			if state.Tasks[i].ID == taskID {
				task = &state.Tasks[i]
				break
			}
		}

		if task == nil {
			return fmt.Errorf("task %q not found", taskID)
		}

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

		if len(task.Attempts) == 0 {
			fmt.Println("No attempts recorded.")
			return nil
		}

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

		return nil
	},
}
