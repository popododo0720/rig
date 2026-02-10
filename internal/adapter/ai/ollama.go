package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/rigdev/rig/internal/config"
	"github.com/rigdev/rig/internal/core"
)

const (
	defaultOllamaURL     = "http://localhost:11434/v1/chat/completions"
	defaultOllamaTimeout = 120 * time.Second
)

// OllamaAdapter implements AIAdapter using Ollama's OpenAI-compatible chat API.
type OllamaAdapter struct {
	apiKey   string
	model    string
	endpoint string
	client   *http.Client
}

var _ core.AIAdapter = (*OllamaAdapter)(nil)

// NewOllama creates a new OllamaAdapter from the AI config.
func NewOllama(cfg config.AIConfig) (*OllamaAdapter, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("ollama: model is required")
	}

	endpoint := strings.TrimSpace(os.Getenv("OLLAMA_API_ENDPOINT"))
	if endpoint == "" {
		endpoint = defaultOllamaURL
	}

	return &OllamaAdapter{
		apiKey:   cfg.APIKey,
		model:    cfg.Model,
		endpoint: endpoint,
		client:   &http.Client{Timeout: defaultOllamaTimeout},
	}, nil
}

// AnalyzeIssue sends the issue to Ollama and parses a Plan from the response.
func (a *OllamaAdapter) AnalyzeIssue(ctx context.Context, issue *core.AIIssue, projectContext string) (*core.AIPlan, error) {
	if issue == nil {
		return nil, fmt.Errorf("ollama: issue is nil")
	}

	systemPrompt := buildSystemPrompt(projectContext)
	userPrompt := fmt.Sprintf(
		`Analyze the following issue and create an implementation plan.

Issue Title: %s
Issue Body:
%s

Respond in the following JSON format ONLY (no markdown fences, no extra text):
{
  "summary": "Brief summary of what needs to be done",
  "steps": ["Step 1 description", "Step 2 description"]
}`,
		issue.Title, issue.Body,
	)

	body, err := a.sendMessage(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("ollama: analyze issue: %w", err)
	}

	return parsePlan(body)
}

// GenerateCode sends the plan and repo files to Ollama and parses FileChange list.
func (a *OllamaAdapter) GenerateCode(ctx context.Context, plan *core.AIPlan, repoFiles map[string]string) ([]core.AIFileChange, error) {
	if plan == nil {
		return nil, fmt.Errorf("ollama: plan is nil")
	}

	systemPrompt := "You are a code generation assistant. Generate file changes to implement the given plan. Output valid JSON only."

	var filesSection strings.Builder
	for path, content := range repoFiles {
		filesSection.WriteString(fmt.Sprintf("--- %s ---\n%s\n", path, content))
	}

	userPrompt := fmt.Sprintf(
		`Implement the following plan by generating file changes.

Plan Summary: %s
Steps:
%s

Existing Files:
%s

Respond in the following JSON format ONLY (no markdown fences, no extra text):
[
  {"path": "relative/file/path.go", "content": "full file content", "action": "create|modify|delete"}
]`,
		plan.Summary,
		formatSteps(plan.Steps),
		filesSection.String(),
	)

	body, err := a.sendMessage(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("ollama: generate code: %w", err)
	}

	return parseFileChanges(body)
}

// AnalyzeFailure sends test/build logs and current code to Ollama for fix suggestions.
func (a *OllamaAdapter) AnalyzeFailure(ctx context.Context, logs string, currentCode map[string]string) ([]core.AIFileChange, error) {
	systemPrompt := "You are a debugging assistant. Analyze test/build failure logs and suggest code fixes. Output valid JSON only."

	var codeSection strings.Builder
	for path, content := range currentCode {
		codeSection.WriteString(fmt.Sprintf("--- %s ---\n%s\n", path, content))
	}

	userPrompt := fmt.Sprintf(
		`Analyze the following test/build failure and suggest file changes to fix it.

Failure Logs:
%s

Current Code:
%s

Respond in the following JSON format ONLY (no markdown fences, no extra text):
[
  {"path": "relative/file/path.go", "content": "full file content", "action": "create|modify|delete"}
]`,
		logs,
		codeSection.String(),
	)

	body, err := a.sendMessage(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("ollama: analyze failure: %w", err)
	}

	return parseFileChanges(body)
}

// AnalyzeDeployFailure sends deploy logs and infra files to Ollama for deploy fix suggestions.
func (a *OllamaAdapter) AnalyzeDeployFailure(ctx context.Context, deployLogs string, infraFiles map[string]string) (*core.AIProposedFix, error) {
	systemPrompt := "You are a DevOps assistant. Analyze deployment failure logs and infrastructure config files to diagnose the issue and suggest fixes. For each file change, explain WHY it needs to be modified."

	var infraSection strings.Builder
	for path, content := range infraFiles {
		infraSection.WriteString(fmt.Sprintf("--- %s ---\n%s\n", path, content))
	}

	userPrompt := fmt.Sprintf(
		`Analyze the following deployment failure and suggest infrastructure file changes to fix it.

Deploy Failure Logs:
%s

Infrastructure Files:
%s

Respond in the following JSON format ONLY (no markdown fences, no extra text):
{
  "summary": "Brief summary of the deployment issue",
  "reason": "Root cause analysis",
  "changes": [
    {"path": "ansible/playbook.yml", "action": "modify", "reason": "Port mismatch", "content": "full content..."}
  ]
}`,
		deployLogs,
		infraSection.String(),
	)

	body, err := a.sendMessage(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("ollama: analyze deploy failure: %w", err)
	}

	return parseProposedFix(body)
}

// ollamaRequest is the OpenAI-compatible chat completions request body.
type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  ollamaOptions   `json:"options"`
}

// ollamaMessage is a single chat message.
type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ollamaOptions controls model behavior.
type ollamaOptions struct {
	Temperature int `json:"temperature"`
}

// ollamaResponse is the OpenAI-compatible response from Ollama.
type ollamaResponse struct {
	Choices []ollamaChoice `json:"choices"`
	Error   *ollamaError   `json:"error,omitempty"`
}

// ollamaChoice is one generated completion choice.
type ollamaChoice struct {
	Message ollamaMessage `json:"message"`
}

// ollamaError represents an API error response.
type ollamaError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// sendMessage posts a prompt to Ollama and returns the first message content.
func (a *OllamaAdapter) sendMessage(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	reqBody := ollamaRequest{
		Model: a.model,
		Messages: []ollamaMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Stream:  false,
		Options: ollamaOptions{Temperature: 0},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint, bytes.NewReader(jsonData))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if a.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		if isConnectionRefused(err) {
			return "", fmt.Errorf("cannot connect to ollama at %s (is Ollama running?): %w", a.endpoint, err)
		}
		return "", fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("api error (status %d): %s", resp.StatusCode, string(respData))
	}

	var apiResp ollamaResponse
	if err := json.Unmarshal(respData, &apiResp); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}

	if apiResp.Error != nil {
		if apiResp.Error.Type != "" {
			return "", fmt.Errorf("api error: %s: %s", apiResp.Error.Type, apiResp.Error.Message)
		}
		return "", fmt.Errorf("api error: %s", apiResp.Error.Message)
	}

	if len(apiResp.Choices) == 0 {
		return "", fmt.Errorf("empty response: no choices")
	}

	content := apiResp.Choices[0].Message.Content
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("empty response: no message content")
	}

	return content, nil
}

func isConnectionRefused(err error) bool {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return false
	}

	msg := strings.ToLower(urlErr.Err.Error())
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "actively refused") ||
		strings.Contains(msg, "cannot assign requested address")
}
