package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rigdev/rig/internal/config"
	"github.com/rigdev/rig/internal/core"
)

const (
	defaultAnthropicURL     = "https://api.anthropic.com/v1/messages"
	defaultAnthropicVersion = "2023-06-01"
	defaultMaxTokens        = 4096
	defaultHTTPTimeout      = 60 * time.Second
)

// AnthropicAdapter implements AIAdapter using the Anthropic Messages API.
type AnthropicAdapter struct {
	apiKey   string
	model    string
	endpoint string
	client   *http.Client
}

var _ core.AIAdapter = (*AnthropicAdapter)(nil)

// NewAnthropic creates a new AnthropicAdapter from the AI config.
func NewAnthropic(cfg config.AIConfig) (*AnthropicAdapter, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("anthropic: api_key is required")
	}
	model := cfg.Model
	if model == "" {
		model = "claude-3-5-sonnet-20241022"
	}
	return &AnthropicAdapter{
		apiKey:   cfg.APIKey,
		model:    model,
		endpoint: defaultAnthropicURL,
		client:   &http.Client{Timeout: defaultHTTPTimeout},
	}, nil
}

// AnalyzeIssue sends the issue to Anthropic and parses a Plan from the response.
func (a *AnthropicAdapter) AnalyzeIssue(ctx context.Context, issue *core.AIIssue, projectContext string) (*core.AIPlan, error) {
	if issue == nil {
		return nil, fmt.Errorf("anthropic: issue is nil")
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
		return nil, fmt.Errorf("anthropic: analyze issue: %w", err)
	}

	return parsePlan(body)
}

// GenerateCode sends the plan and repo files to Anthropic and parses FileChange list.
func (a *AnthropicAdapter) GenerateCode(ctx context.Context, plan *core.AIPlan, repoFiles map[string]string) ([]core.AIFileChange, error) {
	if plan == nil {
		return nil, fmt.Errorf("anthropic: plan is nil")
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
		return nil, fmt.Errorf("anthropic: generate code: %w", err)
	}

	return parseFileChanges(body)
}

// AnalyzeFailure sends test/build logs and current code to Anthropic for fix suggestions.
func (a *AnthropicAdapter) AnalyzeFailure(ctx context.Context, logs string, currentCode map[string]string) ([]core.AIFileChange, error) {
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
		return nil, fmt.Errorf("anthropic: analyze failure: %w", err)
	}

	return parseFileChanges(body)
}

// AnalyzeDeployFailure sends deploy logs and infra files to Anthropic for deploy fix suggestions.
func (a *AnthropicAdapter) AnalyzeDeployFailure(ctx context.Context, deployLogs string, infraFiles map[string]string) (*core.AIProposedFix, error) {
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
		return nil, fmt.Errorf("anthropic: analyze deploy failure: %w", err)
	}

	return parseProposedFix(body)
}

// anthropicRequest is the Anthropic Messages API request body.
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

// anthropicMessage is a single message in the Anthropic conversation.
type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicResponse is the Anthropic Messages API response.
type anthropicResponse struct {
	Content []anthropicContentBlock `json:"content"`
	Error   *anthropicError         `json:"error,omitempty"`
}

// anthropicContentBlock is a content block in the API response.
type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// anthropicError represents an API error response.
type anthropicError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// sendMessage posts a single prompt to the Anthropic Messages API and returns the text response.
func (a *AnthropicAdapter) sendMessage(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	reqBody := anthropicRequest{
		Model:     a.model,
		MaxTokens: defaultMaxTokens,
		System:    systemPrompt,
		Messages: []anthropicMessage{
			{Role: "user", Content: userPrompt},
		},
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
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", defaultAnthropicVersion)

	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return "", fmt.Errorf("rate limited (429): %s", string(respData))
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("api error (status %d): %s", resp.StatusCode, string(respData))
	}

	var apiResp anthropicResponse
	if err := json.Unmarshal(respData, &apiResp); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}

	if apiResp.Error != nil {
		return "", fmt.Errorf("api error: %s: %s", apiResp.Error.Type, apiResp.Error.Message)
	}

	if len(apiResp.Content) == 0 {
		return "", fmt.Errorf("empty response: no content blocks")
	}

	// Extract text from the first text content block.
	for _, block := range apiResp.Content {
		if block.Type == "text" {
			return block.Text, nil
		}
	}

	return "", fmt.Errorf("empty response: no text content block")
}
