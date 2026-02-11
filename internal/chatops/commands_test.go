package chatops

import (
	"strings"
	"testing"
	"time"

	"github.com/rigdev/rig/internal/core"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantAction string
		wantArgs   []string
		wantErr    bool
	}{
		{
			name:       "rig status",
			input:      "rig status",
			wantAction: "status",
		},
		{
			name:       "slash command with url",
			input:      "/rig exec https://github.com/acme/api/issues/42",
			wantAction: "exec",
			wantArgs:   []string{"https://github.com/acme/api/issues/42"},
		},
		{
			name:       "discord command",
			input:      "!rig approve task-1",
			wantAction: "approve",
			wantArgs:   []string{"task-1"},
		},
		{
			name:       "action without rig prefix",
			input:      "tasks",
			wantAction: "tasks",
		},
		{
			name:    "empty input",
			input:   "   ",
			wantErr: true,
		},
		{
			name:    "unknown command",
			input:   "hello world",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := ParseCommand(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q", tt.input)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cmd.Action != tt.wantAction {
				t.Fatalf("expected action %q, got %q", tt.wantAction, cmd.Action)
			}
			if len(cmd.Args) != len(tt.wantArgs) {
				t.Fatalf("expected %d args, got %d", len(tt.wantArgs), len(cmd.Args))
			}
			for i := range tt.wantArgs {
				if cmd.Args[i] != tt.wantArgs[i] {
					t.Fatalf("expected arg[%d]=%q, got %q", i, tt.wantArgs[i], cmd.Args[i])
				}
			}
		})
	}
}

func TestExecuteStatusAndTasks(t *testing.T) {
	statePath := writeState(t, makeStateFixture())

	statusText, err := Execute(&Command{Action: "status"}, statePath)
	if err != nil {
		t.Fatalf("execute status: %v", err)
	}
	if !strings.Contains(statusText, "Task summary:") {
		t.Fatalf("status output missing summary: %q", statusText)
	}
	if !strings.Contains(statusText, "completed") {
		t.Fatalf("status output missing completed phase count: %q", statusText)
	}

	tasksText, err := Execute(&Command{Action: "tasks"}, statePath)
	if err != nil {
		t.Fatalf("execute tasks: %v", err)
	}
	if !strings.Contains(tasksText, "Recent tasks:") {
		t.Fatalf("tasks output missing header: %q", tasksText)
	}
	if !strings.Contains(tasksText, "task-006") {
		t.Fatalf("tasks output should include newest task: %q", tasksText)
	}
	if strings.Contains(tasksText, "task-001") {
		t.Fatalf("tasks output should include only recent 5 tasks: %q", tasksText)
	}
}

func TestExecuteLogs(t *testing.T) {
	statePath := writeState(t, makeStateFixture())

	text, err := Execute(&Command{Action: "logs", Args: []string{"task-006"}}, statePath)
	if err != nil {
		t.Fatalf("execute logs: %v", err)
	}

	if !strings.Contains(text, "Last pipeline steps for task-006") {
		t.Fatalf("logs output missing header: %q", text)
	}
	if strings.Contains(text, "- queued") {
		t.Fatalf("logs output should include only last five steps: %q", text)
	}
	if !strings.Contains(text, "- testing") {
		t.Fatalf("logs output missing expected step: %q", text)
	}
}

func TestExecuteApproveReject(t *testing.T) {
	statePath := writeState(t, makeStateFixture())

	if _, err := Execute(&Command{Action: "approve", Args: []string{"task-003"}}, statePath); err != nil {
		t.Fatalf("approve failed: %v", err)
	}

	state, err := core.LoadState(statePath)
	if err != nil {
		t.Fatalf("load state after approve: %v", err)
	}
	task := state.GetTaskByID("task-003")
	if task == nil {
		t.Fatal("task-003 missing")
	}
	if task.Proposals[0].Status != core.ProposalApproved {
		t.Fatalf("expected proposal approved, got %s", task.Proposals[0].Status)
	}

	if _, err := Execute(&Command{Action: "reject", Args: []string{"task-004"}}, statePath); err != nil {
		t.Fatalf("reject failed: %v", err)
	}

	state, err = core.LoadState(statePath)
	if err != nil {
		t.Fatalf("load state after reject: %v", err)
	}
	task = state.GetTaskByID("task-004")
	if task == nil {
		t.Fatal("task-004 missing")
	}
	if task.Proposals[0].Status != core.ProposalRejected {
		t.Fatalf("expected proposal rejected, got %s", task.Proposals[0].Status)
	}
	if task.Status != core.PhaseFailed {
		t.Fatalf("expected task-004 status failed, got %s", task.Status)
	}
}

func writeState(t *testing.T, s *core.State) string {
	t.Helper()
	path := t.TempDir() + "/state.json"
	if err := core.SaveState(s, path); err != nil {
		t.Fatalf("save state: %v", err)
	}
	return path
}

func makeStateFixture() *core.State {
	base := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)

	tasks := []core.Task{
		newTask("task-001", core.PhaseCompleted, base.Add(1*time.Minute), nil, nil),
		newTask("task-002", core.PhaseCoding, base.Add(2*time.Minute), nil, nil),
		newTask("task-003", core.PhaseAwaitingApproval, base.Add(3*time.Minute), []core.Proposal{{ID: "prop-1", Status: core.ProposalPending, CreatedAt: base.Add(3 * time.Minute)}}, nil),
		newTask("task-004", core.PhaseAwaitingApproval, base.Add(4*time.Minute), []core.Proposal{{ID: "prop-2", Status: core.ProposalPending, CreatedAt: base.Add(4 * time.Minute)}}, nil),
		newTask("task-005", core.PhaseDeploying, base.Add(5*time.Minute), nil, nil),
		newTask("task-006", core.PhaseTesting, base.Add(6*time.Minute), nil, []core.PipelineStep{
			{Phase: core.PhaseQueued, Status: "success", StartedAt: base.Add(6 * time.Minute)},
			{Phase: core.PhasePlanning, Status: "success", StartedAt: base.Add(7 * time.Minute)},
			{Phase: core.PhaseCoding, Status: "success", StartedAt: base.Add(8 * time.Minute)},
			{Phase: core.PhaseCommitting, Status: "success", StartedAt: base.Add(9 * time.Minute)},
			{Phase: core.PhaseDeploying, Status: "success", StartedAt: base.Add(10 * time.Minute)},
			{Phase: core.PhaseTesting, Status: "running", StartedAt: base.Add(11 * time.Minute)},
		}),
	}

	return &core.State{Version: "1.0", Tasks: tasks}
}

func newTask(id string, status core.TaskPhase, createdAt time.Time, proposals []core.Proposal, pipeline []core.PipelineStep) core.Task {
	return core.Task{
		ID:        id,
		Status:    status,
		CreatedAt: createdAt,
		Issue: core.Issue{
			Platform: "github",
			Repo:     "acme/rig",
			ID:       strings.TrimPrefix(id, "task-"),
			Title:    "Issue " + id,
			URL:      "https://github.com/acme/rig/issues/" + strings.TrimPrefix(id, "task-"),
		},
		Proposals: proposals,
		Pipeline:  pipeline,
	}
}
