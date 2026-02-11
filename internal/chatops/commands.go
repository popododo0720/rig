package chatops

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rigdev/rig/internal/core"
)

// Command is a normalized chat command.
type Command struct {
	Action string
	Args   []string
}

var errCommandNotFound = errors.New("command not found")

// ParseCommand parses chat command text such as:
// "rig status", "/rig exec https://...", "!rig logs task-123".
func ParseCommand(text string) (*Command, error) {
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return nil, errors.New("empty command")
	}

	fields := strings.Fields(normalized)
	if len(fields) == 0 {
		return nil, errors.New("empty command")
	}

	first := strings.TrimPrefix(strings.ToLower(fields[0]), "/")
	first = strings.TrimPrefix(first, "!")

	if first == "rig" {
		if len(fields) < 2 {
			return nil, errors.New("missing action")
		}
		return &Command{Action: strings.ToLower(fields[1]), Args: fields[2:]}, nil
	}

	if first == "status" || first == "tasks" || first == "logs" || first == "approve" || first == "reject" || first == "exec" {
		return &Command{Action: first, Args: fields[1:]}, nil
	}

	return nil, errCommandNotFound
}

// Execute handles state-driven chat commands.
func Execute(cmd *Command, statePath string) (string, error) {
	if cmd == nil {
		return "", errors.New("nil command")
	}

	switch cmd.Action {
	case "status":
		return executeStatus(statePath)
	case "tasks":
		return executeTasks(statePath)
	case "logs":
		if len(cmd.Args) < 1 {
			return "", errors.New("logs requires task id")
		}
		return executeLogs(statePath, cmd.Args[0])
	case "approve":
		if len(cmd.Args) < 1 {
			return "", errors.New("approve requires task id")
		}
		return updateProposalStatus(statePath, cmd.Args[0], true)
	case "reject":
		if len(cmd.Args) < 1 {
			return "", errors.New("reject requires task id")
		}
		return updateProposalStatus(statePath, cmd.Args[0], false)
	default:
		return "", fmt.Errorf("unsupported action %q", cmd.Action)
	}
}

func executeStatus(statePath string) (string, error) {
	state, err := core.LoadState(statePath)
	if err != nil {
		return "", fmt.Errorf("load state: %w", err)
	}

	if len(state.Tasks) == 0 {
		return "No tasks found.", nil
	}

	counts := make(map[core.TaskPhase]int)
	for _, task := range state.Tasks {
		counts[task.Status]++
	}

	phases := make([]string, 0, len(counts))
	for phase := range counts {
		phases = append(phases, string(phase))
	}
	sort.Strings(phases)

	lines := make([]string, 0, len(phases)+1)
	lines = append(lines, fmt.Sprintf("Task summary: %d total", len(state.Tasks)))
	for _, phase := range phases {
		lines = append(lines, fmt.Sprintf("- %s: %d", phase, counts[core.TaskPhase(phase)]))
	}

	return strings.Join(lines, "\n"), nil
}

func executeTasks(statePath string) (string, error) {
	state, err := core.LoadState(statePath)
	if err != nil {
		return "", fmt.Errorf("load state: %w", err)
	}

	if len(state.Tasks) == 0 {
		return "No tasks found.", nil
	}

	tasks := append([]core.Task(nil), state.Tasks...)
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt.After(tasks[j].CreatedAt)
	})

	limit := 5
	if len(tasks) < limit {
		limit = len(tasks)
	}

	lines := make([]string, 0, limit+1)
	lines = append(lines, "Recent tasks:")
	for i := 0; i < limit; i++ {
		t := tasks[i]
		lines = append(lines, fmt.Sprintf("- %s [%s] %s", t.ID, t.Status, truncate(t.Issue.Title, 48)))
	}

	return strings.Join(lines, "\n"), nil
}

func executeLogs(statePath, taskID string) (string, error) {
	state, err := core.LoadState(statePath)
	if err != nil {
		return "", fmt.Errorf("load state: %w", err)
	}

	task := state.GetTaskByID(taskID)
	if task == nil {
		return "", fmt.Errorf("task %q not found", taskID)
	}

	if len(task.Pipeline) == 0 {
		return fmt.Sprintf("Task %s has no pipeline steps.", taskID), nil
	}

	start := len(task.Pipeline) - 5
	if start < 0 {
		start = 0
	}

	lines := make([]string, 0, len(task.Pipeline)-start+1)
	lines = append(lines, fmt.Sprintf("Last pipeline steps for %s:", taskID))
	for _, step := range task.Pipeline[start:] {
		line := fmt.Sprintf("- %s [%s] %s", step.Phase, step.Status, step.StartedAt.Format(time.RFC3339))
		if step.Error != "" {
			line += fmt.Sprintf(" error=%s", truncate(step.Error, 80))
		}
		if step.Output != "" {
			line += fmt.Sprintf(" output=%s", truncate(step.Output, 80))
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n"), nil
}

func updateProposalStatus(statePath, taskID string, approve bool) (string, error) {
	var output string
	err := core.WithState(statePath, func(s *core.State) error {
		task := s.GetTaskByID(taskID)
		if task == nil {
			return fmt.Errorf("task %q not found", taskID)
		}

		proposal := task.GetPendingProposal()
		if proposal == nil {
			return errors.New("no pending proposal")
		}

		now := time.Now().UTC()
		proposal.ReviewedAt = &now
		if approve {
			proposal.Status = core.ProposalApproved
			output = fmt.Sprintf("Proposal approved for %s.", taskID)
			return nil
		}

		proposal.Status = core.ProposalRejected
		if task.Status == core.PhaseAwaitingApproval {
			if err := core.Transition(task, core.PhaseFailed); err != nil {
				return fmt.Errorf("transition task to failed: %w", err)
			}
		}
		output = fmt.Sprintf("Proposal rejected for %s.", taskID)
		return nil
	})
	if err != nil {
		return "", err
	}

	return output, nil
}

func truncate(input string, maxLen int) string {
	if len(input) <= maxLen {
		return input
	}
	if maxLen <= 3 {
		return input[:maxLen]
	}
	return input[:maxLen-3] + "..."
}
