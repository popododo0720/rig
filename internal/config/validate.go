package config

import (
	"fmt"
	"strings"
)

// validPlatforms is the set of supported source platforms.
var validPlatforms = map[string]bool{
	"github":    true,
	"gitlab":    true,
	"bitbucket": true,
	"gitea":     true,
}

// validDeployMethods is the set of supported deploy methods.
var validDeployMethods = map[string]bool{
	"custom":         true,
	"docker-compose": true,
	"terraform":      true,
	"ansible":        true,
	"k8s":            true,
}

// Validate checks the Config for completeness and correctness.
// It returns the first error encountered, prefixed with "config: ".
func Validate(cfg *Config) error {
	var errs []string

	// --- Required fields ---
	if cfg.Project.Name == "" {
		errs = append(errs, "config: project.name is required")
	}
	if cfg.Source.Platform == "" {
		errs = append(errs, "config: source.platform is required")
	}
	if cfg.Source.Repo == "" {
		errs = append(errs, "config: source.repo is required")
	}
	if cfg.AI.Provider == "" {
		errs = append(errs, "config: ai.provider is required")
	}
	if cfg.AI.Model == "" {
		errs = append(errs, "config: ai.model is required")
	}
	if cfg.Deploy.Method == "" {
		errs = append(errs, "config: deploy.method is required")
	}

	// --- Platform validation ---
	if cfg.Source.Platform != "" && !validPlatforms[cfg.Source.Platform] {
		errs = append(errs, fmt.Sprintf(
			"config: source.platform '%s' is invalid; must be one of: github, gitlab, bitbucket, gitea",
			cfg.Source.Platform))
	}

	// --- AI max_retry range ---
	if cfg.AI.MaxRetry != 0 && (cfg.AI.MaxRetry < 1 || cfg.AI.MaxRetry > 10) {
		errs = append(errs, fmt.Sprintf(
			"config: ai.max_retry must be between 1 and 10, got %d",
			cfg.AI.MaxRetry))
	}

	// --- Deploy method validation ---
	if cfg.Deploy.Method != "" && !validDeployMethods[cfg.Deploy.Method] {
		errs = append(errs, fmt.Sprintf(
			"config: deploy.method '%s' is invalid; must be one of: custom, docker-compose, terraform, ansible, k8s",
			cfg.Deploy.Method))
	}

	// --- Deploy method-specific requirements ---
	if cfg.Deploy.Method != "" {
		errs = append(errs, validateDeployMethod(cfg.Deploy.Method, &cfg.Deploy.Config)...)
	}

	// --- Custom command validation ---
	if cfg.Deploy.Method == "custom" {
		for i, cmd := range cfg.Deploy.Config.Commands {
			errs = append(errs, validateCustomCommand(i, &cmd)...)
		}
	}

	// --- Rollback validation ---
	errs = append(errs, validateRollback(&cfg.Deploy.Rollback)...)

	// --- Test validation ---
	for i, t := range cfg.Test {
		errs = append(errs, validateTest(i, &t)...)
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// validateDeployMethod checks method-specific required fields.
func validateDeployMethod(method string, dc *DeployMethodConfig) []string {
	var errs []string
	switch method {
	case "custom":
		if len(dc.Commands) == 0 {
			errs = append(errs, "config: deploy.method 'custom' requires at least one command in 'config.commands'")
		}
	case "docker-compose":
		if dc.File == "" {
			errs = append(errs, "config: deploy.method 'docker-compose' requires 'file' field")
		}
	case "terraform":
		if dc.Dir == "" {
			errs = append(errs, "config: deploy.method 'terraform' requires 'dir' field")
		}
	case "ansible":
		if dc.Playbook == "" {
			errs = append(errs, "config: deploy.method 'ansible' requires 'playbook' field")
		}
		if dc.Inventory == "" {
			errs = append(errs, "config: deploy.method 'ansible' requires 'inventory' field")
		}
	case "k8s":
		if dc.Manifest == "" {
			errs = append(errs, "config: deploy.method 'k8s' requires 'manifest' field")
		}
	}
	return errs
}

// validateCustomCommand checks a single custom command.
func validateCustomCommand(idx int, cmd *CustomCommand) []string {
	var errs []string
	prefix := fmt.Sprintf("config: deploy.config.commands[%d]", idx)

	if cmd.Name == "" {
		errs = append(errs, prefix+".name is required")
	}
	if cmd.Run == "" {
		errs = append(errs, prefix+".run is required")
	}
	if cmd.Retry < 0 || cmd.Retry > 10 {
		errs = append(errs, fmt.Sprintf("%s.retry must be between 0 and 10, got %d", prefix, cmd.Retry))
	}

	// Transport validation
	if cmd.Transport.Type == "ssh" {
		sshPrefix := prefix + ".transport.ssh"
		if cmd.Transport.SSH.Host == "" {
			errs = append(errs, sshPrefix+".host is required when transport type is 'ssh'")
		}
		if cmd.Transport.SSH.User == "" {
			errs = append(errs, sshPrefix+".user is required when transport type is 'ssh'")
		}
		if cmd.Transport.SSH.Key == "" {
			errs = append(errs, sshPrefix+".key is required when transport type is 'ssh'")
		}
	}

	return errs
}

// validateRollback checks rollback configuration.
func validateRollback(rb *RollbackConfig) []string {
	var errs []string
	if !rb.Enabled {
		return nil
	}
	if rb.Method == "" {
		errs = append(errs, "config: deploy.rollback requires 'method' when enabled")
	}
	if rb.Method == "custom" && len(rb.Config.Commands) == 0 {
		errs = append(errs, "config: deploy.rollback method 'custom' requires 'config.commands'")
	}
	return errs
}

// validateTest checks a single test configuration.
func validateTest(idx int, t *TestConfig) []string {
	var errs []string
	prefix := fmt.Sprintf("config: test[%d]", idx)

	switch t.Type {
	case "command":
		if t.Run == "" {
			errs = append(errs, prefix+".run is required for type 'command'")
		}
	case "ai-verify":
		if t.Name == "" {
			errs = append(errs, prefix+".name is required for type 'ai-verify'")
		}
		if t.Prompt == "" {
			errs = append(errs, prefix+".prompt is required for type 'ai-verify'")
		}
		if len(t.Tools) == 0 {
			errs = append(errs, prefix+".tools requires at least one tool for type 'ai-verify'")
		}
	}
	return errs
}
