package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/rigdev/rig/internal/core"
	"github.com/spf13/cobra"
)

var explainCmd = &cobra.Command{
	Use:   "explain <task-id>",
	Short: "Explain task failures from local state",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID := args[0]
		statePath := ".rig/state.json"

		state, err := core.LoadState(statePath)
		if err != nil {
			return fmt.Errorf("load state: %w", err)
		}

		task := state.GetTaskByID(taskID)
		if task == nil {
			fmt.Fprintf(os.Stdout, "%s task %q not found\n", markerWarn(), taskID)
			return nil
		}

		fmt.Fprintf(os.Stdout, "Task: %s\n", task.ID)
		fmt.Fprintf(os.Stdout, "Status: %s %s\n", task.Status, statusMarker(string(task.Status)))
		fmt.Fprintf(os.Stdout, "Issue: %s (%s)\n", task.Issue.Title, task.Issue.URL)
		fmt.Fprintf(os.Stdout, "Total attempts: %d\n", len(task.Attempts))
		fmt.Fprintln(os.Stdout)

		if len(task.Attempts) == 0 {
			fmt.Fprintf(os.Stdout, "%s no attempts recorded\n", markerWarn())
		} else {
			fmt.Fprintln(os.Stdout, "Attempts:")
			for _, attempt := range task.Attempts {
				fmt.Fprintf(os.Stdout, "- Attempt #%d [%s] %s\n", attempt.Number, attempt.Status, statusMarker(attempt.Status))
				if attempt.FailReason != "" {
					fmt.Fprintf(os.Stdout, "  Failure reason: %s\n", attempt.FailReason)
				} else {
					fmt.Fprintln(os.Stdout, "  Failure reason: (none)")
				}

				if attempt.Deploy != nil {
					fmt.Fprintf(os.Stdout, "  Deploy status: %s\n", attempt.Deploy.Status)
					if attempt.Deploy.Output != "" {
						fmt.Fprintln(os.Stdout, "  Deploy output:")
						fmt.Fprintln(os.Stdout, indentLines(limitLines(attempt.Deploy.Output, 40), "    "))
					}
				}

				if len(attempt.Tests) == 0 {
					fmt.Fprintln(os.Stdout, "  Test output: (none)")
				} else {
					fmt.Fprintln(os.Stdout, "  Test output:")
					for _, test := range attempt.Tests {
						testStatus := "passed"
						if !test.Passed {
							testStatus = "failed"
						}
						fmt.Fprintf(os.Stdout, "    - %s [%s] %s\n", test.Name, testStatus, statusMarker(testStatus))
						if test.Output != "" {
							fmt.Fprintln(os.Stdout, indentLines(limitLines(test.Output, 30), "      "))
						}
					}
				}
			}
		}

		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, "Pipeline:")
		if len(task.Pipeline) == 0 {
			fmt.Fprintf(os.Stdout, "- %s no pipeline steps recorded\n", markerWarn())
		} else {
			for _, step := range task.Pipeline {
				fmt.Fprintf(os.Stdout, "- %s %s [%s]\n", statusMarker(step.Status), step.Phase, step.Status)
				if step.Output != "" {
					fmt.Fprintln(os.Stdout, "  Detail:")
					fmt.Fprintln(os.Stdout, indentLines(limitLines(step.Output, 30), "    "))
				}
				if step.Error != "" {
					fmt.Fprintln(os.Stdout, "  Error:")
					fmt.Fprintln(os.Stdout, indentLines(limitLines(step.Error, 30), "    "))
				}
			}
		}

		if len(task.Proposals) > 0 {
			fmt.Fprintln(os.Stdout)
			fmt.Fprintln(os.Stdout, "Proposals:")
			for _, proposal := range task.Proposals {
				fmt.Fprintf(os.Stdout, "- %s [%s] %s\n", proposal.ID, proposal.Status, statusMarker(string(proposal.Status)))
				fmt.Fprintf(os.Stdout, "  Summary: %s\n", nonEmpty(proposal.Summary))
				fmt.Fprintf(os.Stdout, "  Reason: %s\n", nonEmpty(proposal.Reason))
				if len(proposal.Changes) == 0 {
					fmt.Fprintln(os.Stdout, "  Changes: (none)")
					continue
				}
				fmt.Fprintln(os.Stdout, "  Changes:")
				for _, change := range proposal.Changes {
					line := fmt.Sprintf("%s (%s)", change.Path, change.Action)
					if change.Reason != "" {
						line += ": " + change.Reason
					}
					fmt.Fprintf(os.Stdout, "    - %s\n", line)
				}
			}
		}

		return nil
	},
}

func statusMarker(status string) string {
	s := strings.ToLower(status)
	switch {
	case strings.Contains(s, "pass"), strings.Contains(s, "success"), strings.Contains(s, "complete"), strings.Contains(s, "approved"):
		return markerOK()
	case strings.Contains(s, "fail"), strings.Contains(s, "error"), strings.Contains(s, "reject"):
		return markerFail()
	default:
		return markerWarn()
	}
}

func markerOK() string {
	return "✓"
}

func markerFail() string {
	return "✗"
}

func markerWarn() string {
	return "⚠"
}

func nonEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)"
	}
	return s
}

func limitLines(content string, maxLines int) string {
	if content == "" {
		return ""
	}
	parts := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(parts) <= maxLines {
		return strings.Join(parts, "\n")
	}
	return strings.Join(parts[:maxLines], "\n") + "\n..."
}
