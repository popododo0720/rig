package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TaskPhase represents the current phase of task execution.
type TaskPhase string

const (
	PhaseQueued           TaskPhase = "queued"
	PhasePlanning         TaskPhase = "planning"
	PhaseCoding           TaskPhase = "coding"
	PhaseCommitting       TaskPhase = "committing"
	PhaseApproval         TaskPhase = "approval"
	PhaseDeploying        TaskPhase = "deploying"
	PhaseTesting          TaskPhase = "testing"
	PhaseReporting        TaskPhase = "reporting"
	PhaseCompleted        TaskPhase = "completed"
	PhaseFailed           TaskPhase = "failed"
	PhaseRollback         TaskPhase = "rollback"
	PhaseAwaitingApproval TaskPhase = "awaiting_approval"
)

// strictlyTerminalPhases are phases from which no transition is allowed.
var strictlyTerminalPhases = map[TaskPhase]bool{
	PhaseCompleted: true,
	PhaseRollback:  true,
}

// inactivePhases are phases where the task is no longer "in flight" for
// duplicate webhook detection. Includes failed because a failed task is
// done processing even though it can still transition to rollback.
// awaiting_approval is also inactive since it requires user action to resume.
var inactivePhases = map[TaskPhase]bool{
	PhaseCompleted:        true,
	PhaseFailed:           true,
	PhaseRollback:         true,
	PhaseAwaitingApproval: true,
}

// validTransitions defines the allowed from→to state transitions.
var validTransitions = map[TaskPhase]map[TaskPhase]bool{
	PhaseQueued:           {PhasePlanning: true, PhaseFailed: true},
	PhasePlanning:         {PhaseCoding: true, PhaseFailed: true},
	PhaseCoding:           {PhaseCommitting: true, PhaseFailed: true},
	PhaseCommitting:       {PhaseApproval: true, PhaseDeploying: true, PhaseFailed: true},
	PhaseApproval:         {PhaseDeploying: true, PhaseFailed: true},
	PhaseDeploying:        {PhaseTesting: true, PhaseCoding: true, PhaseAwaitingApproval: true, PhaseFailed: true},
	PhaseTesting:          {PhaseReporting: true, PhaseCoding: true, PhaseDeploying: true, PhaseAwaitingApproval: true, PhaseFailed: true},
	PhaseReporting:        {PhaseCompleted: true, PhaseFailed: true},
	PhaseFailed:           {PhaseRollback: true},
	PhaseAwaitingApproval: {PhaseCoding: true, PhaseDeploying: true, PhaseFailed: true},
	// PhaseCompleted and PhaseRollback have no outgoing transitions (terminal).
}

// FailReason represents why a task failed.
type FailReason string

const (
	ReasonConfig   FailReason = "config_error"
	ReasonAI       FailReason = "ai_error"
	ReasonGit      FailReason = "git_error"
	ReasonApproval FailReason = "approval_timeout"
	ReasonDeploy   FailReason = "deploy_error"
	ReasonTest     FailReason = "test_error"
	ReasonInfra    FailReason = "infra_error"
	ReasonUnknown  FailReason = "unknown"
)

// State is the top-level persisted state for rig.
type State struct {
	Version string `json:"version"`
	Tasks   []Task `json:"tasks"`
}

// Task represents a single issue being worked on by rig.
type Task struct {
	ID          string         `json:"id"`
	Issue       Issue          `json:"issue"`
	Branch      string         `json:"branch"`
	Status      TaskPhase      `json:"status"`
	PR          *PullRequest   `json:"pr,omitempty"`
	Attempts    []Attempt      `json:"attempts"`
	Proposals   []Proposal     `json:"proposals,omitempty"`
	Pipeline    []PipelineStep `json:"pipeline,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	CompletedAt *time.Time     `json:"completed_at,omitempty"`
}

// Proposal represents an AI-suggested change that requires user approval.
type Proposal struct {
	ID         string           `json:"id"`
	Type       ProposalType     `json:"type"`
	Summary    string           `json:"summary"`
	Reason     string           `json:"reason"`
	Changes    []ProposedChange `json:"changes"`
	Status     ProposalStatus   `json:"status"`
	CreatedAt  time.Time        `json:"created_at"`
	ReviewedAt *time.Time       `json:"reviewed_at,omitempty"`
}

// ProposalType identifies what triggered the proposal.
type ProposalType string

const (
	ProposalDeployFix ProposalType = "deploy_fix"
	ProposalTestFix   ProposalType = "test_fix"
	ProposalInfraFix  ProposalType = "infra_fix"
)

// ProposalStatus tracks the lifecycle of a proposal.
type ProposalStatus string

const (
	ProposalPending  ProposalStatus = "pending"
	ProposalApproved ProposalStatus = "approved"
	ProposalRejected ProposalStatus = "rejected"
)

// ProposedChange is a single file change within a Proposal.
type ProposedChange struct {
	Path   string `json:"path"`
	Action string `json:"action"` // create | modify | delete
	Reason string `json:"reason"` // AI's explanation for this change
	Before string `json:"before"` // original content (empty for create)
	After  string `json:"after"`  // proposed content (empty for delete)
}

// PipelineStep records the execution of each phase with timing and outcome.
type PipelineStep struct {
	Phase     TaskPhase  `json:"phase"`
	Status    string     `json:"status"` // running | success | failed | skipped
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Output    string     `json:"output,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// Issue identifies the source issue that triggered a task.
type Issue struct {
	Platform string `json:"platform"`
	Repo     string `json:"repo"`
	ID       string `json:"id"`
	Title    string `json:"title"`
	URL      string `json:"url"`
}

// PullRequest holds PR metadata once one is created.
type PullRequest struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

// Attempt records a single try at completing a task.
type Attempt struct {
	Number       int           `json:"number"`
	Plan         string        `json:"plan,omitempty"`
	FilesChanged []string      `json:"files_changed,omitempty"`
	Deploy       *DeployResult `json:"deploy,omitempty"`
	Tests        []TestResult  `json:"tests"`
	Status       string        `json:"status"` // running|passed|failed
	FailReason   FailReason    `json:"fail_reason,omitempty"`
	StartedAt    time.Time     `json:"started_at"`
	CompletedAt  *time.Time    `json:"completed_at,omitempty"`
}

// DeployResult captures the outcome of a deployment step.
type DeployResult struct {
	Status   string        `json:"status"` // success|failed
	Duration time.Duration `json:"duration"`
	Output   string        `json:"output,omitempty"`
}

// TestResult captures the outcome of a single test execution.
type TestResult struct {
	Name     string        `json:"name"`
	Type     string        `json:"type"` // command|ai-verify
	Passed   bool          `json:"passed"`
	Output   string        `json:"output,omitempty"`
	Duration time.Duration `json:"duration"`
}

// ErrInvalidTransition is returned when a state transition is not allowed.
var ErrInvalidTransition = errors.New("invalid state transition")

var stateMu sync.Mutex

// Transition validates and applies a phase transition on a task.
// Returns ErrInvalidTransition if the transition is not allowed.
func Transition(task *Task, to TaskPhase) error {
	from := task.Status

	// Terminal states cannot transition to anything.
	if strictlyTerminalPhases[from] {
		return fmt.Errorf("%w: %s is terminal", ErrInvalidTransition, from)
	}

	allowed, ok := validTransitions[from]
	if !ok || !allowed[to] {
		return fmt.Errorf("%w: %s → %s", ErrInvalidTransition, from, to)
	}

	task.Status = to

	// Mark completion timestamp for terminal states.
	if to == PhaseCompleted || to == PhaseFailed || to == PhaseRollback {
		now := time.Now().UTC()
		task.CompletedAt = &now
	}

	return nil
}

// LoadState reads state from the given JSON file path.
// If the file does not exist, it returns a fresh State with version "1.0".
func LoadState(path string) (*State, error) {
	stateMu.Lock()
	defer stateMu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{Version: "1.0", Tasks: []Task{}}, nil
		}
		return nil, fmt.Errorf("read state file: %w", err)
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}
	return &s, nil
}

// SaveState writes state to the given path using atomic write (tmp + rename).
func SaveState(s *State, path string) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	// Ensure parent directory exists.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write tmp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath) // best-effort cleanup
		return fmt.Errorf("atomic rename: %w", err)
	}

	return nil
}

// CreateTask adds a new task in queued status for the given issue.
// It returns the newly created task.
func (s *State) CreateTask(issue Issue) *Task {
	id := fmt.Sprintf("task-%s-%03d", time.Now().UTC().Format("20060102-150405"), len(s.Tasks)+1)
	task := Task{
		ID:        id,
		Issue:     issue,
		Branch:    fmt.Sprintf("rig/issue-%s", issue.ID),
		Status:    PhaseQueued,
		Attempts:  []Attempt{},
		CreatedAt: time.Now().UTC(),
	}
	s.Tasks = append(s.Tasks, task)
	return &s.Tasks[len(s.Tasks)-1]
}

// GetTask finds a task by issue ID. Returns nil if not found.
func (s *State) GetTask(issueID string) *Task {
	for i := range s.Tasks {
		if s.Tasks[i].Issue.ID == issueID {
			return &s.Tasks[i]
		}
	}
	return nil
}

// AddPipelineStep records a new pipeline step for the task.
func (t *Task) AddPipelineStep(phase TaskPhase, status string) *PipelineStep {
	step := PipelineStep{
		Phase:     phase,
		Status:    status,
		StartedAt: time.Now().UTC(),
	}
	t.Pipeline = append(t.Pipeline, step)
	return &t.Pipeline[len(t.Pipeline)-1]
}

// CompletePipelineStep marks the last step of the given phase as completed.
func (t *Task) CompletePipelineStep(phase TaskPhase, status, output, errMsg string) {
	for i := len(t.Pipeline) - 1; i >= 0; i-- {
		if t.Pipeline[i].Phase == phase && t.Pipeline[i].Status == "running" {
			now := time.Now().UTC()
			t.Pipeline[i].EndedAt = &now
			t.Pipeline[i].Status = status
			t.Pipeline[i].Output = output
			t.Pipeline[i].Error = errMsg
			return
		}
	}
}

// AddProposal creates a new proposal and appends it to the task.
func (t *Task) AddProposal(pType ProposalType, summary, reason string, changes []ProposedChange) *Proposal {
	id := fmt.Sprintf("prop-%s-%03d", time.Now().UTC().Format("150405"), len(t.Proposals)+1)
	p := Proposal{
		ID:        id,
		Type:      pType,
		Summary:   summary,
		Reason:    reason,
		Changes:   changes,
		Status:    ProposalPending,
		CreatedAt: time.Now().UTC(),
	}
	t.Proposals = append(t.Proposals, p)
	return &t.Proposals[len(t.Proposals)-1]
}

// GetPendingProposal returns the most recent pending proposal, or nil.
func (t *Task) GetPendingProposal() *Proposal {
	for i := len(t.Proposals) - 1; i >= 0; i-- {
		if t.Proposals[i].Status == ProposalPending {
			return &t.Proposals[i]
		}
	}
	return nil
}

// GetTaskByID finds a task by its task ID. Returns nil if not found.
func (s *State) GetTaskByID(taskID string) *Task {
	for i := range s.Tasks {
		if s.Tasks[i].ID == taskID {
			return &s.Tasks[i]
		}
	}
	return nil
}

// IsInFlight reports whether an issue already has a non-terminal task.
// Used to prevent duplicate processing from repeated webhooks.
func (s *State) IsInFlight(issueID string) bool {
	for _, t := range s.Tasks {
		if t.Issue.ID == issueID && !inactivePhases[t.Status] {
			return true
		}
	}
	return false
}
