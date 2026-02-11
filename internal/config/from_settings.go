package config

import (
	"encoding/json"
	"fmt"
)

// FromSettings reconstructs a Config from a settings key-value map.
// Each key corresponds to a config section, and each value is a JSON blob.
func FromSettings(settings map[string]string) (*Config, error) {
	cfg := &Config{}

	if v, ok := settings["project"]; ok && v != "" {
		if err := json.Unmarshal([]byte(v), &cfg.Project); err != nil {
			return nil, fmt.Errorf("parse project settings: %w", err)
		}
	}

	if v, ok := settings["source"]; ok && v != "" {
		if err := json.Unmarshal([]byte(v), &cfg.Source); err != nil {
			return nil, fmt.Errorf("parse source settings: %w", err)
		}
	}

	if v, ok := settings["ai"]; ok && v != "" {
		if err := json.Unmarshal([]byte(v), &cfg.AI); err != nil {
			return nil, fmt.Errorf("parse ai settings: %w", err)
		}
	}

	if v, ok := settings["deploy"]; ok && v != "" {
		if err := json.Unmarshal([]byte(v), &cfg.Deploy); err != nil {
			return nil, fmt.Errorf("parse deploy settings: %w", err)
		}
	}

	if v, ok := settings["test"]; ok && v != "" {
		if err := json.Unmarshal([]byte(v), &cfg.Test); err != nil {
			return nil, fmt.Errorf("parse test settings: %w", err)
		}
	}

	if v, ok := settings["workflow"]; ok && v != "" {
		if err := json.Unmarshal([]byte(v), &cfg.Workflow); err != nil {
			return nil, fmt.Errorf("parse workflow settings: %w", err)
		}
	}

	if v, ok := settings["notify"]; ok && v != "" {
		if err := json.Unmarshal([]byte(v), &cfg.Notify); err != nil {
			return nil, fmt.Errorf("parse notify settings: %w", err)
		}
	}

	if v, ok := settings["server"]; ok && v != "" {
		if err := json.Unmarshal([]byte(v), &cfg.Server); err != nil {
			return nil, fmt.Errorf("parse server settings: %w", err)
		}
	}

	if v, ok := settings["projects"]; ok && v != "" {
		if err := json.Unmarshal([]byte(v), &cfg.Projects); err != nil {
			return nil, fmt.Errorf("parse projects settings: %w", err)
		}
	}

	return cfg, nil
}
