package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DB wraps a SQLite database connection.
type DB struct {
	db *sql.DB
}

// Open opens or creates a SQLite database at the given path and runs migrations.
func Open(dbPath string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	d := &DB{db: db}
	if err := d.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return d, nil
}

// Close closes the database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS settings (
		key        TEXT PRIMARY KEY,
		value      TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS tasks (
		id           TEXT PRIMARY KEY,
		issue_id     TEXT NOT NULL,
		issue_repo   TEXT NOT NULL DEFAULT '',
		status       TEXT NOT NULL,
		data         TEXT NOT NULL,
		created_at   DATETIME NOT NULL,
		completed_at DATETIME
	);

	CREATE INDEX IF NOT EXISTS idx_tasks_issue_status ON tasks(issue_id, status);
	CREATE INDEX IF NOT EXISTS idx_tasks_created ON tasks(created_at DESC);

	CREATE TABLE IF NOT EXISTS project_agents (
		project_repo TEXT PRIMARY KEY,
		content      TEXT NOT NULL DEFAULT '',
		updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS task_logs (
		id        INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id   TEXT NOT NULL,
		timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		level     TEXT NOT NULL DEFAULT 'info',
		message   TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_task_logs_task ON task_logs(task_id, id);
	`

	_, err := d.db.Exec(schema)
	return err
}
