//go:build integration
// +build integration

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

// TestE2E_SSHDeploy tests the deploy adapter against a real SSH server.
// Run with: go test -tags integration -run TestE2E ./internal/adapter/deploy/ -timeout 60s
//
// Requires:
//   - SSH key at ~/.ssh/rig_test
//   - Server at 192.168.35.123 accepting key auth for root
func TestE2E_SSHDeploy(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("get home dir: %v", err)
	}
	keyPath := filepath.Join(home, ".ssh", "rig_test")
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		t.Skipf("SSH key not found at %s, skipping E2E test", keyPath)
	}

	sshCfg := config.SSHConfig{
		Host: "192.168.35.123",
		Port: 22,
		User: "root",
		Key:  "~/.ssh/rig_test",
	}

	t.Run("deploy_create_file", func(t *testing.T) {
		deployCfg := config.DeployMethodConfig{
			Commands: []config.CustomCommand{
				{
					Name:    "create-marker",
					Run:     "echo 'rig-e2e-test' > /tmp/rig_deploy_test.txt && cat /tmp/rig_deploy_test.txt",
					Timeout: 30 * time.Second,
					Transport: config.TransportConfig{
						Type: "ssh",
						SSH:  sshCfg,
					},
				},
			},
		}

		adapter, err := NewCustom(deployCfg, config.DeployMethodConfig{})
		if err != nil {
			t.Fatalf("create adapter: %v", err)
		}
		if err := adapter.Validate(); err != nil {
			t.Fatalf("validate: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := adapter.Deploy(ctx, nil)
		if err != nil {
			t.Fatalf("deploy failed: %v", err)
		}
		if !result.Success {
			t.Fatalf("deploy not successful: %s", result.Output)
		}
		if result.Duration <= 0 {
			t.Errorf("expected positive duration, got %s", result.Duration)
		}
		t.Logf("deploy output: %s", result.Output)
		t.Logf("deploy duration: %s", result.Duration)
	})

	t.Run("deploy_verify_file", func(t *testing.T) {
		verifyCfg := config.DeployMethodConfig{
			Commands: []config.CustomCommand{
				{
					Name:    "verify-marker",
					Run:     "test -f /tmp/rig_deploy_test.txt && cat /tmp/rig_deploy_test.txt",
					Timeout: 15 * time.Second,
					Transport: config.TransportConfig{
						Type: "ssh",
						SSH:  sshCfg,
					},
				},
			},
		}

		adapter, err := NewCustom(verifyCfg, config.DeployMethodConfig{})
		if err != nil {
			t.Fatalf("create adapter: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		result, err := adapter.Deploy(ctx, nil)
		if err != nil {
			t.Fatalf("verify failed: %v", err)
		}
		if !result.Success {
			t.Fatalf("verify not successful: %s", result.Output)
		}
		t.Logf("verified file content: %s", result.Output)
	})

	t.Run("deploy_with_variables", func(t *testing.T) {
		// Use printf %s to write a file, then cat it. This avoids shell interpretation.
		// variable.Resolve replaces ${VAR} patterns before the command reaches the remote shell.
		varsCfg := config.DeployMethodConfig{
			Commands: []config.CustomCommand{
				{
					Name:    "write-vars",
					Run:     "printf '%s' 'branch=${BRANCH_NAME} issue=${ISSUE_ID}' > /tmp/rig_vars_test.txt",
					Timeout: 15 * time.Second,
					Transport: config.TransportConfig{
						Type: "ssh",
						SSH:  sshCfg,
					},
				},
				{
					Name:    "read-vars",
					Run:     "cat /tmp/rig_vars_test.txt && rm /tmp/rig_vars_test.txt",
					Timeout: 15 * time.Second,
					Transport: config.TransportConfig{
						Type: "ssh",
						SSH:  sshCfg,
					},
				},
			},
		}

		adapter, err := NewCustom(varsCfg, config.DeployMethodConfig{})
		if err != nil {
			t.Fatalf("create adapter: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		vars := map[string]string{
			"BRANCH_NAME": "rig-issue-42",
			"ISSUE_ID":    "42",
		}
		result, err := adapter.Deploy(ctx, vars)
		if err != nil {
			t.Fatalf("deploy with vars failed: %v", err)
		}
		if !result.Success {
			t.Fatalf("not successful: %s", result.Output)
		}
		t.Logf("variable-resolved output: [%s]", result.Output)

		// Verify variable resolution happened
		expected := "branch=rig-issue-42 issue=42"
		if !strings.Contains(result.Output, expected) {
			t.Errorf("expected output to contain %q, got %q", expected, result.Output)
		}
	})

	t.Run("deploy_multi_command_sequence", func(t *testing.T) {
		multiCfg := config.DeployMethodConfig{
			Commands: []config.CustomCommand{
				{
					Name:    "step-1-create-dir",
					Run:     "mkdir -p /tmp/rig_e2e && echo step1-ok",
					Timeout: 15 * time.Second,
					Transport: config.TransportConfig{
						Type: "ssh",
						SSH:  sshCfg,
					},
				},
				{
					Name:    "step-2-write-file",
					Run:     "echo 'deployed-by-rig' > /tmp/rig_e2e/app.txt && echo step2-ok",
					Timeout: 15 * time.Second,
					Transport: config.TransportConfig{
						Type: "ssh",
						SSH:  sshCfg,
					},
				},
				{
					Name:    "step-3-verify",
					Run:     "cat /tmp/rig_e2e/app.txt && ls -la /tmp/rig_e2e/",
					Timeout: 15 * time.Second,
					Transport: config.TransportConfig{
						Type: "ssh",
						SSH:  sshCfg,
					},
				},
			},
		}

		adapter, err := NewCustom(multiCfg, config.DeployMethodConfig{})
		if err != nil {
			t.Fatalf("create adapter: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		result, err := adapter.Deploy(ctx, nil)
		if err != nil {
			t.Fatalf("multi-command deploy failed: %v", err)
		}
		if !result.Success {
			t.Fatalf("not successful: %s", result.Output)
		}
		t.Logf("multi-command output:\n%s", result.Output)
	})

	t.Run("rollback", func(t *testing.T) {
		rollbackCfg := config.DeployMethodConfig{
			Commands: []config.CustomCommand{
				{
					Name:    "cleanup",
					Run:     "rm -rf /tmp/rig_e2e /tmp/rig_deploy_test.txt && echo rollback-ok",
					Timeout: 15 * time.Second,
					Transport: config.TransportConfig{
						Type: "ssh",
						SSH:  sshCfg,
					},
				},
			},
		}

		adapter, err := NewCustom(config.DeployMethodConfig{}, rollbackCfg)
		if err != nil {
			t.Fatalf("create adapter: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		err = adapter.Rollback(ctx)
		if err != nil {
			t.Fatalf("rollback failed: %v", err)
		}
		t.Log("rollback completed successfully")
	})

	t.Run("verify_cleanup", func(t *testing.T) {
		cleanupVerifyCfg := config.DeployMethodConfig{
			Commands: []config.CustomCommand{
				{
					Name:    "check-removed",
					Run:     "test ! -f /tmp/rig_deploy_test.txt && test ! -d /tmp/rig_e2e && echo cleanup-verified",
					Timeout: 15 * time.Second,
					Transport: config.TransportConfig{
						Type: "ssh",
						SSH:  sshCfg,
					},
				},
			},
		}

		adapter, err := NewCustom(cleanupVerifyCfg, config.DeployMethodConfig{})
		if err != nil {
			t.Fatalf("create adapter: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		result, err := adapter.Deploy(ctx, nil)
		if err != nil {
			t.Fatalf("cleanup verify failed: %v", err)
		}
		if !result.Success {
			t.Fatalf("cleanup verification failed: %s", result.Output)
		}
		t.Log("confirmed: all test artifacts cleaned up on remote server")
	})
}
