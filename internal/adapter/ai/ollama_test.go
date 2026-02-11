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

// fakeOllamaServer creates a test server that returns the given response body
// with the specified status code, and validates request headers.
func fakeOllamaServer(t *testing.T, statusCode int, responseBody string, expectedAuthHeader string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("expected Content-Type 'application/json', got %q", got)
		}

		gotAuth := r.Header.Get("Authorization")
		if expectedAuthHeader == "" {
			if gotAuth != "" {
				t.Errorf("expected no Authorization header, got %q", gotAuth)
			}
		} else if gotAuth != expectedAuthHeader {
			t.Errorf("expected Authorization %q, got %q", expectedAuthHeader, gotAuth)
		}

		var reqBody ollamaRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		if reqBody.Model == "" {
			t.Error("expected non-empty model in request")
		}
		if len(reqBody.Messages) != 2 {
			t.Errorf("expected 2 messages (system + user), got %d", len(reqBody.Messages))
		}
		if reqBody.Stream {
			t.Error("expected stream=false")
		}
		if reqBody.Options.Temperature != 0 {
			t.Errorf("expected temperature=0, got %d", reqBody.Options.Temperature)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		w.Write([]byte(responseBody))
	}))
}

func newTestOllamaAdapter(t *testing.T, serverURL string) *OllamaAdapter {
	t.Helper()
	adapter, err := NewOllama(config.AIConfig{
		Provider: "ollama",
		Model:    "llama3.1:8b",
		APIKey:   "test-key",
	})
	if err != nil {
		t.Fatalf("NewOllama failed: %v", err)
	}
	adapter.endpoint = serverURL
	return adapter
}

func TestNewOllamaMissingModel(t *testing.T) {
	_, err := NewOllama(config.AIConfig{Provider: "ollama"})
	if err == nil {
		t.Fatal("expected error for missing model, got nil")
	}
	if !strings.Contains(err.Error(), "model is required") {
		t.Errorf("expected model required error, got: %v", err)
	}
}

func TestAnalyzeIssueOllama(t *testing.T) {
	planJSON := `{"summary": "Add user authentication", "steps": ["Create auth middleware", "Add login endpoint", "Add JWT token generation"]}`
	respBody := `{"choices": [{"message": {"content": ` + jsonEscape(planJSON) + `}}]}`

	server := fakeOllamaServer(t, http.StatusOK, respBody, "Bearer test-key")
	defer server.Close()

	adapter := newTestOllamaAdapter(t, server.URL)

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

func TestGenerateCodeOllama(t *testing.T) {
	changesJSON := `[{"path": "internal/auth/handler.go", "content": "package auth\n\nfunc Login() {}", "action": "create"}, {"path": "internal/auth/middleware.go", "content": "package auth\n\nfunc Middleware() {}", "action": "create"}]`
	respBody := `{"choices": [{"message": {"content": ` + jsonEscape(changesJSON) + `}}]}`

	server := fakeOllamaServer(t, http.StatusOK, respBody, "Bearer test-key")
	defer server.Close()

	adapter := newTestOllamaAdapter(t, server.URL)

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

func TestAnalyzeFailureOllama(t *testing.T) {
	changesJSON := `[{"path": "internal/auth/handler.go", "content": "package auth\n\nfunc Login() error { return nil }", "action": "modify"}]`
	respBody := `{"choices": [{"message": {"content": ` + jsonEscape(changesJSON) + `}}]}`

	server := fakeOllamaServer(t, http.StatusOK, respBody, "Bearer test-key")
	defer server.Close()

	adapter := newTestOllamaAdapter(t, server.URL)

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

func TestOllamaAPIError(t *testing.T) {
	server := fakeOllamaServer(t, http.StatusInternalServerError, `{"error": {"type": "server_error", "message": "Internal error"}}`, "Bearer test-key")
	defer server.Close()

	adapter := newTestOllamaAdapter(t, server.URL)

	_, err := adapter.AnalyzeIssue(context.Background(), &core.AIIssue{
		Title: "Test",
		Body:  "Test body",
	}, "")

	if err == nil {
		t.Fatal("expected API error, got nil")
	}
	if !strings.Contains(err.Error(), "api error (status 500)") {
		t.Errorf("expected status 500 error, got: %v", err)
	}
}

func TestOllamaEmptyResponse(t *testing.T) {
	respBody := `{"choices": []}`
	server := fakeOllamaServer(t, http.StatusOK, respBody, "Bearer test-key")
	defer server.Close()

	adapter := newTestOllamaAdapter(t, server.URL)

	_, err := adapter.AnalyzeIssue(context.Background(), &core.AIIssue{
		Title: "Test",
		Body:  "Test body",
	}, "")

	if err == nil {
		t.Fatal("expected empty response error, got nil")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("expected 'empty response' error, got: %v", err)
	}
}

func TestOllamaMalformedJSONResponse(t *testing.T) {
	respBody := `{"choices": [{"message": {"content": "this is not json at all"}}]}`
	server := fakeOllamaServer(t, http.StatusOK, respBody, "Bearer test-key")
	defer server.Close()

	adapter := newTestOllamaAdapter(t, server.URL)

	plan, err := adapter.AnalyzeIssue(context.Background(), &core.AIIssue{
		Title: "Test",
		Body:  "Test body",
	}, "")

	if err != nil {
		t.Fatalf("expected fallback plan, got error: %v", err)
	}
	if plan.Summary == "" {
		t.Error("expected non-empty fallback summary")
	}
}

func TestOllamaResponseWithMarkdownFences(t *testing.T) {
	planJSON := "```json\n{\"summary\": \"Fix bug\", \"steps\": [\"Apply patch\"]}\n```"
	respBody := `{"choices": [{"message": {"content": ` + jsonEscape(planJSON) + `}}]}`

	server := fakeOllamaServer(t, http.StatusOK, respBody, "Bearer test-key")
	defer server.Close()

	adapter := newTestOllamaAdapter(t, server.URL)

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

func TestOllamaContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	adapter := newTestOllamaAdapter(t, server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := adapter.AnalyzeIssue(ctx, &core.AIIssue{
		Title: "Test",
		Body:  "Test body",
	}, "")

	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
}

func TestOllamaFileChangeMissingPath(t *testing.T) {
	changesJSON := `[{"path": "", "content": "data", "action": "create"}]`
	respBody := `{"choices": [{"message": {"content": ` + jsonEscape(changesJSON) + `}}]}`

	server := fakeOllamaServer(t, http.StatusOK, respBody, "Bearer test-key")
	defer server.Close()

	adapter := newTestOllamaAdapter(t, server.URL)

	_, err := adapter.GenerateCode(context.Background(), &core.AIPlan{
		Summary: "Test",
		Steps:   []string{"Step 1"},
	}, nil)

	if err == nil {
		t.Fatal("expected error for missing path")
	}
	if !strings.Contains(err.Error(), "missing path") {
		t.Errorf("expected 'missing path' error, got: %v", err)
	}
}

func TestOllamaFileChangeMissingActionDefaultsToCreate(t *testing.T) {
	changesJSON := `[{"path": "file.go", "content": "data", "action": ""}]`
	respBody := `{"choices": [{"message": {"content": ` + jsonEscape(changesJSON) + `}}]}`

	server := fakeOllamaServer(t, http.StatusOK, respBody, "Bearer test-key")
	defer server.Close()

	adapter := newTestOllamaAdapter(t, server.URL)

	changes, err := adapter.GenerateCode(context.Background(), &core.AIPlan{
		Summary: "Test",
		Steps:   []string{"Step 1"},
	}, nil)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Action != "create" {
		t.Errorf("expected default action 'create', got: %q", changes[0].Action)
	}
}

func TestOllamaAuthOptionalWithoutAPIKey(t *testing.T) {
	planJSON := `{"summary": "Plan", "steps": ["One step"]}`
	respBody := `{"choices": [{"message": {"content": ` + jsonEscape(planJSON) + `}}]}`

	server := fakeOllamaServer(t, http.StatusOK, respBody, "")
	defer server.Close()

	adapter, err := NewOllama(config.AIConfig{
		Provider: "ollama",
		Model:    "llama3.1:8b",
	})
	if err != nil {
		t.Fatalf("NewOllama failed: %v", err)
	}
	adapter.endpoint = server.URL

	_, err = adapter.AnalyzeIssue(context.Background(), &core.AIIssue{Title: "Test", Body: "Test body"}, "")
	if err != nil {
		t.Fatalf("unexpected error without API key: %v", err)
	}
}

func TestOllamaAuthOptionalWithAPIKey(t *testing.T) {
	planJSON := `{"summary": "Plan", "steps": ["One step"]}`
	respBody := `{"choices": [{"message": {"content": ` + jsonEscape(planJSON) + `}}]}`

	server := fakeOllamaServer(t, http.StatusOK, respBody, "Bearer test-key")
	defer server.Close()

	adapter := newTestOllamaAdapter(t, server.URL)

	_, err := adapter.AnalyzeIssue(context.Background(), &core.AIIssue{Title: "Test", Body: "Test body"}, "")
	if err != nil {
		t.Fatalf("unexpected error with API key: %v", err)
	}
}
