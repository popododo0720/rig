package deploy

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/rigdev/rig/internal/config"
	"github.com/rigdev/rig/internal/variable"
	"golang.org/x/crypto/ssh"
)

const (
	defaultTimeout = 5 * time.Minute
	maxRetry       = 10
	defaultSSHPort = 22
)

// CustomAdapter implements DeployAdapter using custom shell commands.
type CustomAdapter struct {
	commands []config.CustomCommand
	rollback []config.CustomCommand
}

// NewCustom creates a new CustomAdapter from deploy and rollback configs.
func NewCustom(cfg config.DeployMethodConfig, rollbackCfg config.DeployMethodConfig) (*CustomAdapter, error) {
	return &CustomAdapter{
		commands: cfg.Commands,
		rollback: rollbackCfg.Commands,
	}, nil
}

// Validate checks that all commands are properly configured.
func (a *CustomAdapter) Validate() error {
	allCmds := make([]config.CustomCommand, 0, len(a.commands)+len(a.rollback))
	allCmds = append(allCmds, a.commands...)
	allCmds = append(allCmds, a.rollback...)

	for _, cmd := range allCmds {
		if cmd.Name == "" {
			return fmt.Errorf("command missing name")
		}
		if cmd.Run == "" {
			return fmt.Errorf("command %q missing run", cmd.Name)
		}
		if cmd.Retry > maxRetry {
			return fmt.Errorf("command %q retry %d exceeds max %d", cmd.Name, cmd.Retry, maxRetry)
		}
		if cmd.Transport.Type == "ssh" {
			if err := validateSSH(cmd.Transport.SSH); err != nil {
				return fmt.Errorf("command %q ssh: %w", cmd.Name, err)
			}
		}
	}
	return nil
}

func validateSSH(cfg config.SSHConfig) error {
	if cfg.Host == "" {
		return fmt.Errorf("host is required")
	}
	if cfg.User == "" {
		return fmt.Errorf("user is required")
	}
	if cfg.Key == "" {
		return fmt.Errorf("key is required")
	}
	return nil
}

// Deploy executes all commands sequentially with variable resolution.
func (a *CustomAdapter) Deploy(ctx context.Context, vars map[string]string) (*Result, error) {
	return a.runCommands(ctx, a.commands, vars)
}

// Rollback executes all rollback commands sequentially.
func (a *CustomAdapter) Rollback(ctx context.Context) error {
	result, err := a.runCommands(ctx, a.rollback, nil)
	if err != nil {
		return err
	}
	if !result.Success {
		return fmt.Errorf("rollback failed: %s", result.Output)
	}
	return nil
}

// Status returns a stub status (not yet implemented).
func (a *CustomAdapter) Status(ctx context.Context) (*Status, error) {
	return &Status{Running: false}, nil
}

// runCommands executes a list of commands sequentially with retry logic.
func (a *CustomAdapter) runCommands(ctx context.Context, cmds []config.CustomCommand, vars map[string]string) (*Result, error) {
	start := time.Now()
	var allOutput strings.Builder

	for i, cmd := range cmds {
		resolved := variable.Resolve(cmd.Run, vars)

		timeout := cmd.Timeout
		if timeout == 0 {
			timeout = defaultTimeout
		}

		var lastErr error
		attempts := 1 + cmd.Retry // first attempt + retries

		for attempt := 0; attempt < attempts; attempt++ {
			cmdCtx, cancel := context.WithTimeout(ctx, timeout)

			var output string
			var err error
			if cmd.Transport.Type == "ssh" {
				output, err = a.executeSSH(cmdCtx, cmd, resolved)
			} else {
				output, err = a.executeLocal(cmdCtx, cmd, resolved)
			}
			cancel()

			if err == nil {
				allOutput.WriteString(output)
				lastErr = nil
				break
			}
			lastErr = err
		}

		if lastErr != nil {
			return &Result{
				Success:  false,
				Output:   fmt.Sprintf("failed at command %d (%s): %s", i+1, cmd.Name, lastErr.Error()),
				Duration: time.Since(start),
			}, lastErr
		}
	}

	return &Result{
		Success:  true,
		Output:   allOutput.String(),
		Duration: time.Since(start),
	}, nil
}

// executeLocal runs a command on the local machine.
func (a *CustomAdapter) executeLocal(ctx context.Context, cmd config.CustomCommand, resolved string) (string, error) {
	c := exec.CommandContext(ctx, "sh", "-c", resolved)

	// Ensure child processes are killed when context is cancelled.
	c.WaitDelay = 500 * time.Millisecond
	c.Cancel = func() error {
		return c.Process.Kill()
	}

	if cmd.Workdir != "" {
		c.Dir = cmd.Workdir
	}

	if len(cmd.Env) > 0 {
		c.Env = os.Environ()
		for k, v := range cmd.Env {
			c.Env = append(c.Env, k+"="+v)
		}
	}

	output, err := c.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("command timed out: %w", ctx.Err())
		}
		return "", fmt.Errorf("command failed: %w (output: %s)", err, string(output))
	}

	return string(output), nil
}

// executeSSH runs a command on a remote machine over SSH.
func (a *CustomAdapter) executeSSH(ctx context.Context, cmd config.CustomCommand, resolved string) (string, error) {
	keyBytes, err := os.ReadFile(cmd.Transport.SSH.Key)
	if err != nil {
		return "", fmt.Errorf("read ssh key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return "", fmt.Errorf("parse ssh key: %w", err)
	}

	sshConfig := &ssh.ClientConfig{
		User:            cmd.Transport.SSH.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	port := cmd.Transport.SSH.Port
	if port == 0 {
		port = defaultSSHPort
	}

	addr := fmt.Sprintf("%s:%d", cmd.Transport.SSH.Host, port)

	// Use a channel to handle context cancellation during dial
	type dialResult struct {
		client *ssh.Client
		err    error
	}
	ch := make(chan dialResult, 1)
	go func() {
		client, err := ssh.Dial("tcp", addr, sshConfig)
		ch <- dialResult{client, err}
	}()

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("ssh dial timed out: %w", ctx.Err())
	case res := <-ch:
		if res.err != nil {
			return "", fmt.Errorf("ssh dial: %w", res.err)
		}
		defer res.client.Close()

		session, err := res.client.NewSession()
		if err != nil {
			return "", fmt.Errorf("ssh session: %w", err)
		}
		defer session.Close()

		var buf bytes.Buffer
		session.Stdout = &buf
		session.Stderr = &buf

		// Run command with context cancellation
		done := make(chan error, 1)
		go func() {
			done <- session.Run(resolved)
		}()

		select {
		case <-ctx.Done():
			_ = session.Signal(ssh.SIGTERM)
			return "", fmt.Errorf("command timed out: %w", ctx.Err())
		case err := <-done:
			if err != nil {
				return "", fmt.Errorf("ssh command failed: %w (output: %s)", err, buf.String())
			}
			return buf.String(), nil
		}
	}
}
