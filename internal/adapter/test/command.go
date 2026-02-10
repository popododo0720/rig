package test

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/rigdev/rig/internal/config"
	"github.com/rigdev/rig/internal/core"
	"github.com/rigdev/rig/internal/variable"
)

// CommandRunner executes test commands via shell and checks exit codes.
type CommandRunner struct {
	cfg config.TestConfig
}

var _ core.TestRunnerIface = (*CommandRunner)(nil)

// NewCommandRunner creates a CommandRunner from a test configuration.
func NewCommandRunner(cfg config.TestConfig) *CommandRunner {
	return &CommandRunner{cfg: cfg}
}

// Run executes the configured test command.
// Exit code 0 = pass, non-zero = fail.
// It resolves variables in the command string before execution.
func (r *CommandRunner) Run(ctx context.Context, vars map[string]string) (*core.TestResult, error) {
	// Resolve variables in the command string.
	command := variable.Resolve(r.cfg.Run, vars)

	// Apply timeout if configured.
	if r.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.cfg.Timeout)
		defer cancel()
	}

	start := time.Now()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Cancel = func() error { return cmd.Process.Kill() }
	cmd.WaitDelay = 3 * time.Second

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	duration := time.Since(start)

	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\n--- stderr ---\n" + stderr.String()
	}

	result := &core.TestResult{
		Name:     r.cfg.Name,
		Type:     r.cfg.Type,
		Duration: duration,
		Output:   output,
	}

	if err != nil {
		// Check if it was a timeout.
		if ctx.Err() == context.DeadlineExceeded {
			result.Passed = false
			result.Output = fmt.Sprintf("test timed out after %s\n%s", r.cfg.Timeout, output)
			return result, nil
		}
		// Non-zero exit code.
		result.Passed = false
		return result, nil
	}

	result.Passed = true
	return result, nil
}
