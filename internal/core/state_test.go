package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateTransition(t *testing.T) {
	tests := []struct {
		name    string
		from    TaskPhase
		to      TaskPhase
		wantErr bool
	}{
		// Valid forward transitions
		{"queued→planning", PhaseQueued, PhasePlanning, false},
		{"planning→coding", PhasePlanning, PhaseCoding, false},
		{"coding→committing", PhaseCoding, PhaseCommitting, false},
		{"committing→approval", PhaseCommitting, PhaseApproval, false},
		{"committing→deploying", PhaseCommitting, PhaseDeploying, false},
		{"approval→deploying", PhaseApproval, PhaseDeploying, false},
		{"deploying→testing", PhaseDeploying, PhaseTesting, false},
		{"testing→reporting", PhaseTesting, PhaseReporting, false},
		{"reporting→completed", PhaseReporting, PhaseCompleted, false},

		// Valid retry transitions
		{"testing→coding (retry)", PhaseTesting, PhaseCoding, false},
		{"testing→deploying (redeploy)", PhaseTesting, PhaseDeploying, false},
		{"deploying→coding (deploy fail retry)", PhaseDeploying, PhaseCoding, false},

		// Valid failure transitions (any non-terminal → failed)
		{"queued→failed", PhaseQueued, PhaseFailed, false},
		{"planning→failed", PhasePlanning, PhaseFailed, false},
		{"coding→failed", PhaseCoding, PhaseFailed, false},
		{"committing→failed", PhaseCommitting, PhaseFailed, false},
		{"approval→failed", PhaseApproval, PhaseFailed, false},
		{"deploying→failed", PhaseDeploying, PhaseFailed, false},
		{"testing→failed", PhaseTesting, PhaseFailed, false},
		{"reporting→failed", PhaseReporting, PhaseFailed, false},

		// Valid rollback
		{"failed→rollback", PhaseFailed, PhaseRollback, false},

		// Invalid: completed is terminal
		{"completed→queued REJECTED", PhaseCompleted, PhaseQueued, true},
		{"completed→planning REJECTED", PhaseCompleted, PhasePlanning, true},
		{"completed→failed REJECTED", PhaseCompleted, PhaseFailed, true},

		// Invalid: rollback is terminal
		{"rollback→queued REJECTED", PhaseRollback, PhaseQueued, true},
		{"rollback→failed REJECTED", PhaseRollback, PhaseFailed, true},

		// Invalid: skipping phases
		{"queued→completed REJECTED", PhaseQueued, PhaseCompleted, true},
		{"queued→coding REJECTED", PhaseQueued, PhaseCoding, true},
		{"planning→deploying REJECTED", PhasePlanning, PhaseDeploying, true},
		{"coding→testing REJECTED", PhaseCoding, PhaseTesting, true},

		// Invalid: backward (non-retry)
		{"reporting→testing REJECTED", PhaseReporting, PhaseTesting, true},
		{"coding→queued REJECTED", PhaseCoding, PhaseQueued, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &Task{Status: tt.from}
			err := Transition(task, tt.to)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Transition(%s → %s) expected error, got nil", tt.from, tt.to)
				}
				// Status should NOT change on error
				if task.Status != tt.from {
					t.Errorf("status changed to %s on failed transition", task.Status)
				}
			} else {
				if err != nil {
					t.Errorf("Transition(%s → %s) unexpected error: %v", tt.from, tt.to, err)
				}
				if task.Status != tt.to {
					t.Errorf("status = %s, want %s", task.Status, tt.to)
				}
			}
		})
	}
}

func TestStateTransitionSetsCompletedAt(t *testing.T) {
	task := &Task{Status: PhaseReporting}
	if err := Transition(task, PhaseCompleted); err != nil {
		t.Fatal(err)
	}
	if task.CompletedAt == nil {
		t.Error("CompletedAt not set after transition to completed")
	}

	task2 := &Task{Status: PhaseCoding}
	if err := Transition(task2, PhaseFailed); err != nil {
		t.Fatal(err)
	}
	if task2.CompletedAt == nil {
		t.Error("CompletedAt not set after transition to failed")
	}
}

func TestAtomicSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rig.state.json")

	now := time.Now().UTC().Truncate(time.Second) // truncate for JSON round-trip
	completedAt := now.Add(10 * time.Minute)

	original := &State{
		Version: "1.0",
		Tasks: []Task{
			{
				ID: "task-20260210-001",
				Issue: Issue{
					Platform: "github",
					Repo:     "my-org/my-app",
					ID:       "42",
					Title:    "Fix login page bug",
					URL:      "https://github.com/my-org/my-app/issues/42",
				},
				Branch: "rig/issue-42",
				Status: PhaseCompleted,
				PR: &PullRequest{
					ID:  "15",
					URL: "https://github.com/my-org/my-app/pull/15",
				},
				Attempts: []Attempt{
					{
						Number:       1,
						Plan:         "Fix login form validation",
						FilesChanged: []string{"src/auth/login.ts"},
						Tests: []TestResult{
							{Name: "unit-test", Type: "command", Passed: true, Duration: 5 * time.Second},
						},
						Status:    "passed",
						StartedAt: now,
					},
				},
				CreatedAt:   now,
				CompletedAt: &completedAt,
			},
		},
	}

	// Save
	if err := SaveState(original, path); err != nil {
		t.Fatalf("SaveState error: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file does not exist: %v", err)
	}

	// Verify no tmp file left behind
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("tmp file was not cleaned up")
	}

	// Load it back
	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState error: %v", err)
	}

	// Compare via JSON round-trip (simplest deep equality for time.Time)
	origJSON, _ := json.Marshal(original)
	loadedJSON, _ := json.Marshal(loaded)
	if string(origJSON) != string(loadedJSON) {
		t.Errorf("round-trip mismatch:\noriginal: %s\nloaded:   %s", origJSON, loadedJSON)
	}
}

func TestLoadState(t *testing.T) {
	t.Run("non-existent file returns empty state", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "does-not-exist.json")

		s, err := LoadState(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.Version != "1.0" {
			t.Errorf("version = %q, want %q", s.Version, "1.0")
		}
		if len(s.Tasks) != 0 {
			t.Errorf("tasks = %d, want 0", len(s.Tasks))
		}
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "bad.json")
		os.WriteFile(path, []byte("{invalid"), 0644)

		_, err := LoadState(path)
		if err == nil {
			t.Error("expected error for invalid JSON, got nil")
		}
	})
}

func TestIsInFlight(t *testing.T) {
	s := &State{
		Version: "1.0",
		Tasks: []Task{
			{Issue: Issue{ID: "1"}, Status: PhaseCoding},    // in-flight
			{Issue: Issue{ID: "2"}, Status: PhaseCompleted}, // terminal
			{Issue: Issue{ID: "3"}, Status: PhaseFailed},    // terminal
			{Issue: Issue{ID: "4"}, Status: PhaseRollback},  // terminal
			{Issue: Issue{ID: "5"}, Status: PhaseQueued},    // in-flight
			{Issue: Issue{ID: "6"}, Status: PhaseTesting},   // in-flight
		},
	}

	tests := []struct {
		issueID string
		want    bool
	}{
		{"1", true},   // coding → in-flight
		{"2", false},  // completed → terminal
		{"3", false},  // failed → terminal
		{"4", false},  // rollback → terminal
		{"5", true},   // queued → in-flight
		{"6", true},   // testing → in-flight
		{"99", false}, // doesn't exist
	}

	for _, tt := range tests {
		t.Run("issue-"+tt.issueID, func(t *testing.T) {
			got := s.IsInFlight(tt.issueID)
			if got != tt.want {
				t.Errorf("IsInFlight(%q) = %v, want %v", tt.issueID, got, tt.want)
			}
		})
	}
}

func TestCreateTask(t *testing.T) {
	s := &State{Version: "1.0", Tasks: []Task{}}

	issue := Issue{
		Platform: "github",
		Repo:     "my-org/my-app",
		ID:       "42",
		Title:    "Fix login bug",
		URL:      "https://github.com/my-org/my-app/issues/42",
	}

	task := s.CreateTask(issue)

	if task.Status != PhaseQueued {
		t.Errorf("status = %s, want %s", task.Status, PhaseQueued)
	}
	if task.Issue.ID != "42" {
		t.Errorf("issue ID = %s, want 42", task.Issue.ID)
	}
	if task.Branch != "rig/issue-42" {
		t.Errorf("branch = %s, want rig/issue-42", task.Branch)
	}
	if task.CreatedAt.IsZero() {
		t.Error("CreatedAt not set")
	}
	if task.ID == "" {
		t.Error("ID not generated")
	}
	if len(s.Tasks) != 1 {
		t.Errorf("tasks count = %d, want 1", len(s.Tasks))
	}
}

func TestGetTask(t *testing.T) {
	s := &State{
		Version: "1.0",
		Tasks: []Task{
			{Issue: Issue{ID: "10"}, Status: PhaseCoding},
			{Issue: Issue{ID: "20"}, Status: PhaseCompleted},
		},
	}

	if task := s.GetTask("10"); task == nil {
		t.Error("GetTask(10) returned nil")
	} else if task.Issue.ID != "10" {
		t.Errorf("GetTask(10) returned issue %s", task.Issue.ID)
	}

	if task := s.GetTask("20"); task == nil {
		t.Error("GetTask(20) returned nil")
	}

	if task := s.GetTask("99"); task != nil {
		t.Error("GetTask(99) should return nil")
	}
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := &State{Version: "1.0", Tasks: []Task{}}
	issue := Issue{
		Platform: "github",
		Repo:     "test/repo",
		ID:       "7",
		Title:    "Test issue",
		URL:      "https://github.com/test/repo/issues/7",
	}
	task := s.CreateTask(issue)

	// Transition through several phases
	if err := Transition(task, PhasePlanning); err != nil {
		t.Fatal(err)
	}
	if err := Transition(task, PhaseCoding); err != nil {
		t.Fatal(err)
	}

	// Save
	if err := SaveState(s, path); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	// Load
	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	if loaded.Version != s.Version {
		t.Errorf("version mismatch: %s vs %s", loaded.Version, s.Version)
	}
	if len(loaded.Tasks) != 1 {
		t.Fatalf("tasks count = %d, want 1", len(loaded.Tasks))
	}
	lt := loaded.Tasks[0]
	if lt.Status != PhaseCoding {
		t.Errorf("status = %s, want %s", lt.Status, PhaseCoding)
	}
	if lt.Issue.ID != "7" {
		t.Errorf("issue ID = %s, want 7", lt.Issue.ID)
	}
	if lt.Branch != "rig/issue-7" {
		t.Errorf("branch = %s, want rig/issue-7", lt.Branch)
	}
}
