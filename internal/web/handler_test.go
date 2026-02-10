package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rigdev/rig/internal/config"
	"github.com/rigdev/rig/internal/core"
)

// testState returns a State with two tasks for testing.
func testState() *core.State {
	now := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	completed := now.Add(30 * time.Minute)
	return &core.State{
		Version: "1.0",
		Tasks: []core.Task{
			{
				ID:     "task-001",
				Branch: "rig/issue-42",
				Status: core.PhaseCompleted,
				Issue: core.Issue{
					Platform: "github",
					Repo:     "acme/app",
					ID:       "42",
					Title:    "Fix login bug",
					URL:      "https://github.com/acme/app/issues/42",
				},
				PR: &core.PullRequest{
					ID:  "99",
					URL: "https://github.com/acme/app/pull/99",
				},
				Attempts: []core.Attempt{
					{
						Number:       1,
						Status:       "passed",
						FilesChanged: []string{"auth.go"},
						StartedAt:    now,
						CompletedAt:  &completed,
						Tests: []core.TestResult{
							{
								Name:     "unit-test",
								Type:     "command",
								Passed:   true,
								Duration: 5 * time.Second,
								Output:   "ok",
							},
						},
					},
				},
				CreatedAt:   now,
				CompletedAt: &completed,
			},
			{
				ID:     "task-002",
				Branch: "rig/issue-43",
				Status: core.PhaseCoding,
				Issue: core.Issue{
					Platform: "github",
					Repo:     "acme/app",
					ID:       "43",
					Title:    "Add dark mode",
					URL:      "https://github.com/acme/app/issues/43",
				},
				Attempts:  []core.Attempt{},
				CreatedAt: now.Add(time.Hour),
			},
		},
	}
}

func testConfig() *config.Config {
	return &config.Config{
		Project: config.ProjectConfig{
			Name:     "acme-app",
			Language: "go",
		},
		Source: config.SourceConfig{
			Platform:   "github",
			Repo:       "acme/app",
			BaseBranch: "main",
			Token:      "ghp_SECRETTOKEN",
		},
		AI: config.AIConfig{
			Provider: "anthropic",
			Model:    "claude-opus-4-6",
			APIKey:   "sk-ant-SECRET",
			MaxRetry: 3,
		},
		Deploy: config.DeployConfig{
			Method: "custom",
		},
		Workflow: config.WorkflowConfig{
			Steps: []string{"code", "deploy", "test"},
		},
	}
}

// writeStateFile serializes state to a temp JSON file and returns its path.
func writeStateFile(t *testing.T, s *core.State) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "state.json")
	if err := core.SaveState(s, p); err != nil {
		t.Fatalf("write state file: %v", err)
	}
	return p
}

func TestGetTasks(t *testing.T) {
	statePath := writeStateFile(t, testState())
	handler := NewHandler(statePath, testConfig())

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}

	var tasks []core.Task
	if err := json.NewDecoder(rec.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}

	if tasks[0].ID != "task-001" {
		t.Errorf("expected first task ID task-001, got %s", tasks[0].ID)
	}
}

func TestGetTaskByID(t *testing.T) {
	statePath := writeStateFile(t, testState())
	handler := NewHandler(statePath, testConfig())

	// Found
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/task-002", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var task core.Task
	if err := json.NewDecoder(rec.Body).Decode(&task); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if task.ID != "task-002" {
		t.Errorf("expected task-002, got %s", task.ID)
	}

	if task.Status != core.PhaseCoding {
		t.Errorf("expected status coding, got %s", task.Status)
	}
}

func TestGetTaskNotFound(t *testing.T) {
	statePath := writeStateFile(t, testState())
	handler := NewHandler(statePath, testConfig())

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/nonexistent", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestGetConfig(t *testing.T) {
	statePath := writeStateFile(t, testState())
	handler := NewHandler(statePath, testConfig())

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()

	// Must contain project name
	if !containsString(body, "acme-app") {
		t.Error("response should contain project name")
	}

	// Must NOT contain secrets
	if containsString(body, "ghp_SECRETTOKEN") {
		t.Error("response must not contain GitHub token")
	}
	if containsString(body, "sk-ant-SECRET") {
		t.Error("response must not contain API key")
	}
}

func TestStaticFileServing(t *testing.T) {
	// Use a non-existent state file — LoadState handles that gracefully.
	statePath := filepath.Join(t.TempDir(), "nonexistent.json")
	handler := NewHandler(statePath, testConfig())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !containsString(body, "Rig Dashboard") {
		t.Error("expected HTML containing 'Rig Dashboard'")
	}
}

func TestGetTasksMissingStateFile(t *testing.T) {
	// LoadState returns empty state for missing file.
	statePath := filepath.Join(t.TempDir(), "missing", "state.json")
	handler := NewHandler(statePath, testConfig())

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var tasks []core.Task
	if err := json.NewDecoder(rec.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks for missing state, got %d", len(tasks))
	}
}

func containsString(haystack, needle string) bool {
	return len(haystack) >= len(needle) && searchString(haystack, needle)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// Ensure test temp files are cleaned up.
func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
