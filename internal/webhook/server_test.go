package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rigdev/rig/internal/config"
	"github.com/rigdev/rig/internal/core"
)

// signPayload computes the HMAC-SHA256 signature for a payload.
func signPayload(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// makeIssuePayload creates a GitHub issues event JSON payload.
func makeIssuePayload(action string, number int, title string, labels []string, repo string) []byte {
	type label struct {
		Name string `json:"name"`
	}
	lbls := make([]label, len(labels))
	for i, l := range labels {
		lbls[i] = label{Name: l}
	}
	payload := map[string]interface{}{
		"action": action,
		"issue": map[string]interface{}{
			"number":   number,
			"title":    title,
			"html_url": fmt.Sprintf("https://github.com/%s/issues/%d", repo, number),
			"labels":   lbls,
		},
		"repository": map[string]interface{}{
			"full_name": repo,
		},
	}
	data, _ := json.Marshal(payload)
	return data
}

func TestServerValidWebhook(t *testing.T) {
	secret := "test-secret-123"
	var executedIssue *core.Issue

	handler := NewHandler(secret, []config.TriggerConfig{
		{Event: "issues.opened", Labels: []string{"rig"}},
	}, "", func(issue core.Issue) error {
		executedIssue = &issue
		return nil
	})

	srv := NewServer(config.ServerConfig{Port: 0, Secret: secret}, handler)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	payload := makeIssuePayload("opened", 42, "Fix bug", []string{"rig", "bug"}, "myorg/myrepo")
	sig := signPayload(secret, payload)

	req, _ := http.NewRequest("POST", ts.URL+"/webhook", strings.NewReader(string(payload)))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("Expected 202 Accepted, got %d", resp.StatusCode)
	}

	if executedIssue == nil {
		t.Fatal("Expected execute callback to be called")
	}
	if executedIssue.ID != "42" {
		t.Errorf("Expected issue ID '42', got %q", executedIssue.ID)
	}
	if executedIssue.Repo != "myorg/myrepo" {
		t.Errorf("Expected repo 'myorg/myrepo', got %q", executedIssue.Repo)
	}
}

func TestServerInvalidSignature(t *testing.T) {
	secret := "correct-secret"

	handler := NewHandler(secret, nil, "", nil)
	srv := NewServer(config.ServerConfig{Port: 0, Secret: secret}, handler)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	payload := makeIssuePayload("opened", 1, "Test", nil, "org/repo")
	wrongSig := signPayload("wrong-secret", payload)

	req, _ := http.NewRequest("POST", ts.URL+"/webhook", strings.NewReader(string(payload)))
	req.Header.Set("X-Hub-Signature-256", wrongSig)
	req.Header.Set("X-GitHub-Event", "issues")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized, got %d", resp.StatusCode)
	}
}

func TestServerMissingSignature(t *testing.T) {
	secret := "my-secret"

	handler := NewHandler(secret, nil, "", nil)
	srv := NewServer(config.ServerConfig{Port: 0, Secret: secret}, handler)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	payload := makeIssuePayload("opened", 1, "Test", nil, "org/repo")

	req, _ := http.NewRequest("POST", ts.URL+"/webhook", strings.NewReader(string(payload)))
	req.Header.Set("X-GitHub-Event", "issues")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", resp.StatusCode)
	}
}

func TestServerNoSecretRejectsRequest(t *testing.T) {
	var called bool

	handler := NewHandler("", []config.TriggerConfig{
		{Event: "issues.opened"},
	}, "", func(issue core.Issue) error {
		called = true
		return nil
	})

	srv := NewServer(config.ServerConfig{Port: 0}, handler)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	payload := makeIssuePayload("opened", 5, "No secret", nil, "org/repo")

	req, _ := http.NewRequest("POST", ts.URL+"/webhook", strings.NewReader(string(payload)))
	req.Header.Set("X-GitHub-Event", "issues")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Empty secret now rejects requests for security.
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", resp.StatusCode)
	}
	if called {
		t.Error("Expected execute NOT to be called when no secret configured")
	}
}

func TestServerUntrackedEventIgnored(t *testing.T) {
	secret := "test-secret-untracked"
	var called bool

	handler := NewHandler(secret, nil, "", func(issue core.Issue) error {
		called = true
		return nil
	})

	srv := NewServer(config.ServerConfig{}, handler)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// "issues.closed" is not a tracked action.
	payload := makeIssuePayload("closed", 1, "Close it", nil, "org/repo")
	sig := signPayload(secret, payload)

	req, _ := http.NewRequest("POST", ts.URL+"/webhook", strings.NewReader(string(payload)))
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-Hub-Signature-256", sig)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
	if called {
		t.Error("Expected execute NOT to be called for untracked event")
	}
}

func TestServerMethodNotAllowed(t *testing.T) {
	handler := NewHandler("", nil, "", nil)
	srv := NewServer(config.ServerConfig{}, handler)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/webhook")
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %d", resp.StatusCode)
	}
}
