package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rigdev/rig/internal/core"
)

func testDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// --- Settings ---

func TestSettings_SetAndGet(t *testing.T) {
	db := testDB(t)

	if err := db.SetSetting("ai", `{"provider":"anthropic"}`); err != nil {
		t.Fatalf("set: %v", err)
	}

	val, err := db.GetSetting("ai")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if val != `{"provider":"anthropic"}` {
		t.Errorf("got %q, want %q", val, `{"provider":"anthropic"}`)
	}
}

func TestSettings_GetMissing(t *testing.T) {
	db := testDB(t)

	val, err := db.GetSetting("nonexistent")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if val != "" {
		t.Errorf("got %q, want empty", val)
	}
}

func TestSettings_Upsert(t *testing.T) {
	db := testDB(t)

	if err := db.SetSetting("key", "v1"); err != nil {
		t.Fatalf("set v1: %v", err)
	}
	if err := db.SetSetting("key", "v2"); err != nil {
		t.Fatalf("set v2: %v", err)
	}

	val, err := db.GetSetting("key")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if val != "v2" {
		t.Errorf("got %q, want %q", val, "v2")
	}
}

func TestSettings_GetAll(t *testing.T) {
	db := testDB(t)

	db.SetSetting("a", "1")
	db.SetSetting("b", "2")

	all, err := db.GetAllSettings()
	if err != nil {
		t.Fatalf("get all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("got %d settings, want 2", len(all))
	}
	if all["a"] != "1" || all["b"] != "2" {
		t.Errorf("unexpected settings: %v", all)
	}
}

func TestSettings_HasSettings_Empty(t *testing.T) {
	db := testDB(t)

	has, err := db.HasSettings()
	if err != nil {
		t.Fatalf("has: %v", err)
	}
	if has {
		t.Error("expected false for empty db")
	}
}

func TestSettings_HasSettings_WithData(t *testing.T) {
	db := testDB(t)

	db.SetSetting("key", "val")

	has, err := db.HasSettings()
	if err != nil {
		t.Fatalf("has: %v", err)
	}
	if !has {
		t.Error("expected true after insert")
	}
}

func TestSettings_Delete(t *testing.T) {
	db := testDB(t)

	db.SetSetting("key", "val")
	if err := db.DeleteSetting("key"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	val, err := db.GetSetting("key")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if val != "" {
		t.Errorf("got %q after delete, want empty", val)
	}
}

// --- Tasks ---

func TestTasks_SaveAndGet(t *testing.T) {
	db := testDB(t)

	now := time.Now().UTC().Truncate(time.Second)
	task := &core.Task{
		ID:        "task-001",
		Issue:     core.Issue{Platform: "github", Repo: "owner/repo", ID: "42", Title: "Fix bug"},
		Branch:    "rig/issue-42",
		Status:    core.PhaseQueued,
		CreatedAt: now,
	}

	if err := db.SaveTask(task); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := db.GetTask("task-001")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected task, got nil")
	}
	if got.ID != "task-001" || got.Issue.ID != "42" || got.Status != core.PhaseQueued {
		t.Errorf("unexpected task: %+v", got)
	}
}

func TestTasks_GetMissing(t *testing.T) {
	db := testDB(t)

	got, err := db.GetTask("nonexistent")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestTasks_List(t *testing.T) {
	db := testDB(t)

	now := time.Now().UTC()
	for i, id := range []string{"task-001", "task-002", "task-003"} {
		task := &core.Task{
			ID:        id,
			Issue:     core.Issue{Platform: "github", Repo: "o/r", ID: id, Title: "T"},
			Status:    core.PhaseQueued,
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		}
		db.SaveTask(task)
	}

	tasks, err := db.ListTasks()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("got %d tasks, want 3", len(tasks))
	}
	// Should be ordered by created_at DESC
	if tasks[0].ID != "task-003" {
		t.Errorf("expected task-003 first, got %s", tasks[0].ID)
	}
}

func TestTasks_Upsert(t *testing.T) {
	db := testDB(t)

	now := time.Now().UTC()
	task := &core.Task{
		ID:        "task-001",
		Issue:     core.Issue{Platform: "github", Repo: "o/r", ID: "1", Title: "T"},
		Status:    core.PhaseQueued,
		CreatedAt: now,
	}
	db.SaveTask(task)

	// Update status
	task.Status = core.PhaseCompleted
	db.SaveTask(task)

	got, _ := db.GetTask("task-001")
	if got.Status != core.PhaseCompleted {
		t.Errorf("got status %s, want completed", got.Status)
	}
}

func TestTasks_IsInFlight(t *testing.T) {
	db := testDB(t)

	now := time.Now().UTC()
	task := &core.Task{
		ID:        "task-001",
		Issue:     core.Issue{Platform: "github", Repo: "o/r", ID: "42", Title: "T"},
		Status:    core.PhaseCoding,
		CreatedAt: now,
	}
	db.SaveTask(task)

	inFlight, err := db.IsInFlight("42")
	if err != nil {
		t.Fatalf("in-flight: %v", err)
	}
	if !inFlight {
		t.Error("expected in-flight for coding status")
	}

	// Completed task should not be in-flight
	task.Status = core.PhaseCompleted
	db.SaveTask(task)

	inFlight, _ = db.IsInFlight("42")
	if inFlight {
		t.Error("expected not in-flight for completed status")
	}
}

// --- Logs ---

func TestLogs_AppendAndGet(t *testing.T) {
	db := testDB(t)

	if err := db.AppendLog("task-001", "info", "started"); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := db.AppendLog("task-001", "error", "failed"); err != nil {
		t.Fatalf("append: %v", err)
	}

	logs, err := db.GetLogs("task-001")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("got %d logs, want 2", len(logs))
	}
	if logs[0].Level != "info" || logs[0].Message != "started" {
		t.Errorf("unexpected first log: %+v", logs[0])
	}
	if logs[1].Level != "error" || logs[1].Message != "failed" {
		t.Errorf("unexpected second log: %+v", logs[1])
	}
}

func TestLogs_GetSince(t *testing.T) {
	db := testDB(t)

	db.AppendLog("task-001", "info", "msg1")
	db.AppendLog("task-001", "info", "msg2")
	db.AppendLog("task-001", "info", "msg3")

	// Get all first to find the ID boundary
	all, _ := db.GetLogs("task-001")
	if len(all) != 3 {
		t.Fatalf("got %d logs, want 3", len(all))
	}

	// Get logs since ID of first entry
	since, err := db.GetLogsSince("task-001", all[0].ID)
	if err != nil {
		t.Fatalf("get since: %v", err)
	}
	if len(since) != 2 {
		t.Fatalf("got %d logs since, want 2", len(since))
	}
	if since[0].Message != "msg2" {
		t.Errorf("got %q, want msg2", since[0].Message)
	}
}

func TestLogs_IsolationByTask(t *testing.T) {
	db := testDB(t)

	db.AppendLog("task-001", "info", "a")
	db.AppendLog("task-002", "info", "b")

	logs1, _ := db.GetLogs("task-001")
	logs2, _ := db.GetLogs("task-002")

	if len(logs1) != 1 || logs1[0].Message != "a" {
		t.Errorf("task-001 logs: %+v", logs1)
	}
	if len(logs2) != 1 || logs2[0].Message != "b" {
		t.Errorf("task-002 logs: %+v", logs2)
	}
}

// --- Agents ---

func TestAgents_SetAndGet(t *testing.T) {
	db := testDB(t)

	if err := db.SetAgents("owner/repo", "# AGENTS\nsome content"); err != nil {
		t.Fatalf("set: %v", err)
	}

	content, err := db.GetAgents("owner/repo")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if content != "# AGENTS\nsome content" {
		t.Errorf("got %q", content)
	}
}

func TestAgents_GetMissing(t *testing.T) {
	db := testDB(t)

	content, err := db.GetAgents("nonexistent")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if content != "" {
		t.Errorf("expected empty, got %q", content)
	}
}

func TestAgents_List(t *testing.T) {
	db := testDB(t)

	db.SetAgents("owner/a", "content-a")
	db.SetAgents("owner/b", "content-b")

	agents, err := db.ListAgents()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("got %d agents, want 2", len(agents))
	}
	if agents["owner/a"] != "content-a" || agents["owner/b"] != "content-b" {
		t.Errorf("unexpected agents: %v", agents)
	}
}

// --- DB Lifecycle ---

func TestOpen_CreatesDir(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sub", "nested", "test.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// Verify directory was created
	if _, err := os.Stat(filepath.Dir(dbPath)); err != nil {
		t.Errorf("dir not created: %v", err)
	}
}

func TestOpen_CloseIdempotent(t *testing.T) {
	db := testDB(t)
	// Close should not panic when called multiple times
	db.Close()
	// t.Cleanup will call Close() again — should not panic
}
