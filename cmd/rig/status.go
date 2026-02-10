package main

import (
	"fmt"
	"os"

	"github.com/rigdev/rig/internal/core"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current task status from state.json",
	RunE: func(cmd *cobra.Command, args []string) error {
		statePath := ".rig/state.json"

		state, err := core.LoadState(statePath)
		if err != nil {
			return fmt.Errorf("load state: %w", err)
		}

		if len(state.Tasks) == 0 {
			fmt.Println("No tasks found.")
			return nil
		}

		fmt.Fprintf(os.Stdout, "%-30s %-12s %-20s %-10s %s\n",
			"TASK ID", "STATUS", "ISSUE", "ATTEMPTS", "CREATED")
		fmt.Println("------------------------------------------------------------------------------------")

		for _, t := range state.Tasks {
			fmt.Fprintf(os.Stdout, "%-30s %-12s %-20s %-10d %s\n",
				t.ID,
				t.Status,
				truncate(t.Issue.Title, 18),
				len(t.Attempts),
				t.CreatedAt.Format("2006-01-02 15:04"),
			)
		}

		return nil
	},
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-2] + ".."
}
