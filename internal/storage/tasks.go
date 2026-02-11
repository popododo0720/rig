package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rigdev/rig/internal/core"
)

// SaveTask upserts a task. The full task is stored as JSON in the data column.
func (d *DB) SaveTask(task *core.Task) error {
	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}

	var completedAt *time.Time
	if task.CompletedAt != nil {
		completedAt = task.CompletedAt
	}

	_, err = d.db.Exec(
		`INSERT INTO tasks (id, issue_id, issue_repo, status, data, created_at, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			status = excluded.status,
			data = excluded.data,
			completed_at = excluded.completed_at`,
		task.ID, task.Issue.ID, task.Issue.Repo, string(task.Status), string(data),
		task.CreatedAt, completedAt,
	)
	if err != nil {
		return fmt.Errorf("save task %s: %w", task.ID, err)
	}
	return nil
}

// GetTask retrieves a task by its ID.
func (d *DB) GetTask(taskID string) (*core.Task, error) {
	var data string
	err := d.db.QueryRow("SELECT data FROM tasks WHERE id = ?", taskID).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get task %s: %w", taskID, err)
	}

	var task core.Task
	if err := json.Unmarshal([]byte(data), &task); err != nil {
		return nil, fmt.Errorf("unmarshal task %s: %w", taskID, err)
	}
	return &task, nil
}

// ListTasks returns all tasks ordered by creation time descending.
func (d *DB) ListTasks() ([]core.Task, error) {
	rows, err := d.db.Query("SELECT data FROM tasks ORDER BY created_at DESC")
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []core.Task
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		var task core.Task
		if err := json.Unmarshal([]byte(data), &task); err != nil {
			return nil, fmt.Errorf("unmarshal task: %w", err)
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

// IsInFlight returns true if the given issue already has a non-terminal task.
func (d *DB) IsInFlight(issueID string) (bool, error) {
	var count int
	err := d.db.QueryRow(
		`SELECT COUNT(*) FROM tasks WHERE issue_id = ? AND status NOT IN ('completed', 'failed', 'rollback')`,
		issueID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check in-flight %s: %w", issueID, err)
	}
	return count > 0, nil
}
