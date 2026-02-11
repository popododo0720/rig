package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// envVarPattern matches ${VAR_NAME} patterns in config content.
var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// ResolveEnvVars substitutes ${VAR_NAME} patterns with os.Getenv(VAR_NAME).
// Unresolved variables (env var not set) are left as-is without error.
func ResolveEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		varName := match[2 : len(match)-1]
		if val, ok := os.LookupEnv(varName); ok {
			return val
		}
		return match // leave unresolved as-is
	})
}

// LoadConfig reads a YAML configuration file, substitutes environment
// variables, parses into Config, and validates the result.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: failed to read file %s: %w", path, err)
	}

	// Check for unresolved variables — any ${VAR} where the env var is not set.
	if err := validateEnvVars(data); err != nil {
		return nil, err
	}

	// Substitute ${VAR_NAME} with os.Getenv(VAR_NAME)
	resolved := envVarPattern.ReplaceAllStringFunc(string(data), func(match string) string {
		varName := match[2 : len(match)-1] // strip ${ and }
		return os.Getenv(varName)
	})

	var cfg Config
	if err := yaml.Unmarshal([]byte(resolved), &cfg); err != nil {
		return nil, fmt.Errorf("config: failed to parse YAML: %w", err)
	}

	if err := Validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// validateEnvVars checks that all ${VAR} references in raw data
// correspond to environment variables that are actually set.
func validateEnvVars(data []byte) error {
	matches := envVarPattern.FindAllStringSubmatch(string(data), -1)
	var unresolved []string
	seen := map[string]bool{}
	for _, m := range matches {
		varName := m[1]
		if seen[varName] {
			continue
		}
		seen[varName] = true
		if _, ok := os.LookupEnv(varName); !ok {
			unresolved = append(unresolved, "${"+varName+"}")
		}
	}
	if len(unresolved) > 0 {
		return fmt.Errorf("config: unresolved variables found: %s",
			strings.Join(unresolved, ", "))
	}
	return nil
}
