package main

import (
	"fmt"
	"strings"

	"github.com/rigdev/rig/internal/core"
	"github.com/spf13/cobra"
)

var proposalsCmd = &cobra.Command{
	Use:   "proposals [task-id]",
	Short: "Show pending proposals",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		state, err := core.LoadState(defaultStatePath)
		if err != nil {
			return fmt.Errorf("load state: %w", err)
		}

		if len(args) == 1 {
			taskID := args[0]
			task := state.GetTaskByID(taskID)
			if task == nil {
				return fmt.Errorf("task %q not found", taskID)
			}

			pending := pendingProposals(task)
			if len(pending) == 0 {
				fmt.Printf("No pending proposals for task %s.\n", taskID)
				return nil
			}

			printTaskProposals(task, pending)
			return nil
		}

		found := false
		for i := range state.Tasks {
			task := &state.Tasks[i]
			pending := pendingProposals(task)
			if len(pending) == 0 {
				continue
			}
			if found {
				fmt.Println()
			}
			printTaskProposals(task, pending)
			found = true
		}

		if !found {
			fmt.Println("No pending proposals.")
		}

		return nil
	},
}

func pendingProposals(task *core.Task) []core.Proposal {
	proposals := make([]core.Proposal, 0)
	for _, proposal := range task.Proposals {
		if proposal.Status == core.ProposalPending {
			proposals = append(proposals, proposal)
		}
	}
	return proposals
}

func printTaskProposals(task *core.Task, proposals []core.Proposal) {
	fmt.Printf("Task: %s\n", task.ID)
	for _, proposal := range proposals {
		fmt.Printf("Proposal: %s (%s) [%s]\n", proposal.ID, proposal.Type, proposal.Status)
		fmt.Printf("Summary: %q\n", proposal.Summary)
		fmt.Println("Changes:")
		for _, change := range proposal.Changes {
			fmt.Printf("  - %s (%s): %q\n", change.Path, change.Action, change.Reason)
			fmt.Println("    --- Before ---")
			fmt.Println(indentLines(firstNLines(change.Before, 10), "    "))
			fmt.Println("    --- After ---")
			fmt.Println(indentLines(firstNLines(change.After, 10), "    "))
		}
		fmt.Printf("Run: rig approve %s  |  rig reject %s\n", task.ID, task.ID)
	}
}

func firstNLines(content string, n int) string {
	if content == "" {
		return "(empty)"
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

func indentLines(content, indent string) string {
	if content == "" {
		return indent
	}
	return indent + strings.ReplaceAll(content, "\n", "\n"+indent)
}
