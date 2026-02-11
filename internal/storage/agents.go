package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// GetAgents retrieves the AGENTS.md content for a project repo.
func (d *DB) GetAgents(projectRepo string) (string, error) {
	var content string
	err := d.db.QueryRow("SELECT content FROM project_agents WHERE project_repo = ?", projectRepo).Scan(&content)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get agents for %q: %w", projectRepo, err)
	}
	return content, nil
}

// SetAgents upserts the AGENTS.md content for a project repo.
func (d *DB) SetAgents(projectRepo, content string) error {
	_, err := d.db.Exec(
		`INSERT INTO project_agents (project_repo, content, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(project_repo) DO UPDATE SET content = excluded.content, updated_at = excluded.updated_at`,
		projectRepo, content, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("set agents for %q: %w", projectRepo, err)
	}
	return nil
}

// ListAgents returns all project repos that have AGENTS.md content.
func (d *DB) ListAgents() (map[string]string, error) {
	rows, err := d.db.Query("SELECT project_repo, content FROM project_agents")
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	agents := make(map[string]string)
	for rows.Next() {
		var repo, content string
		if err := rows.Scan(&repo, &content); err != nil {
			return nil, fmt.Errorf("scan agents: %w", err)
		}
		agents[repo] = content
	}
	return agents, rows.Err()
}
