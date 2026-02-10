package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testdataDir returns the absolute path to the testdata directory.
func testdataDir(t *testing.T) string {
	t.Helper()
	// Tests run from the package directory; testdata is at repo root.
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata"))
	if err != nil {
		t.Fatalf("failed to resolve testdata dir: %v", err)
	}
	return dir
}

func setEnvVars(t *testing.T) {
	t.Helper()
	t.Setenv("GITHUB_TOKEN", "test-github-token")
	t.Setenv("ANTHROPIC_API_KEY", "test-api-key")
	t.Setenv("WEBHOOK_SECRET", "test-secret")
}

func TestLoadValidConfig(t *testing.T) {
	setEnvVars(t)
	cfg, err := LoadConfig(filepath.Join(testdataDir(t), "valid.yaml"))
	if err != nil {
		t.Fatalf("expected valid config to load, got error: %v", err)
	}

	// Verify key fields
	if cfg.Project.Name != "test-app" {
		t.Errorf("project.name = %q, want %q", cfg.Project.Name, "test-app")
	}
	if cfg.Source.Platform != "github" {
		t.Errorf("source.platform = %q, want %q", cfg.Source.Platform, "github")
	}
	if cfg.Source.Token != "test-github-token" {
		t.Errorf("source.token = %q, want %q (env var substitution)", cfg.Source.Token, "test-github-token")
	}
	if cfg.AI.Provider != "anthropic" {
		t.Errorf("ai.provider = %q, want %q", cfg.AI.Provider, "anthropic")
	}
	if cfg.AI.MaxRetry != 3 {
		t.Errorf("ai.max_retry = %d, want %d", cfg.AI.MaxRetry, 3)
	}
	if cfg.Deploy.Method != "custom" {
		t.Errorf("deploy.method = %q, want %q", cfg.Deploy.Method, "custom")
	}
	if len(cfg.Deploy.Config.Commands) != 1 {
		t.Errorf("deploy.config.commands len = %d, want 1", len(cfg.Deploy.Config.Commands))
	}
	if len(cfg.Test) != 1 {
		t.Errorf("test len = %d, want 1", len(cfg.Test))
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("server.port = %d, want %d", cfg.Server.Port, 8080)
	}
	if cfg.Server.Secret != "test-secret" {
		t.Errorf("server.secret = %q, want %q", cfg.Server.Secret, "test-secret")
	}
}

func TestValidateMissingFields(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		wantErr string
	}{
		{
			name:    "missing deploy method",
			fixture: "missing_deploy.yaml",
			wantErr: "deploy.method",
		},
		{
			name:    "invalid platform",
			fixture: "invalid_platform.yaml",
			wantErr: "source.platform",
		},
		{
			name:    "max retry out of range",
			fixture: "max_retry_invalid.yaml",
			wantErr: "ai.max_retry",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set env vars so unresolved-var check doesn't interfere
			setEnvVars(t)
			_, err := LoadConfig(filepath.Join(testdataDir(t), tt.fixture))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidateMethodSpecific(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		wantErr string
	}{
		{
			name:    "custom no commands",
			fixture: "custom_no_commands.yaml",
			wantErr: "custom",
		},
		{
			name:    "terraform no dir",
			fixture: "terraform_no_dir.yaml",
			wantErr: "terraform",
		},
		{
			name:    "ansible missing inventory",
			fixture: "ansible_missing_inventory.yaml",
			wantErr: "ansible",
		},
		{
			name:    "k8s no manifest",
			fixture: "k8s_no_manifest.yaml",
			wantErr: "k8s",
		},
		{
			name:    "bad transport ssh missing fields",
			fixture: "bad_transport.yaml",
			wantErr: "ssh",
		},
		{
			name:    "rollback enabled no method",
			fixture: "rollback_enabled_no_method.yaml",
			wantErr: "rollback",
		},
		{
			name:    "test command missing run",
			fixture: "test_command_no_run.yaml",
			wantErr: "run",
		},
		{
			name:    "ai-verify missing tools",
			fixture: "ai_verify_no_tools.yaml",
			wantErr: "tools",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnvVars(t)
			_, err := LoadConfig(filepath.Join(testdataDir(t), tt.fixture))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestUnresolvedVariables(t *testing.T) {
	// Ensure the variable is NOT set
	os.Unsetenv("UNDEFINED_VAR_THAT_DOES_NOT_EXIST")

	_, err := LoadConfig(filepath.Join(testdataDir(t), "unresolved_var.yaml"))
	if err == nil {
		t.Fatal("expected error for unresolved variables, got nil")
	}
	if !strings.Contains(err.Error(), "unresolved") {
		t.Errorf("error = %q, want it to contain 'unresolved'", err.Error())
	}
	if !strings.Contains(err.Error(), "UNDEFINED_VAR_THAT_DOES_NOT_EXIST") {
		t.Errorf("error = %q, want it to contain variable name", err.Error())
	}
}

func TestLoadConfigFileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "failed to read file") {
		t.Errorf("error = %q, want it to contain 'failed to read file'", err.Error())
	}
}

func TestEnvVarSubstitution(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "my-secret-token")
	t.Setenv("ANTHROPIC_API_KEY", "my-api-key")
	t.Setenv("WEBHOOK_SECRET", "my-webhook-secret")

	cfg, err := LoadConfig(filepath.Join(testdataDir(t), "valid.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Source.Token != "my-secret-token" {
		t.Errorf("source.token = %q, want %q", cfg.Source.Token, "my-secret-token")
	}
	if cfg.AI.APIKey != "my-api-key" {
		t.Errorf("ai.api_key = %q, want %q", cfg.AI.APIKey, "my-api-key")
	}
	if cfg.Server.Secret != "my-webhook-secret" {
		t.Errorf("server.secret = %q, want %q", cfg.Server.Secret, "my-webhook-secret")
	}
}
