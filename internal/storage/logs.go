package storage

import "time"

// LogEntry represents a single log line for a task.
type LogEntry struct {
	ID        int64     `json:"id"`
	TaskID    string    `json:"task_id"`
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

// AppendLog adds a log entry for a task.
func (d *DB) AppendLog(taskID, level, message string) error {
	_, err := d.db.Exec(
		`INSERT INTO task_logs (task_id, timestamp, level, message) VALUES (?, datetime('now'), ?, ?)`,
		taskID, level, message,
	)
	return err
}

// GetLogs returns all log entries for a task, ordered by id.
func (d *DB) GetLogs(taskID string) ([]LogEntry, error) {
	rows, err := d.db.Query(
		`SELECT id, task_id, timestamp, level, message FROM task_logs WHERE task_id = ? ORDER BY id`,
		taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []LogEntry
	for rows.Next() {
		var l LogEntry
		if err := rows.Scan(&l.ID, &l.TaskID, &l.Timestamp, &l.Level, &l.Message); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

// GetLogsSince returns log entries after a given id (for polling).
func (d *DB) GetLogsSince(taskID string, afterID int64) ([]LogEntry, error) {
	rows, err := d.db.Query(
		`SELECT id, task_id, timestamp, level, message FROM task_logs WHERE task_id = ? AND id > ? ORDER BY id`,
		taskID, afterID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []LogEntry
	for rows.Next() {
		var l LogEntry
		if err := rows.Scan(&l.ID, &l.TaskID, &l.Timestamp, &l.Level, &l.Message); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}
