package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/rigdev/rig/internal/config"
	"github.com/rigdev/rig/internal/core"
)

const (
	defaultOpenAIURL   = "https://api.openai.com/v1/chat/completions"
	defaultOpenAIModel = "gpt-4o"
)

// OpenAIAdapter implements AIAdapter using the OpenAI Chat Completions API.
type OpenAIAdapter struct {
	apiKey   string
	model    string
	endpoint string
	client   *http.Client
}

var _ core.AIAdapter = (*OpenAIAdapter)(nil)

// NewOpenAI creates a new OpenAIAdapter from the AI config.
func NewOpenAI(cfg config.AIConfig) (*OpenAIAdapter, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("openai: api_key is required")
	}

	model := cfg.Model
	if model == "" {
		model = defaultOpenAIModel
	}

	return &OpenAIAdapter{
		apiKey:   cfg.APIKey,
		model:    model,
		endpoint: defaultOpenAIURL,
		client:   &http.Client{Timeout: defaultHTTPTimeout},
	}, nil
}

// AnalyzeIssue sends the issue to OpenAI and parses a Plan from the response.
func (a *OpenAIAdapter) AnalyzeIssue(ctx context.Context, issue *core.AIIssue, projectContext string) (*core.AIPlan, error) {
	if issue == nil {
		return nil, fmt.Errorf("openai: issue is nil")
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
		return nil, fmt.Errorf("openai: analyze issue: %w", err)
	}

	return parsePlan(body)
}

// GenerateCode sends the plan and repo files to OpenAI and parses FileChange list.
func (a *OpenAIAdapter) GenerateCode(ctx context.Context, plan *core.AIPlan, repoFiles map[string]string) ([]core.AIFileChange, error) {
	if plan == nil {
		return nil, fmt.Errorf("openai: plan is nil")
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
		return nil, fmt.Errorf("openai: generate code: %w", err)
	}

	return parseFileChanges(body)
}

// AnalyzeFailure sends test/build logs and current code to OpenAI for fix suggestions.
func (a *OpenAIAdapter) AnalyzeFailure(ctx context.Context, logs string, currentCode map[string]string) ([]core.AIFileChange, error) {
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
		return nil, fmt.Errorf("openai: analyze failure: %w", err)
	}

	return parseFileChanges(body)
}

// openAIRequest is the OpenAI Chat Completions API request body.
type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens"`
	Temperature int             `json:"temperature"`
}

// openAIMessage is a single message in the OpenAI conversation.
type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAIResponse is the OpenAI Chat Completions API response.
type openAIResponse struct {
	Choices []openAIChoice `json:"choices"`
	Error   *openAIError   `json:"error,omitempty"`
}

// openAIChoice is a response choice.
type openAIChoice struct {
	Message openAIMessage `json:"message"`
}

// openAIError represents an API error response.
type openAIError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// sendMessage posts prompts to OpenAI Chat Completions API and returns the assistant text.
func (a *OpenAIAdapter) sendMessage(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	reqBody := openAIRequest{
		Model: a.model,
		Messages: []openAIMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		MaxTokens:   defaultMaxTokens,
		Temperature: 0,
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
	req.Header.Set("Authorization", "Bearer "+a.apiKey)

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

	var apiResp openAIResponse
	if err := json.Unmarshal(respData, &apiResp); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}

	if apiResp.Error != nil {
		return "", fmt.Errorf("api error: %s: %s", apiResp.Error.Type, apiResp.Error.Message)
	}

	if len(apiResp.Choices) == 0 {
		return "", fmt.Errorf("empty response: no choices")
	}

	content := strings.TrimSpace(apiResp.Choices[0].Message.Content)
	if content == "" {
		return "", fmt.Errorf("empty response: empty message content")
	}

	return content, nil
}
