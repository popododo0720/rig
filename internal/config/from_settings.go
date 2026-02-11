package config

import (
	"encoding/json"
	"fmt"
)

// FromSettings reconstructs a Config from a settings key-value map.
// Each key corresponds to a config section, and each value is a JSON blob.
// Environment variables in ${VAR} format are resolved before parsing.
func FromSettings(settings map[string]string) (*Config, error) {
	cfg := &Config{}

	// unmarshalSection resolves env vars in the JSON blob before unmarshalling.
	unmarshalSection := func(key string, target interface{}) error {
		v, ok := settings[key]
		if !ok || v == "" {
			return nil
		}
		resolved := ResolveEnvVars(v)
		if err := json.Unmarshal([]byte(resolved), target); err != nil {
			return fmt.Errorf("parse %s settings: %w", key, err)
		}
		return nil
	}

	if err := unmarshalSection("project", &cfg.Project); err != nil {
		return nil, err
	}
	if err := unmarshalSection("source", &cfg.Source); err != nil {
		return nil, err
	}
	if err := unmarshalSection("ai", &cfg.AI); err != nil {
		return nil, err
	}
	if err := unmarshalSection("deploy", &cfg.Deploy); err != nil {
		return nil, err
	}
	if err := unmarshalSection("test", &cfg.Test); err != nil {
		return nil, err
	}
	if err := unmarshalSection("workflow", &cfg.Workflow); err != nil {
		return nil, err
	}
	if err := unmarshalSection("notify", &cfg.Notify); err != nil {
		return nil, err
	}
	if err := unmarshalSection("server", &cfg.Server); err != nil {
		return nil, err
	}
	if err := unmarshalSection("projects", &cfg.Projects); err != nil {
		return nil, err
	}

	return cfg, nil
}
