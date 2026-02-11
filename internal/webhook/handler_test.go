package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rigdev/rig/internal/config"
	"github.com/rigdev/rig/internal/core"
)

const testSecret = "test-webhook-secret"

// signTestPayload computes HMAC-SHA256 for test payloads.
func signTestPayload(payload []byte) string {
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// newSignedRequest creates a POST request with proper event header and signature.
func newSignedRequest(url string, payload []byte, event string) *http.Request {
	req, _ := http.NewRequest("POST", url+"/webhook", strings.NewReader(string(payload)))
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-Hub-Signature-256", signTestPayload(payload))
	return req
}

func TestHandlerDuplicateIssueSkipped(t *testing.T) {
	// Create a temp state file with an in-flight task.
	tmpDir, err := os.MkdirTemp("", "rig-handler-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	statePath := filepath.Join(tmpDir, "state.json")
	state := &core.State{
		Version: "1.0",
		Tasks: []core.Task{
			{
				ID:     "task-001",
				Issue:  core.Issue{ID: "99", Platform: "github", Repo: "org/repo"},
				Status: core.PhaseCoding, // In-flight (not completed/failed/rollback).
			},
		},
	}
	if err := core.SaveState(state, statePath); err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	var called bool
	handler := NewHandler(testSecret, []config.TriggerConfig{
		{Event: "issues.opened"},
	}, statePath, func(issue core.Issue) error {
		called = true
		return nil
	})

	srv := NewServer(config.ServerConfig{}, handler)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// Send a webhook for issue #99 which is already in-flight.
	payload := makeIssuePayload("opened", 99, "Duplicate issue", nil, "org/repo")
	req := newSignedRequest(ts.URL, payload, "issues")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 for duplicate, got %d", resp.StatusCode)
	}
	if called {
		t.Error("Expected execute NOT to be called for duplicate issue")
	}
}

func TestHandlerLabelFiltering(t *testing.T) {
	tests := []struct {
		name       string
		triggers   []config.TriggerConfig
		labels     []string
		wantCalled bool
	}{
		{
			name: "matching label",
			triggers: []config.TriggerConfig{
				{Event: "issues.opened", Labels: []string{"rig"}},
			},
			labels:     []string{"rig", "enhancement"},
			wantCalled: true,
		},
		{
			name: "no matching label",
			triggers: []config.TriggerConfig{
				{Event: "issues.opened", Labels: []string{"rig"}},
			},
			labels:     []string{"bug", "enhancement"},
			wantCalled: false,
		},
		{
			name: "case insensitive label",
			triggers: []config.TriggerConfig{
				{Event: "issues.opened", Labels: []string{"RIG"}},
			},
			labels:     []string{"rig"},
			wantCalled: true,
		},
		{
			name:       "no triggers accepts all",
			triggers:   nil,
			labels:     []string{"anything"},
			wantCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var called bool
			handler := NewHandler(testSecret, tt.triggers, "", func(issue core.Issue) error {
				called = true
				return nil
			})

			srv := NewServer(config.ServerConfig{}, handler)
			ts := httptest.NewServer(srv.Router())
			defer ts.Close()

			payload := makeIssuePayload("opened", 1, "Test", tt.labels, "org/repo")
			req := newSignedRequest(ts.URL, payload, "issues")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			resp.Body.Close()

			if called != tt.wantCalled {
				t.Errorf("Expected called=%v, got %v", tt.wantCalled, called)
			}
		})
	}
}

func TestHandlerKeywordFiltering(t *testing.T) {
	tests := []struct {
		name       string
		keyword    string
		title      string
		wantCalled bool
	}{
		{
			name:       "keyword in title",
			keyword:    "deploy",
			title:      "Please deploy this fix",
			wantCalled: true,
		},
		{
			name:       "keyword not in title",
			keyword:    "deploy",
			title:      "Fix the login bug",
			wantCalled: false,
		},
		{
			name:       "case insensitive keyword",
			keyword:    "DEPLOY",
			title:      "please deploy now",
			wantCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var called bool
			handler := NewHandler(testSecret, []config.TriggerConfig{
				{Event: "issues.opened", Keyword: tt.keyword},
			}, "", func(issue core.Issue) error {
				called = true
				return nil
			})

			srv := NewServer(config.ServerConfig{}, handler)
			ts := httptest.NewServer(srv.Router())
			defer ts.Close()

			payload := makeIssuePayload("opened", 1, tt.title, nil, "org/repo")
			req := newSignedRequest(ts.URL, payload, "issues")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			resp.Body.Close()

			if called != tt.wantCalled {
				t.Errorf("Expected called=%v, got %v", tt.wantCalled, called)
			}
		})
	}
}

func TestHandlerIssueCommentKeyword(t *testing.T) {
	var called bool
	handler := NewHandler(testSecret, []config.TriggerConfig{
		{Event: "issue_comment.created", Keyword: "/rig"},
	}, "", func(issue core.Issue) error {
		called = true
		return nil
	})

	srv := NewServer(config.ServerConfig{}, handler)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// Build a comment event payload.
	payload, _ := json.Marshal(map[string]interface{}{
		"action": "created",
		"issue": map[string]interface{}{
			"number":   10,
			"title":    "Some issue",
			"html_url": "https://github.com/org/repo/issues/10",
			"labels":   []interface{}{},
		},
		"comment": map[string]interface{}{
			"body": "/rig please fix this",
		},
		"repository": map[string]interface{}{
			"full_name": "org/repo",
		},
	})

	req := newSignedRequest(ts.URL, payload, "issue_comment")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("Expected 202, got %d", resp.StatusCode)
	}
	if !called {
		t.Error("Expected execute to be called for comment with keyword")
	}
}

func TestHandlerExecuteError(t *testing.T) {
	handler := NewHandler(testSecret, []config.TriggerConfig{
		{Event: "issues.opened"},
	}, "", func(issue core.Issue) error {
		return fmt.Errorf("engine error: something broke")
	})

	srv := NewServer(config.ServerConfig{}, handler)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	payload := makeIssuePayload("opened", 1, "Test", nil, "org/repo")
	req := newSignedRequest(ts.URL, payload, "issues")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("Expected 500, got %d", resp.StatusCode)
	}
}

func TestHandlerIssueLabeledEvent(t *testing.T) {
	var called bool
	handler := NewHandler(testSecret, []config.TriggerConfig{
		{Event: "issues.labeled", Labels: []string{"rig"}},
	}, "", func(issue core.Issue) error {
		called = true
		return nil
	})

	srv := NewServer(config.ServerConfig{}, handler)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	payload := makeIssuePayload("labeled", 7, "Add feature", []string{"rig"}, "org/repo")
	req := newSignedRequest(ts.URL, payload, "issues")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("Expected 202, got %d", resp.StatusCode)
	}
	if !called {
		t.Error("Expected execute to be called for labeled event")
	}
}

func TestHandlerSignatureVerification(t *testing.T) {
	tests := []struct {
		name       string
		secret     string
		sigSecret  string
		signature  string
		wantStatus int
	}{
		{
			name:       "valid signature",
			secret:     "s3cr3t",
			sigSecret:  "s3cr3t",
			wantStatus: http.StatusAccepted,
		},
		{
			name:       "wrong secret",
			secret:     "s3cr3t",
			sigSecret:  "wrong",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "malformed signature",
			secret:     "s3cr3t",
			signature:  "notsha256=abc",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "empty signature",
			secret:     "s3cr3t",
			signature:  "",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewHandler(tt.secret, []config.TriggerConfig{
				{Event: "issues.opened"},
			}, "", func(issue core.Issue) error {
				return nil
			})

			srv := NewServer(config.ServerConfig{}, handler)
			ts := httptest.NewServer(srv.Router())
			defer ts.Close()

			payload := makeIssuePayload("opened", 1, "Test", nil, "org/repo")

			sig := tt.signature
			if sig == "" && tt.sigSecret != "" {
				sig = signPayload(tt.sigSecret, payload)
			}

			req, _ := http.NewRequest("POST", ts.URL+"/webhook", strings.NewReader(string(payload)))
			req.Header.Set("X-GitHub-Event", "issues")
			if sig != "" {
				req.Header.Set("X-Hub-Signature-256", sig)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, resp.StatusCode)
			}
		})
	}
}

func TestHandlerCompletedTaskNotInFlight(t *testing.T) {
	// Completed tasks should NOT be considered in-flight.
	tmpDir, err := os.MkdirTemp("", "rig-handler-completed-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	statePath := filepath.Join(tmpDir, "state.json")
	state := &core.State{
		Version: "1.0",
		Tasks: []core.Task{
			{
				ID:     "task-old",
				Issue:  core.Issue{ID: "50", Platform: "github", Repo: "org/repo"},
				Status: core.PhaseCompleted,
			},
		},
	}
	if err := core.SaveState(state, statePath); err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	var called bool
	handler := NewHandler(testSecret, []config.TriggerConfig{
		{Event: "issues.opened"},
	}, statePath, func(issue core.Issue) error {
		called = true
		return nil
	})

	srv := NewServer(config.ServerConfig{}, handler)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	payload := makeIssuePayload("opened", 50, "Reopen work", nil, "org/repo")
	req := newSignedRequest(ts.URL, payload, "issues")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("Expected 202, got %d", resp.StatusCode)
	}
	if !called {
		t.Error("Expected execute to be called for completed task re-triggered")
	}
}
