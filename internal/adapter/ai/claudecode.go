package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/rigdev/rig/internal/config"
	"github.com/rigdev/rig/internal/core"
)

const defaultClaudeTimeout = 5 * time.Minute

// ClaudeCodeAdapter implements AIAdapter by shelling out to the claude CLI.
type ClaudeCodeAdapter struct {
	claudePath string
	model      string
	timeout    time.Duration
}

var _ core.AIAdapter = (*ClaudeCodeAdapter)(nil)

// NewClaudeCode creates a new ClaudeCodeAdapter.
// The claude CLI must be available on PATH.
func NewClaudeCode(cfg config.AIConfig) (*ClaudeCodeAdapter, error) {
	claudePath := "claude"
	if _, err := exec.LookPath(claudePath); err != nil {
		return nil, fmt.Errorf("claude-code: 'claude' CLI not found in PATH: %w", err)
	}
	return &ClaudeCodeAdapter{
		claudePath: claudePath,
		model:      cfg.Model,
		timeout:    defaultClaudeTimeout,
	}, nil
}

func (a *ClaudeCodeAdapter) AnalyzeIssue(ctx context.Context, issue *core.AIIssue, projectContext string) (*core.AIPlan, error) {
	if issue == nil {
		return nil, fmt.Errorf("claude-code: issue is nil")
	}

	body := issue.Body
	if strings.TrimSpace(body) == "" {
		body = "(no description provided)"
	}

	prompt := a.buildPrompt(
		buildSystemPrompt(projectContext),
		fmt.Sprintf(
			`Analyze the following issue and create an implementation plan.

Issue Title: %s
Issue Body:
%s

IMPORTANT: You MUST respond with ONLY a JSON object. No explanation, no markdown, no text before or after. Just the raw JSON:
{"summary": "what needs to be done", "steps": ["step 1", "step 2"]}`,
			issue.Title, body,
		),
	)

	body, err := a.runClaude(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("claude-code: analyze issue: %w", err)
	}

	return parsePlan(body)
}

func (a *ClaudeCodeAdapter) GenerateCode(ctx context.Context, plan *core.AIPlan, repoFiles map[string]string) ([]core.AIFileChange, error) {
	if plan == nil {
		return nil, fmt.Errorf("claude-code: plan is nil")
	}

	var filesSection strings.Builder
	for path, content := range repoFiles {
		filesSection.WriteString(fmt.Sprintf("--- %s ---\n%s\n", path, content))
	}

	prompt := a.buildPrompt(
		"You are a JSON API that returns file changes. You do NOT write files. You do NOT need permissions. You ONLY output a JSON array. No markdown. No explanation.",
		fmt.Sprintf(
			`Return a JSON array of file changes to implement this plan.

Plan: %s
Steps:
%s

Existing Files:
%s

RULES:
- Output ONLY a raw JSON array, nothing else
- No markdown fences, no explanation, no comments
- Each element: {"path": "file.go", "content": "full file content here", "action": "create"}
- action must be: create, modify, or delete
- content must contain the COMPLETE file content

[`,
			plan.Summary,
			formatSteps(plan.Steps),
			filesSection.String(),
		),
	)

	body, err := a.runClaude(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("claude-code: generate code: %w", err)
	}

	return parseFileChanges(body)
}

func (a *ClaudeCodeAdapter) AnalyzeFailure(ctx context.Context, logs string, currentCode map[string]string) ([]core.AIFileChange, error) {
	var codeSection strings.Builder
	for path, content := range currentCode {
		codeSection.WriteString(fmt.Sprintf("--- %s ---\n%s\n", path, content))
	}

	prompt := a.buildPrompt(
		"You are a debugging assistant. Analyze test/build failure logs and suggest code fixes. Always write clean, well-structured code. Output valid JSON only.",
		fmt.Sprintf(
			`Analyze the following test/build failure and suggest file changes to fix it.

Failure Logs:
%s

Current Code:
%s

IMPORTANT: You MUST respond with ONLY a JSON array. No explanation, no markdown, no text before or after. Just the raw JSON:
[{"path": "relative/file/path.go", "content": "full file content", "action": "create|modify|delete"}]`,
			logs,
			codeSection.String(),
		),
	)

	body, err := a.runClaude(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("claude-code: analyze failure: %w", err)
	}

	return parseFileChanges(body)
}

func (a *ClaudeCodeAdapter) AnalyzeDeployFailure(ctx context.Context, deployLogs string, infraFiles map[string]string) (*core.AIProposedFix, error) {
	var infraSection strings.Builder
	for path, content := range infraFiles {
		infraSection.WriteString(fmt.Sprintf("--- %s ---\n%s\n", path, content))
	}

	prompt := a.buildPrompt(
		"You are a DevOps assistant. Analyze deployment failure logs and infrastructure config files to diagnose the issue and suggest fixes. For each file change, explain WHY it needs to be modified.",
		fmt.Sprintf(
			`Analyze the following deployment failure and suggest infrastructure file changes to fix it.

Deploy Failure Logs:
%s

Infrastructure Files:
%s

IMPORTANT: You MUST respond with ONLY a JSON object. No explanation, no markdown, no text before or after. Just the raw JSON:
{"summary": "Brief summary", "reason": "Root cause", "changes": [{"path": "file.yml", "action": "modify", "reason": "why", "content": "full content"}]}`,
			deployLogs,
			infraSection.String(),
		),
	)

	body, err := a.runClaude(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("claude-code: analyze deploy failure: %w", err)
	}

	return parseProposedFix(body)
}

// buildPrompt combines system and user prompts for the claude CLI.
func (a *ClaudeCodeAdapter) buildPrompt(systemPrompt, userPrompt string) string {
	return fmt.Sprintf("%s\n\n%s", systemPrompt, userPrompt)
}

// runClaude executes the claude CLI with the given prompt and returns the text response.
func (a *ClaudeCodeAdapter) runClaude(ctx context.Context, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	args := []string{"-p", prompt, "--output-format", "json", "--max-turns", "1"}
	if a.model != "" {
		args = append(args, "--model", a.model)
	}
	cmd := exec.CommandContext(ctx, a.claudePath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("command failed: %w (stderr: %s)", err, stderr.String())
	}

	raw := stdout.Bytes()

	// Claude CLI with --output-format json wraps the result in a JSON envelope.
	var envelope struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope.Result != "" {
		return envelope.Result, nil
	}

	// Fallback: the output might be a JSON array of results.
	var results []struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(raw, &results); err == nil && len(results) > 0 {
		for _, r := range results {
			if r.Result != "" {
				return r.Result, nil
			}
		}
	}

	// Last fallback: return raw output as-is.
	return strings.TrimSpace(string(raw)), nil
}
