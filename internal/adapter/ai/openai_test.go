package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rigdev/rig/internal/config"
	"github.com/rigdev/rig/internal/core"
)

// fakeOpenAIServer creates a test server that returns the given response body
// with the specified status code, and validates request headers.
func fakeOpenAIServer(t *testing.T, statusCode int, responseBody string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("expected Authorization 'Bearer test-key', got %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("expected Content-Type 'application/json', got %q", got)
		}

		var reqBody openAIRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		if reqBody.Model == "" {
			t.Error("expected non-empty model in request")
		}
		if reqBody.MaxTokens != defaultMaxTokens {
			t.Errorf("expected max_tokens %d, got %d", defaultMaxTokens, reqBody.MaxTokens)
		}
		if reqBody.Temperature != 0 {
			t.Errorf("expected temperature 0, got %d", reqBody.Temperature)
		}
		if len(reqBody.Messages) < 2 {
			t.Fatalf("expected at least two messages, got %d", len(reqBody.Messages))
		}
		if reqBody.Messages[0].Role != "system" {
			t.Errorf("expected first role 'system', got %q", reqBody.Messages[0].Role)
		}
		if reqBody.Messages[1].Role != "user" {
			t.Errorf("expected second role 'user', got %q", reqBody.Messages[1].Role)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		w.Write([]byte(responseBody))
	}))
}

func newTestOpenAIAdapter(t *testing.T, serverURL string) *OpenAIAdapter {
	t.Helper()
	adapter, err := NewOpenAI(config.AIConfig{
		Provider: "openai",
		APIKey:   "test-key",
		Model:    "gpt-4o-mini",
	})
	if err != nil {
		t.Fatalf("NewOpenAI failed: %v", err)
	}
	adapter.endpoint = serverURL
	return adapter
}

func TestNewOpenAIMissingAPIKey(t *testing.T) {
	_, err := NewOpenAI(config.AIConfig{Provider: "openai"})
	if err == nil {
		t.Fatal("expected error for missing api_key, got nil")
	}
	if !strings.Contains(err.Error(), "api_key is required") {
		t.Errorf("expected api_key error, got: %v", err)
	}
}

func TestNewOpenAIDefaultModel(t *testing.T) {
	adapter, err := NewOpenAI(config.AIConfig{
		Provider: "openai",
		APIKey:   "test-key",
	})
	if err != nil {
		t.Fatalf("NewOpenAI failed: %v", err)
	}
	if adapter.model != "gpt-4o" {
		t.Errorf("expected default model, got: %s", adapter.model)
	}
}

func TestOpenAIAnalyzeIssue(t *testing.T) {
	planJSON := `{"summary": "Add user authentication", "steps": ["Create auth middleware", "Add login endpoint", "Add JWT token generation"]}`
	respBody := `{"choices": [{"message": {"content": ` + jsonEscape(planJSON) + `}}]}`

	server := fakeOpenAIServer(t, http.StatusOK, respBody)
	defer server.Close()

	adapter := newTestOpenAIAdapter(t, server.URL)

	plan, err := adapter.AnalyzeIssue(context.Background(), &core.AIIssue{
		Title: "Add authentication",
		Body:  "We need user login and JWT tokens.",
	}, "Go web application project")

	if err != nil {
		t.Fatalf("AnalyzeIssue failed: %v", err)
	}
	if plan.Summary != "Add user authentication" {
		t.Errorf("expected summary 'Add user authentication', got: %q", plan.Summary)
	}
	if len(plan.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(plan.Steps))
	}
	if plan.Steps[0] != "Create auth middleware" {
		t.Errorf("unexpected step[0]: %q", plan.Steps[0])
	}
}

func TestOpenAIGenerateCode(t *testing.T) {
	changesJSON := `[{"path": "internal/auth/handler.go", "content": "package auth\n\nfunc Login() {}", "action": "create"}, {"path": "internal/auth/middleware.go", "content": "package auth\n\nfunc Middleware() {}", "action": "create"}]`
	respBody := `{"choices": [{"message": {"content": ` + jsonEscape(changesJSON) + `}}]}`

	server := fakeOpenAIServer(t, http.StatusOK, respBody)
	defer server.Close()

	adapter := newTestOpenAIAdapter(t, server.URL)

	changes, err := adapter.GenerateCode(context.Background(), &core.AIPlan{
		Summary: "Add authentication",
		Steps:   []string{"Create handler", "Create middleware"},
	}, map[string]string{
		"main.go": "package main\n\nfunc main() {}",
	})

	if err != nil {
		t.Fatalf("GenerateCode failed: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("expected 2 file changes, got %d", len(changes))
	}
	if changes[0].Path != "internal/auth/handler.go" {
		t.Errorf("unexpected path[0]: %q", changes[0].Path)
	}
	if changes[0].Action != "create" {
		t.Errorf("unexpected action[0]: %q", changes[0].Action)
	}
}

func TestOpenAIAnalyzeFailure(t *testing.T) {
	changesJSON := `[{"path": "internal/auth/handler.go", "content": "package auth\n\nfunc Login() error { return nil }", "action": "modify"}]`
	respBody := `{"choices": [{"message": {"content": ` + jsonEscape(changesJSON) + `}}]}`

	server := fakeOpenAIServer(t, http.StatusOK, respBody)
	defer server.Close()

	adapter := newTestOpenAIAdapter(t, server.URL)

	changes, err := adapter.AnalyzeFailure(context.Background(),
		"FAIL: TestLogin - expected nil error, got: missing return",
		map[string]string{
			"internal/auth/handler.go": "package auth\n\nfunc Login() error {}",
		},
	)

	if err != nil {
		t.Fatalf("AnalyzeFailure failed: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 file change, got %d", len(changes))
	}
	if changes[0].Action != "modify" {
		t.Errorf("expected action 'modify', got: %q", changes[0].Action)
	}
}

func TestOpenAIRateLimit(t *testing.T) {
	server := fakeOpenAIServer(t, http.StatusTooManyRequests, `{"error": {"type": "rate_limit_error", "message": "Too many requests"}}`)
	defer server.Close()

	adapter := newTestOpenAIAdapter(t, server.URL)

	_, err := adapter.AnalyzeIssue(context.Background(), &core.AIIssue{Title: "Test", Body: "Test body"}, "")
	if err == nil {
		t.Fatal("expected rate limit error, got nil")
	}
	if !strings.Contains(err.Error(), "rate limited (429)") {
		t.Errorf("expected rate limit error, got: %v", err)
	}
}

func TestOpenAIAPIError(t *testing.T) {
	server := fakeOpenAIServer(t, http.StatusInternalServerError, `{"error": {"type": "server_error", "message": "Internal error"}}`)
	defer server.Close()

	adapter := newTestOpenAIAdapter(t, server.URL)

	_, err := adapter.AnalyzeIssue(context.Background(), &core.AIIssue{Title: "Test", Body: "Test body"}, "")
	if err == nil {
		t.Fatal("expected API error, got nil")
	}
	if !strings.Contains(err.Error(), "api error (status 500)") {
		t.Errorf("expected status 500 error, got: %v", err)
	}
}

func TestOpenAIEmptyResponse(t *testing.T) {
	respBody := `{"choices": []}`
	server := fakeOpenAIServer(t, http.StatusOK, respBody)
	defer server.Close()

	adapter := newTestOpenAIAdapter(t, server.URL)

	_, err := adapter.AnalyzeIssue(context.Background(), &core.AIIssue{Title: "Test", Body: "Test body"}, "")
	if err == nil {
		t.Fatal("expected empty response error, got nil")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("expected 'empty response' error, got: %v", err)
	}
}

func TestOpenAIMalformedJSONResponse(t *testing.T) {
	respBody := `{"choices": [{"message": {"content": "this is not json at all"}}]}`
	server := fakeOpenAIServer(t, http.StatusOK, respBody)
	defer server.Close()

	adapter := newTestOpenAIAdapter(t, server.URL)

	_, err := adapter.AnalyzeIssue(context.Background(), &core.AIIssue{Title: "Test", Body: "Test body"}, "")
	if err == nil {
		t.Fatal("expected parse error for malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "parse plan") {
		t.Errorf("expected 'parse plan' error, got: %v", err)
	}
}

func TestOpenAIResponseWithMarkdownFences(t *testing.T) {
	planJSON := "```json\n{\"summary\": \"Fix bug\", \"steps\": [\"Apply patch\"]}\n```"
	respBody := `{"choices": [{"message": {"content": ` + jsonEscape(planJSON) + `}}]}`

	server := fakeOpenAIServer(t, http.StatusOK, respBody)
	defer server.Close()

	adapter := newTestOpenAIAdapter(t, server.URL)

	plan, err := adapter.AnalyzeIssue(context.Background(), &core.AIIssue{
		Title: "Bug fix",
		Body:  "Fix the thing",
	}, "")

	if err != nil {
		t.Fatalf("AnalyzeIssue with markdown fences failed: %v", err)
	}
	if plan.Summary != "Fix bug" {
		t.Errorf("expected summary 'Fix bug', got: %q", plan.Summary)
	}
	if len(plan.Steps) != 1 || plan.Steps[0] != "Apply patch" {
		t.Errorf("unexpected steps: %v", plan.Steps)
	}
}

func TestOpenAIContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	adapter := newTestOpenAIAdapter(t, server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := adapter.AnalyzeIssue(ctx, &core.AIIssue{Title: "Test", Body: "Test body"}, "")
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
}

func TestOpenAIFileChangeMissingPath(t *testing.T) {
	changesJSON := `[{"path": "", "content": "data", "action": "create"}]`
	respBody := `{"choices": [{"message": {"content": ` + jsonEscape(changesJSON) + `}}]}`

	server := fakeOpenAIServer(t, http.StatusOK, respBody)
	defer server.Close()

	adapter := newTestOpenAIAdapter(t, server.URL)

	_, err := adapter.GenerateCode(context.Background(), &core.AIPlan{Summary: "Test", Steps: []string{"Step 1"}}, nil)
	if err == nil {
		t.Fatal("expected error for missing path")
	}
	if !strings.Contains(err.Error(), "missing path") {
		t.Errorf("expected 'missing path' error, got: %v", err)
	}
}

func TestOpenAIFileChangeMissingAction(t *testing.T) {
	changesJSON := `[{"path": "file.go", "content": "data", "action": ""}]`
	respBody := `{"choices": [{"message": {"content": ` + jsonEscape(changesJSON) + `}}]}`

	server := fakeOpenAIServer(t, http.StatusOK, respBody)
	defer server.Close()

	adapter := newTestOpenAIAdapter(t, server.URL)

	_, err := adapter.GenerateCode(context.Background(), &core.AIPlan{Summary: "Test", Steps: []string{"Step 1"}}, nil)
	if err == nil {
		t.Fatal("expected error for missing action")
	}
	if !strings.Contains(err.Error(), "missing action") {
		t.Errorf("expected 'missing action' error, got: %v", err)
	}
}
