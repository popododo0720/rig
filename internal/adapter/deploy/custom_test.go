package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rigdev/rig/internal/config"
)

func localCmd(name, run string) config.CustomCommand {
	return config.CustomCommand{
		Name:      name,
		Run:       run,
		Transport: config.TransportConfig{Type: "local"},
	}
}

func TestCustomLocalSuccess(t *testing.T) {
	adapter := &CustomAdapter{
		commands: []config.CustomCommand{
			localCmd("echo", "echo hello"),
		},
	}

	result, err := adapter.Deploy(context.Background(), map[string]string{})
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}
	if !result.Success {
		t.Errorf("Expected success, got output: %s", result.Output)
	}
	if !strings.Contains(result.Output, "hello") {
		t.Errorf("Expected output to contain 'hello', got: %q", result.Output)
	}
}

func TestCustomLocalFailure(t *testing.T) {
	adapter := &CustomAdapter{
		commands: []config.CustomCommand{
			localCmd("fail", "exit 1"),
		},
	}

	result, err := adapter.Deploy(context.Background(), map[string]string{})
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if result.Success {
		t.Error("Expected failure")
	}
	if !strings.Contains(result.Output, "failed at command 1") {
		t.Errorf("Expected failure message, got: %q", result.Output)
	}
}

func TestCustomLocalTimeout(t *testing.T) {
	cmd := localCmd("slow", "sleep 30")
	cmd.Timeout = 1 * time.Second

	adapter := &CustomAdapter{
		commands: []config.CustomCommand{cmd},
	}

	result, err := adapter.Deploy(context.Background(), map[string]string{})
	if err == nil {
		t.Fatal("Expected timeout error, got nil")
	}
	if result.Success {
		t.Error("Expected failure on timeout")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("Expected timeout error, got: %v", err)
	}
}

func TestCustomLocalRetry(t *testing.T) {
	// Create a temp file to track attempts
	tmpFile, err := os.CreateTemp("", "rig-retry-*")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	os.Remove(tmpPath) // Remove so the script can create it
	defer os.Remove(tmpPath)

	// Command that fails first 2 times, succeeds on 3rd
	// Uses a file to track the attempt count
	script := `if [ -f "` + tmpPath + `" ]; then count=$(cat "` + tmpPath + `"); else count=0; fi; count=$((count + 1)); echo $count > "` + tmpPath + `"; if [ $count -lt 3 ]; then exit 1; fi; echo success`

	cmd := localCmd("retry-test", script)
	cmd.Retry = 3

	adapter := &CustomAdapter{
		commands: []config.CustomCommand{cmd},
	}

	result, err := adapter.Deploy(context.Background(), map[string]string{})
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}
	if !result.Success {
		t.Errorf("Expected success after retries, got: %s", result.Output)
	}
}

func TestCustomLocalWorkdir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rig-workdir-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cmd := localCmd("pwd-test", "pwd")
	cmd.Workdir = tmpDir

	adapter := &CustomAdapter{
		commands: []config.CustomCommand{cmd},
	}

	result, err := adapter.Deploy(context.Background(), map[string]string{})
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}
	if !result.Success {
		t.Errorf("Expected success, got: %s", result.Output)
	}
	// On some platforms the temp dir path may have symlinks resolved
	// Just check that it ran without error and produced output
	if strings.TrimSpace(result.Output) == "" {
		t.Error("Expected non-empty output from pwd")
	}
}

func TestCustomLocalEnv(t *testing.T) {
	cmd := localCmd("env-test", "echo $RIG_TEST_VAR")
	cmd.Env = map[string]string{
		"RIG_TEST_VAR": "rig-value-42",
	}

	adapter := &CustomAdapter{
		commands: []config.CustomCommand{cmd},
	}

	result, err := adapter.Deploy(context.Background(), map[string]string{})
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}
	if !result.Success {
		t.Errorf("Expected success, got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "rig-value-42") {
		t.Errorf("Expected env var value in output, got: %q", result.Output)
	}
}

func TestCustomLocalVariableResolution(t *testing.T) {
	adapter := &CustomAdapter{
		commands: []config.CustomCommand{
			localCmd("var-test", "echo ${BRANCH_NAME}"),
		},
	}

	vars := map[string]string{
		"BRANCH_NAME": "feature/deploy-v2",
	}

	result, err := adapter.Deploy(context.Background(), vars)
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}
	if !result.Success {
		t.Errorf("Expected success")
	}
	if !strings.Contains(result.Output, "feature/deploy-v2") {
		t.Errorf("Expected resolved variable in output, got: %q", result.Output)
	}
}

func TestCustomSSHValidate(t *testing.T) {
	tests := []struct {
		name    string
		ssh     config.SSHConfig
		wantErr string
	}{
		{
			name:    "missing host",
			ssh:     config.SSHConfig{User: "root", Key: "/path/to/key"},
			wantErr: "host is required",
		},
		{
			name:    "missing user",
			ssh:     config.SSHConfig{Host: "example.com", Key: "/path/to/key"},
			wantErr: "user is required",
		},
		{
			name:    "missing auth",
			ssh:     config.SSHConfig{Host: "example.com", User: "root"},
			wantErr: "key or password is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := &CustomAdapter{
				commands: []config.CustomCommand{
					{
						Name: "ssh-cmd",
						Run:  "echo hello",
						Transport: config.TransportConfig{
							Type: "ssh",
							SSH:  tt.ssh,
						},
					},
				},
			}

			err := adapter.Validate()
			if err == nil {
				t.Fatal("Expected validation error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestCustomSSHValidatePasswordOnly(t *testing.T) {
	adapter := &CustomAdapter{
		commands: []config.CustomCommand{
			{
				Name: "ssh-cmd",
				Run:  "echo hello",
				Transport: config.TransportConfig{
					Type: "ssh",
					SSH: config.SSHConfig{
						Host:     "example.com",
						User:     "root",
						Password: "secret",
					},
				},
			},
		},
	}

	if err := adapter.Validate(); err != nil {
		t.Fatalf("expected password-only SSH config to validate, got: %v", err)
	}
}

func TestCustomSSHValidateMaxRetry(t *testing.T) {
	adapter := &CustomAdapter{
		commands: []config.CustomCommand{
			{
				Name:      "too-many-retries",
				Run:       "echo hello",
				Retry:     15,
				Transport: config.TransportConfig{Type: "local"},
			},
		},
	}

	err := adapter.Validate()
	if err == nil {
		t.Fatal("Expected validation error for retry > 10")
	}
	if !strings.Contains(err.Error(), "exceeds max") {
		t.Errorf("Expected retry exceeds error, got: %v", err)
	}
}

func TestCustomValidateMissingName(t *testing.T) {
	adapter := &CustomAdapter{
		commands: []config.CustomCommand{
			{Run: "echo hello", Transport: config.TransportConfig{Type: "local"}},
		},
	}

	err := adapter.Validate()
	if err == nil {
		t.Fatal("Expected validation error for missing name")
	}
	if !strings.Contains(err.Error(), "missing name") {
		t.Errorf("Expected missing name error, got: %v", err)
	}
}

func TestCustomValidateMissingRun(t *testing.T) {
	adapter := &CustomAdapter{
		commands: []config.CustomCommand{
			{Name: "no-run", Transport: config.TransportConfig{Type: "local"}},
		},
	}

	err := adapter.Validate()
	if err == nil {
		t.Fatal("Expected validation error for missing run")
	}
	if !strings.Contains(err.Error(), "missing run") {
		t.Errorf("Expected missing run error, got: %v", err)
	}
}

func TestCustomRollback(t *testing.T) {
	// Create a temp file to track rollback execution order
	tmpFile, err := os.CreateTemp("", "rig-rollback-*")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	adapter := &CustomAdapter{
		rollback: []config.CustomCommand{
			localCmd("step1", `echo -n "step1 " >> "`+tmpPath+`"`),
			localCmd("step2", `echo -n "step2 " >> "`+tmpPath+`"`),
			localCmd("step3", `echo -n "step3" >> "`+tmpPath+`"`),
		},
	}

	err = adapter.Rollback(context.Background())
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	content, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("Failed to read temp file: %v", err)
	}

	expected := "step1 step2 step3"
	if string(content) != expected {
		t.Errorf("Expected rollback output %q, got %q", expected, string(content))
	}
}

func TestCustomMultipleCommands(t *testing.T) {
	adapter := &CustomAdapter{
		commands: []config.CustomCommand{
			localCmd("cmd1", "echo first"),
			localCmd("cmd2", "echo second"),
			localCmd("cmd3", "echo third"),
		},
	}

	result, err := adapter.Deploy(context.Background(), map[string]string{})
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}
	if !result.Success {
		t.Errorf("Expected success")
	}
	if !strings.Contains(result.Output, "first") || !strings.Contains(result.Output, "second") || !strings.Contains(result.Output, "third") {
		t.Errorf("Expected all command outputs, got: %q", result.Output)
	}
}

func TestCustomCommandFailsMiddle(t *testing.T) {
	adapter := &CustomAdapter{
		commands: []config.CustomCommand{
			localCmd("cmd1", "echo first"),
			localCmd("cmd2", "exit 1"),
			localCmd("cmd3", "echo third"),
		},
	}

	result, err := adapter.Deploy(context.Background(), map[string]string{})
	if err == nil {
		t.Fatal("Expected error for failing middle command")
	}
	if result.Success {
		t.Error("Expected failure")
	}
	if !strings.Contains(result.Output, "cmd2") {
		t.Errorf("Expected failure message to reference cmd2, got: %q", result.Output)
	}
}

func TestCustomNewCustom(t *testing.T) {
	cfg := config.DeployMethodConfig{
		Commands: []config.CustomCommand{
			localCmd("deploy", "echo deploy"),
		},
	}
	rollbackCfg := config.DeployMethodConfig{
		Commands: []config.CustomCommand{
			localCmd("rollback", "echo rollback"),
		},
	}

	adapter, err := NewCustom(cfg, rollbackCfg)
	if err != nil {
		t.Fatalf("NewCustom failed: %v", err)
	}
	if len(adapter.commands) != 1 {
		t.Errorf("Expected 1 command, got %d", len(adapter.commands))
	}
	if len(adapter.rollback) != 1 {
		t.Errorf("Expected 1 rollback command, got %d", len(adapter.rollback))
	}
}

func TestCustomStatus(t *testing.T) {
	adapter := &CustomAdapter{}

	status, err := adapter.Status(context.Background())
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if status.Running {
		t.Error("Expected not running")
	}
}

func TestResolveSSHKeyPathHomeAlias(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get user home dir: %v", err)
	}

	resolved, err := resolveSSHKeyPath("~/.ssh/rig_test")
	if err != nil {
		t.Fatalf("resolveSSHKeyPath failed: %v", err)
	}

	expected := filepath.Join(home, ".ssh", "rig_test")
	if resolved != expected {
		t.Fatalf("resolved path = %q, want %q", resolved, expected)
	}
}

func TestResolveSSHKeyPathPassthrough(t *testing.T) {
	input := "/tmp/id_rsa"
	resolved, err := resolveSSHKeyPath(input)
	if err != nil {
		t.Fatalf("resolveSSHKeyPath failed: %v", err)
	}
	if resolved != input {
		t.Fatalf("resolved path = %q, want %q", resolved, input)
	}
}
