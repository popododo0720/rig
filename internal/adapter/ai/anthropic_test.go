package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rigdev/rig/internal/config"
)

// fakeAnthropicServer creates a test server that returns the given response body
// with the specified status code, and validates request headers.
func fakeAnthropicServer(t *testing.T, statusCode int, responseBody string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Validate method and path.
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		// Validate required headers.
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Errorf("expected x-api-key 'test-key', got %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Errorf("expected anthropic-version '2023-06-01', got %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("expected Content-Type 'application/json', got %q", got)
		}

		// Validate request body is valid JSON with expected fields.
		var reqBody anthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		if reqBody.Model == "" {
			t.Error("expected non-empty model in request")
		}
		if len(reqBody.Messages) == 0 {
			t.Error("expected at least one message in request")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		w.Write([]byte(responseBody))
	}))
}

func newTestAdapter(t *testing.T, serverURL string) *AnthropicAdapter {
	t.Helper()
	adapter, err := NewAnthropic(config.AIConfig{
		Provider: "anthropic",
		APIKey:   "test-key",
		Model:    "claude-3-5-sonnet-20241022",
	})
	if err != nil {
		t.Fatalf("NewAnthropic failed: %v", err)
	}
	adapter.endpoint = serverURL
	return adapter
}

func TestNewAnthropicMissingAPIKey(t *testing.T) {
	_, err := NewAnthropic(config.AIConfig{
		Provider: "anthropic",
	})
	if err == nil {
		t.Fatal("expected error for missing api_key, got nil")
	}
	if !strings.Contains(err.Error(), "api_key is required") {
		t.Errorf("expected api_key error, got: %v", err)
	}
}

func TestNewAnthropicDefaultModel(t *testing.T) {
	adapter, err := NewAnthropic(config.AIConfig{
		Provider: "anthropic",
		APIKey:   "test-key",
	})
	if err != nil {
		t.Fatalf("NewAnthropic failed: %v", err)
	}
	if adapter.model != "claude-3-5-sonnet-20241022" {
		t.Errorf("expected default model, got: %s", adapter.model)
	}
}

func TestAnalyzeIssue(t *testing.T) {
	planJSON := `{"summary": "Add user authentication", "steps": ["Create auth middleware", "Add login endpoint", "Add JWT token generation"]}`
	respBody := `{"content": [{"type": "text", "text": "` + strings.ReplaceAll(planJSON, `"`, `\"`) + `"}]}`

	server := fakeAnthropicServer(t, http.StatusOK, respBody)
	defer server.Close()

	adapter := newTestAdapter(t, server.URL)

	plan, err := adapter.AnalyzeIssue(context.Background(), &Issue{
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

func TestAnalyzeIssueNilIssue(t *testing.T) {
	adapter := newTestAdapter(t, "http://unused")
	_, err := adapter.AnalyzeIssue(context.Background(), nil, "")
	if err == nil {
		t.Fatal("expected error for nil issue")
	}
	if !strings.Contains(err.Error(), "issue is nil") {
		t.Errorf("expected 'issue is nil' error, got: %v", err)
	}
}

func TestGenerateCode(t *testing.T) {
	changesJSON := `[{"path": "internal/auth/handler.go", "content": "package auth\n\nfunc Login() {}", "action": "create"}, {"path": "internal/auth/middleware.go", "content": "package auth\n\nfunc Middleware() {}", "action": "create"}]`
	respBody := `{"content": [{"type": "text", "text": ` + jsonEscape(changesJSON) + `}]}`

	server := fakeAnthropicServer(t, http.StatusOK, respBody)
	defer server.Close()

	adapter := newTestAdapter(t, server.URL)

	changes, err := adapter.GenerateCode(context.Background(), &Plan{
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
	if changes[1].Path != "internal/auth/middleware.go" {
		t.Errorf("unexpected path[1]: %q", changes[1].Path)
	}
}

func TestGenerateCodeNilPlan(t *testing.T) {
	adapter := newTestAdapter(t, "http://unused")
	_, err := adapter.GenerateCode(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error for nil plan")
	}
	if !strings.Contains(err.Error(), "plan is nil") {
		t.Errorf("expected 'plan is nil' error, got: %v", err)
	}
}

func TestAnalyzeFailure(t *testing.T) {
	changesJSON := `[{"path": "internal/auth/handler.go", "content": "package auth\n\nfunc Login() error { return nil }", "action": "modify"}]`
	respBody := `{"content": [{"type": "text", "text": ` + jsonEscape(changesJSON) + `}]}`

	server := fakeAnthropicServer(t, http.StatusOK, respBody)
	defer server.Close()

	adapter := newTestAdapter(t, server.URL)

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

func TestRateLimit(t *testing.T) {
	server := fakeAnthropicServer(t, http.StatusTooManyRequests, `{"error": {"type": "rate_limit_error", "message": "Too many requests"}}`)
	defer server.Close()

	adapter := newTestAdapter(t, server.URL)

	_, err := adapter.AnalyzeIssue(context.Background(), &Issue{
		Title: "Test",
		Body:  "Test body",
	}, "")

	if err == nil {
		t.Fatal("expected rate limit error, got nil")
	}
	if !strings.Contains(err.Error(), "rate limited (429)") {
		t.Errorf("expected rate limit error, got: %v", err)
	}
}

func TestAPIError(t *testing.T) {
	server := fakeAnthropicServer(t, http.StatusInternalServerError, `{"error": {"type": "server_error", "message": "Internal error"}}`)
	defer server.Close()

	adapter := newTestAdapter(t, server.URL)

	_, err := adapter.AnalyzeIssue(context.Background(), &Issue{
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

func TestEmptyResponse(t *testing.T) {
	respBody := `{"content": []}`
	server := fakeAnthropicServer(t, http.StatusOK, respBody)
	defer server.Close()

	adapter := newTestAdapter(t, server.URL)

	_, err := adapter.AnalyzeIssue(context.Background(), &Issue{
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

func TestEmptyResponseNoTextBlock(t *testing.T) {
	respBody := `{"content": [{"type": "tool_use", "text": ""}]}`
	server := fakeAnthropicServer(t, http.StatusOK, respBody)
	defer server.Close()

	adapter := newTestAdapter(t, server.URL)

	_, err := adapter.AnalyzeIssue(context.Background(), &Issue{
		Title: "Test",
		Body:  "Test body",
	}, "")

	if err == nil {
		t.Fatal("expected error for no text block, got nil")
	}
	if !strings.Contains(err.Error(), "no text content block") {
		t.Errorf("expected 'no text content block' error, got: %v", err)
	}
}

func TestMalformedJSONResponse(t *testing.T) {
	respBody := `{"content": [{"type": "text", "text": "this is not json at all"}]}`
	server := fakeAnthropicServer(t, http.StatusOK, respBody)
	defer server.Close()

	adapter := newTestAdapter(t, server.URL)

	_, err := adapter.AnalyzeIssue(context.Background(), &Issue{
		Title: "Test",
		Body:  "Test body",
	}, "")

	if err == nil {
		t.Fatal("expected parse error for malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "parse plan") {
		t.Errorf("expected 'parse plan' error, got: %v", err)
	}
}

func TestResponseWithMarkdownFences(t *testing.T) {
	// AI sometimes wraps JSON in markdown code fences despite instructions.
	planJSON := "```json\n{\"summary\": \"Fix bug\", \"steps\": [\"Apply patch\"]}\n```"
	respBody := `{"content": [{"type": "text", "text": ` + jsonEscape(planJSON) + `}]}`

	server := fakeAnthropicServer(t, http.StatusOK, respBody)
	defer server.Close()

	adapter := newTestAdapter(t, server.URL)

	plan, err := adapter.AnalyzeIssue(context.Background(), &Issue{
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

func TestFileChangeMissingPath(t *testing.T) {
	changesJSON := `[{"path": "", "content": "data", "action": "create"}]`
	respBody := `{"content": [{"type": "text", "text": ` + jsonEscape(changesJSON) + `}]}`

	server := fakeAnthropicServer(t, http.StatusOK, respBody)
	defer server.Close()

	adapter := newTestAdapter(t, server.URL)

	_, err := adapter.GenerateCode(context.Background(), &Plan{
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

func TestFileChangeMissingAction(t *testing.T) {
	changesJSON := `[{"path": "file.go", "content": "data", "action": ""}]`
	respBody := `{"content": [{"type": "text", "text": ` + jsonEscape(changesJSON) + `}]}`

	server := fakeAnthropicServer(t, http.StatusOK, respBody)
	defer server.Close()

	adapter := newTestAdapter(t, server.URL)

	_, err := adapter.GenerateCode(context.Background(), &Plan{
		Summary: "Test",
		Steps:   []string{"Step 1"},
	}, nil)

	if err == nil {
		t.Fatal("expected error for missing action")
	}
	if !strings.Contains(err.Error(), "missing action") {
		t.Errorf("expected 'missing action' error, got: %v", err)
	}
}

func TestEmptyPlanSummary(t *testing.T) {
	planJSON := `{"summary": "", "steps": ["step 1"]}`
	respBody := `{"content": [{"type": "text", "text": ` + jsonEscape(planJSON) + `}]}`

	server := fakeAnthropicServer(t, http.StatusOK, respBody)
	defer server.Close()

	adapter := newTestAdapter(t, server.URL)

	_, err := adapter.AnalyzeIssue(context.Background(), &Issue{
		Title: "Test",
		Body:  "Test body",
	}, "")

	if err == nil {
		t.Fatal("expected error for empty plan summary")
	}
	if !strings.Contains(err.Error(), "empty summary") {
		t.Errorf("expected 'empty summary' error, got: %v", err)
	}
}

func TestContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until context is cancelled — simulates slow API.
		<-r.Context().Done()
	}))
	defer server.Close()

	adapter := newTestAdapter(t, server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := adapter.AnalyzeIssue(ctx, &Issue{
		Title: "Test",
		Body:  "Test body",
	}, "")

	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
}

func TestRequestHeaders(t *testing.T) {
	// This test is implicitly covered by fakeAnthropicServer header checks,
	// but we verify explicitly here.
	var capturedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		planJSON := `{"summary": "Test plan", "steps": ["step 1"]}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{"content": [{"type": "text", "text": ` + jsonEscape(planJSON) + `}]}`
		w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := newTestAdapter(t, server.URL)

	_, err := adapter.AnalyzeIssue(context.Background(), &Issue{
		Title: "Test",
		Body:  "Test body",
	}, "project context")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := capturedHeaders.Get("x-api-key"); got != "test-key" {
		t.Errorf("expected x-api-key 'test-key', got %q", got)
	}
	if got := capturedHeaders.Get("anthropic-version"); got != "2023-06-01" {
		t.Errorf("expected anthropic-version '2023-06-01', got %q", got)
	}
	if got := capturedHeaders.Get("Content-Type"); got != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", got)
	}
}

func TestCleanJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain json",
			input: `{"key": "value"}`,
			want:  `{"key": "value"}`,
		},
		{
			name:  "with json fence",
			input: "```json\n{\"key\": \"value\"}\n```",
			want:  `{"key": "value"}`,
		},
		{
			name:  "with plain fence",
			input: "```\n{\"key\": \"value\"}\n```",
			want:  `{"key": "value"}`,
		},
		{
			name:  "with whitespace",
			input: "  \n{\"key\": \"value\"}\n  ",
			want:  `{"key": "value"}`,
		},
		{
			name:  "empty",
			input: "",
			want:  "",
		},
		{
			name:  "whitespace only",
			input: "   \n  \t  ",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanJSON(tt.input)
			if got != tt.want {
				t.Errorf("cleanJSON(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// jsonEscape returns a JSON-encoded string (with surrounding quotes) from the input.
func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
