package deploy

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rigdev/rig/internal/config"
	"github.com/rigdev/rig/internal/core"
	"github.com/rigdev/rig/internal/variable"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	defaultTimeout = 5 * time.Minute
	maxRetry       = 10
	defaultSSHPort = 22
)

// CustomAdapter implements core.DeployAdapterIface using custom shell commands.
type CustomAdapter struct {
	commands []config.CustomCommand
	rollback []config.CustomCommand
}

var _ core.DeployAdapterIface = (*CustomAdapter)(nil)

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
	if cfg.Key == "" && cfg.Password == "" {
		return fmt.Errorf("key or password is required")
	}
	return nil
}

// Deploy executes all commands sequentially with variable resolution.
func (a *CustomAdapter) Deploy(ctx context.Context, vars map[string]string) (*core.AdapterDeployResult, error) {
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

// runCommands executes a list of commands sequentially with retry logic.
func (a *CustomAdapter) runCommands(ctx context.Context, cmds []config.CustomCommand, vars map[string]string) (*core.AdapterDeployResult, error) {
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
			return &core.AdapterDeployResult{
				Success:  false,
				Output:   fmt.Sprintf("failed at command %d (%s): %s", i+1, cmd.Name, lastErr.Error()),
				Duration: time.Since(start),
			}, lastErr
		}
	}

	return &core.AdapterDeployResult{
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
	authMethods := make([]ssh.AuthMethod, 0, 2)

	if cmd.Transport.SSH.Key != "" {
		keyPath, err := resolveSSHKeyPath(cmd.Transport.SSH.Key)
		if err != nil {
			return "", fmt.Errorf("resolve ssh key path: %w", err)
		}

		keyBytes, err := os.ReadFile(keyPath)
		if err != nil {
			return "", fmt.Errorf("read ssh key: %w", err)
		}

		signer, parseErr := ssh.ParsePrivateKey(keyBytes)
		if parseErr != nil {
			return "", fmt.Errorf("parse ssh key: %w", parseErr)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	if cmd.Transport.SSH.Password != "" {
		authMethods = append(authMethods, ssh.Password(cmd.Transport.SSH.Password))
	}

	if len(authMethods) == 0 {
		return "", fmt.Errorf("ssh auth requires key or password")
	}

	hostKeyCallback, err := buildHostKeyCallback(cmd.Transport.SSH)
	if err != nil {
		return "", fmt.Errorf("build host key callback: %w", err)
	}

	sshConfig := &ssh.ClientConfig{
		User:            cmd.Transport.SSH.User,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
	}

	dialTimeout := 30 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return "", fmt.Errorf("ssh dial timed out: %w", ctx.Err())
		}
		dialTimeout = remaining
	}
	sshConfig.Timeout = dialTimeout

	port := cmd.Transport.SSH.Port
	if port == 0 {
		port = defaultSSHPort
	}

	addr := net.JoinHostPort(cmd.Transport.SSH.Host, strconv.Itoa(port))

	conn, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("ssh dial timed out: %w", ctx.Err())
		}
		return "", fmt.Errorf("ssh dial: %w", err)
	}

	c, chans, reqs, err := ssh.NewClientConn(conn, addr, sshConfig)
	if err != nil {
		_ = conn.Close()
		if ctx.Err() != nil {
			return "", fmt.Errorf("ssh handshake timed out: %w", ctx.Err())
		}
		return "", fmt.Errorf("ssh handshake: %w", err)
	}
	client := ssh.NewClient(c, chans, reqs)
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("ssh session: %w", err)
	}
	defer session.Close()

	var buf bytes.Buffer
	session.Stdout = &buf
	session.Stderr = &buf

	// Run command with context cancellation.
	done := make(chan error, 1)
	go func() {
		done <- session.Run(resolved)
	}()

	select {
	case <-ctx.Done():
		_ = session.Close()
		<-done
		return "", fmt.Errorf("command timed out: %w", ctx.Err())
	case err := <-done:
		if err != nil {
			return "", fmt.Errorf("ssh command failed: %w (output: %s)", err, buf.String())
		}
		return buf.String(), nil
	}
}

// buildHostKeyCallback returns an ssh.HostKeyCallback based on SSHConfig.
// If KnownHosts is set, it uses the known_hosts file for verification.
// If KnownHosts is empty, it falls back to the default ~/.ssh/known_hosts.
// If no known_hosts file exists, it falls back to InsecureIgnoreHostKey with a warning.
func buildHostKeyCallback(cfg config.SSHConfig) (ssh.HostKeyCallback, error) {
	knownHostsPath := cfg.KnownHosts

	if knownHostsPath == "" {
		// Try default known_hosts locations.
		home, err := os.UserHomeDir()
		if err == nil {
			defaultPath := filepath.Join(home, ".ssh", "known_hosts")
			if _, statErr := os.Stat(defaultPath); statErr == nil {
				knownHostsPath = defaultPath
			}
		}
	} else {
		// Resolve ~ prefix.
		resolved, err := resolveSSHKeyPath(knownHostsPath)
		if err != nil {
			return nil, fmt.Errorf("resolve known_hosts path: %w", err)
		}
		knownHostsPath = resolved
	}

	if knownHostsPath == "" {
		// No known_hosts file found — fall back to insecure with log warning.
		return ssh.InsecureIgnoreHostKey(), nil
	}

	callback, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("parse known_hosts %s: %w", knownHostsPath, err)
	}

	// Wrap to handle the knownhosts.KeyError for unknown but not revoked keys.
	// If the host is completely unknown (no entry), we reject with a helpful message.
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := callback(hostname, remote, key)
		if err != nil {
			return fmt.Errorf("host key verification failed for %s: %w (add the host key to %s or set known_hosts: \"\" in config to skip verification)", hostname, err, knownHostsPath)
		}
		return nil
	}, nil
}

func resolveSSHKeyPath(path string) (string, error) {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("get user home dir: %w", err)
		}
		return home, nil
	}

	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("get user home dir: %w", err)
		}
		rest := path[2:]
		return filepath.Join(home, rest), nil
	}

	return path, nil
}
