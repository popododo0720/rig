package git

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v60/github"
	"github.com/rigdev/rig/internal/core"
)

// newTestGitHub creates a GitHubAdapter with a custom HTTP client backed by httptest.
func newTestGitHub(t *testing.T, mux *http.ServeMux) (*GitHubAdapter, *httptest.Server) {
	t.Helper()

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := github.NewClient(nil)
	u, _ := url.Parse(server.URL + "/")
	client.BaseURL = u

	tmpDir := t.TempDir()

	return &GitHubAdapter{
		client:    client,
		owner:     "test-owner",
		repo:      "test-repo",
		token:     "test-token",
		secret:    "test-secret",
		workspace: tmpDir,
	}, server
}

// --- GetIssue tests ---

func TestGitHubGetIssue(t *testing.T) {
	tests := []struct {
		name       string
		number     int
		response   string
		statusCode int
		wantTitle  string
		wantErr    bool
	}{
		{
			name:   "success",
			number: 42,
			response: `{
				"id": 100,
				"number": 42,
				"title": "Fix the bug",
				"body": "There is a bug that needs fixing",
				"created_at": "2025-01-15T10:00:00Z",
				"user": {"login": "octocat"},
				"labels": [{"name": "bug"}, {"name": "priority"}]
			}`,
			statusCode: http.StatusOK,
			wantTitle:  "Fix the bug",
		},
		{
			name:       "not found",
			number:     999,
			response:   `{"message": "Not Found"}`,
			statusCode: http.StatusNotFound,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc(fmt.Sprintf("/repos/test-owner/test-repo/issues/%d", tt.number), func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				fmt.Fprint(w, tt.response)
			})

			adapter, _ := newTestGitHub(t, mux)

			issue, err := adapter.GetIssue(context.Background(), "test-owner", "test-repo", tt.number)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if issue.Title != tt.wantTitle {
				t.Errorf("title = %q, want %q", issue.Title, tt.wantTitle)
			}
			if issue.Number != tt.number {
				t.Errorf("number = %d, want %d", issue.Number, tt.number)
			}
			if issue.Body != "There is a bug that needs fixing" {
				t.Errorf("body = %q, want %q", issue.Body, "There is a bug that needs fixing")
			}
			if issue.Author != "octocat" {
				t.Errorf("author = %q, want %q", issue.Author, "octocat")
			}
			if len(issue.Labels) != 2 {
				t.Fatalf("labels count = %d, want 2", len(issue.Labels))
			}
			if issue.Labels[0] != "bug" || issue.Labels[1] != "priority" {
				t.Errorf("labels = %v, want [bug priority]", issue.Labels)
			}
			if issue.ID != "100" {
				t.Errorf("id = %q, want %q", issue.ID, "100")
			}
		})
	}
}

// --- PostComment tests ---

func TestGitHubPostComment(t *testing.T) {
	tests := []struct {
		name       string
		number     int
		body       string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "success",
			number:     42,
			body:       "LGTM!",
			statusCode: http.StatusCreated,
		},
		{
			name:       "server error",
			number:     42,
			body:       "test",
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedBody string

			mux := http.NewServeMux()
			mux.HandleFunc(fmt.Sprintf("/repos/test-owner/test-repo/issues/%d/comments", tt.number), func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %s, want POST", r.Method)
				}

				var payload struct {
					Body string `json:"body"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
					receivedBody = payload.Body
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				fmt.Fprint(w, `{"id": 1}`)
			})

			adapter, _ := newTestGitHub(t, mux)

			err := adapter.PostComment(context.Background(), "test-owner", "test-repo", tt.number, tt.body)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if receivedBody != tt.body {
				t.Errorf("comment body = %q, want %q", receivedBody, tt.body)
			}
		})
	}
}

// --- CreatePR tests ---

func TestGitHubCreatePR(t *testing.T) {
	tests := []struct {
		name       string
		base       string
		head       string
		title      string
		body       string
		statusCode int
		response   string
		wantErr    bool
		wantNumber int
		wantURL    string
	}{
		{
			name:  "success",
			base:  "main",
			head:  "feature/fix-bug",
			title: "Fix bug in parser",
			body:  "This PR fixes the parsing issue",
			response: `{
				"number": 101,
				"html_url": "https://github.com/test-owner/test-repo/pull/101",
				"title": "Fix bug in parser"
			}`,
			statusCode: http.StatusCreated,
			wantNumber: 101,
			wantURL:    "https://github.com/test-owner/test-repo/pull/101",
		},
		{
			name:       "validation error",
			base:       "main",
			head:       "main",
			title:      "Bad PR",
			body:       "",
			response:   `{"message": "Validation Failed"}`,
			statusCode: http.StatusUnprocessableEntity,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedPayload struct {
				Title string `json:"title"`
				Body  string `json:"body"`
				Head  string `json:"head"`
				Base  string `json:"base"`
			}

			mux := http.NewServeMux()
			mux.HandleFunc("/repos/test-owner/test-repo/pulls", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %s, want POST", r.Method)
				}
				json.NewDecoder(r.Body).Decode(&receivedPayload)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				fmt.Fprint(w, tt.response)
			})

			adapter, _ := newTestGitHub(t, mux)

			pr, err := adapter.CreatePR(context.Background(), tt.base, tt.head, tt.title, tt.body)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if pr.Number != tt.wantNumber {
				t.Errorf("number = %d, want %d", pr.Number, tt.wantNumber)
			}
			if pr.URL != tt.wantURL {
				t.Errorf("url = %q, want %q", pr.URL, tt.wantURL)
			}
			if pr.Title != tt.title {
				t.Errorf("title = %q, want %q", pr.Title, tt.title)
			}

			// Verify the payload sent to the API
			if receivedPayload.Base != tt.base {
				t.Errorf("payload base = %q, want %q", receivedPayload.Base, tt.base)
			}
			if receivedPayload.Head != tt.head {
				t.Errorf("payload head = %q, want %q", receivedPayload.Head, tt.head)
			}
		})
	}
}

// --- ParseWebhook tests ---

func TestGitHubParseWebhook(t *testing.T) {
	secret := "my-webhook-secret"

	issuePayload := `{
		"action": "opened",
		"issue": {
			"id": 500,
			"number": 7,
			"title": "New feature request",
			"body": "Please add dark mode",
			"created_at": "2025-02-01T12:00:00Z",
			"user": {"login": "contributor"},
			"labels": [{"name": "enhancement"}]
		}
	}`

	makeSignature := func(body []byte, sec string) string {
		mac := hmac.New(sha256.New, []byte(sec))
		mac.Write(body)
		return "sha256=" + hex.EncodeToString(mac.Sum(nil))
	}

	tests := []struct {
		name      string
		body      string
		signature string
		secret    string
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "valid signature",
			body:      issuePayload,
			signature: makeSignature([]byte(issuePayload), secret),
			secret:    secret,
		},
		{
			name:      "invalid signature",
			body:      issuePayload,
			signature: "sha256=deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
			secret:    secret,
			wantErr:   true,
			errMsg:    "signature mismatch",
		},
		{
			name:      "missing sha256 prefix",
			body:      issuePayload,
			signature: "invalid-format",
			secret:    secret,
			wantErr:   true,
			errMsg:    "invalid signature format",
		},
		{
			name:      "invalid hex",
			body:      issuePayload,
			signature: "sha256=not-valid-hex",
			secret:    secret,
			wantErr:   true,
			errMsg:    "decode signature hex",
		},
		{
			name:      "no secret configured (skip verification)",
			body:      issuePayload,
			signature: "",
			secret:    "",
		},
		{
			name:      "invalid json",
			body:      `{not json`,
			signature: makeSignature([]byte(`{not json`), secret),
			secret:    secret,
			wantErr:   true,
			errMsg:    "parse webhook payload",
		},
		{
			name:      "payload without issue",
			body:      `{"action": "push"}`,
			signature: makeSignature([]byte(`{"action": "push"}`), secret),
			secret:    secret,
			wantErr:   true,
			errMsg:    "does not contain an issue",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := &GitHubAdapter{secret: tt.secret}

			issue, err := adapter.ParseWebhook([]byte(tt.body), tt.signature)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tt.errMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if issue.Number != 7 {
				t.Errorf("number = %d, want 7", issue.Number)
			}
			if issue.Title != "New feature request" {
				t.Errorf("title = %q, want %q", issue.Title, "New feature request")
			}
			if issue.Body != "Please add dark mode" {
				t.Errorf("body = %q, want %q", issue.Body, "Please add dark mode")
			}
			if issue.Author != "contributor" {
				t.Errorf("author = %q, want %q", issue.Author, "contributor")
			}
			if len(issue.Labels) != 1 || issue.Labels[0] != "enhancement" {
				t.Errorf("labels = %v, want [enhancement]", issue.Labels)
			}
			if issue.ID != "500" {
				t.Errorf("id = %q, want %q", issue.ID, "500")
			}
		})
	}
}

// --- Local git operation tests ---

// initBareRepo creates a bare git repo and a working clone in a temp dir.
// Returns the workspace (clone) path and the bare repo path.
func initBareRepo(t *testing.T) (string, string) {
	t.Helper()

	baseDir := t.TempDir()
	bareDir := filepath.Join(baseDir, "bare.git")
	workDir := filepath.Join(baseDir, "work")

	// Create bare repo
	run(t, baseDir, "git", "init", "--bare", bareDir)

	// Clone it
	run(t, baseDir, "git", "clone", bareDir, workDir)

	// Configure git user in the clone
	run(t, workDir, "git", "config", "user.email", "test@rig.dev")
	run(t, workDir, "git", "config", "user.name", "Rig Test")

	// Create initial commit so we have a branch to work from
	initialFile := filepath.Join(workDir, "README.md")
	if err := os.WriteFile(initialFile, []byte("# Test Repo\n"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	run(t, workDir, "git", "add", ".")
	run(t, workDir, "git", "commit", "-m", "initial commit")
	run(t, workDir, "git", "push", "origin", "HEAD")

	return workDir, bareDir
}

func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	c := exec.Command(name, args...)
	c.Dir = dir
	output, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("command %q failed: %v\noutput: %s", name+" "+strings.Join(args, " "), err, output)
	}
	return string(output)
}

func TestGitLocalCreateBranch(t *testing.T) {
	workDir, _ := initBareRepo(t)

	adapter := &GitHubAdapter{workspace: workDir}

	err := adapter.CreateBranch(context.Background(), "feature/test-branch")
	if err != nil {
		t.Fatalf("CreateBranch failed: %v", err)
	}

	// Verify we are on the new branch
	output := run(t, workDir, "git", "branch", "--show-current")
	branch := strings.TrimSpace(output)
	if branch != "feature/test-branch" {
		t.Errorf("current branch = %q, want %q", branch, "feature/test-branch")
	}
}

func TestGitLocalCommitAndPush(t *testing.T) {
	workDir, bareDir := initBareRepo(t)

	adapter := &GitHubAdapter{workspace: workDir}

	// Create a branch first
	err := adapter.CreateBranch(context.Background(), "feature/commit-test")
	if err != nil {
		t.Fatalf("CreateBranch failed: %v", err)
	}

	// Commit and push file changes
	changes := []core.GitFileChange{
		{Path: "src/main.go", Content: "package main\n\nfunc main() {}\n", Action: "create"},
		{Path: "docs/README.md", Content: "# Docs\n", Action: "create"},
	}

	err = adapter.CommitAndPush(context.Background(), changes, "feat: add main and docs")
	if err != nil {
		t.Fatalf("CommitAndPush failed: %v", err)
	}

	// Verify files exist in workspace
	for _, fc := range changes {
		absPath := filepath.Join(workDir, fc.Path)
		content, err := os.ReadFile(absPath)
		if err != nil {
			t.Errorf("file %q not found: %v", fc.Path, err)
			continue
		}
		if string(content) != fc.Content {
			t.Errorf("file %q content = %q, want %q", fc.Path, string(content), fc.Content)
		}
	}

	// Verify commit exists in the bare repo
	cloneDir := t.TempDir()
	c := exec.Command("git", "clone", bareDir, cloneDir)
	if output, err := c.CombinedOutput(); err != nil {
		t.Fatalf("clone bare repo: %v\noutput: %s", err, output)
	}

	// Check that the branch exists in the bare repo
	output := run(t, cloneDir, "git", "branch", "-a")
	if !strings.Contains(output, "feature/commit-test") {
		t.Errorf("branch feature/commit-test not found in bare repo, branches: %s", output)
	}
}

func TestGitLocalCommitAndPushDelete(t *testing.T) {
	workDir, _ := initBareRepo(t)

	adapter := &GitHubAdapter{workspace: workDir}

	// Create a branch
	err := adapter.CreateBranch(context.Background(), "feature/delete-test")
	if err != nil {
		t.Fatalf("CreateBranch failed: %v", err)
	}

	// First create a file
	changes := []core.GitFileChange{
		{Path: "to-delete.txt", Content: "temporary content", Action: "create"},
	}
	err = adapter.CommitAndPush(context.Background(), changes, "add file to delete")
	if err != nil {
		t.Fatalf("CommitAndPush (create) failed: %v", err)
	}

	// Now delete it
	deleteChanges := []core.GitFileChange{
		{Path: "to-delete.txt", Action: "delete"},
	}
	err = adapter.CommitAndPush(context.Background(), deleteChanges, "delete file")
	if err != nil {
		t.Fatalf("CommitAndPush (delete) failed: %v", err)
	}

	// Verify file is gone
	absPath := filepath.Join(workDir, "to-delete.txt")
	if _, err := os.Stat(absPath); !os.IsNotExist(err) {
		t.Errorf("expected file to be deleted, but it still exists")
	}
}

func TestGitLocalCommitAndPushInvalidAction(t *testing.T) {
	workDir, _ := initBareRepo(t)

	adapter := &GitHubAdapter{workspace: workDir}

	err := adapter.CreateBranch(context.Background(), "feature/invalid-action")
	if err != nil {
		t.Fatalf("CreateBranch failed: %v", err)
	}

	changes := []core.GitFileChange{
		{Path: "test.txt", Content: "data", Action: "unknown"},
	}
	err = adapter.CommitAndPush(context.Background(), changes, "bad action")
	if err == nil {
		t.Fatal("expected error for unknown action, got nil")
	}
	if !strings.Contains(err.Error(), "unknown file action") {
		t.Errorf("error = %q, want to contain 'unknown file action'", err.Error())
	}
}

func TestGitLocalCloneOrPull(t *testing.T) {
	baseDir := t.TempDir()
	bareDir := filepath.Join(baseDir, "origin.git")
	workDir := filepath.Join(baseDir, "clone-target")

	// Create a bare repo with an initial commit
	run(t, baseDir, "git", "init", "--bare", bareDir)

	// Create a temp clone to push initial content
	tmpClone := filepath.Join(baseDir, "tmp-clone")
	run(t, baseDir, "git", "clone", bareDir, tmpClone)
	run(t, tmpClone, "git", "config", "user.email", "test@rig.dev")
	run(t, tmpClone, "git", "config", "user.name", "Rig Test")
	if err := os.WriteFile(filepath.Join(tmpClone, "init.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	run(t, tmpClone, "git", "add", ".")
	run(t, tmpClone, "git", "commit", "-m", "init")
	run(t, tmpClone, "git", "push", "origin", "HEAD")

	// Use the adapter to clone (we override workspace and use local bare repo URL)
	adapter := &GitHubAdapter{workspace: workDir}

	// Manually clone since CloneOrPull uses github.com URL format.
	// We test the pull path by using gitCmd directly.
	c := exec.Command("git", "clone", bareDir, workDir)
	if output, err := c.CombinedOutput(); err != nil {
		t.Fatalf("manual clone: %v\noutput: %s", err, output)
	}

	// Verify initial file
	content, err := os.ReadFile(filepath.Join(workDir, "init.txt"))
	if err != nil {
		t.Fatalf("read file after clone: %v", err)
	}
	if string(content) != "hello" {
		t.Errorf("content = %q, want %q", string(content), "hello")
	}

	// Push a new commit to the bare repo via tmpClone
	if err := os.WriteFile(filepath.Join(tmpClone, "second.txt"), []byte("world"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	run(t, tmpClone, "git", "add", ".")
	run(t, tmpClone, "git", "commit", "-m", "second commit")
	run(t, tmpClone, "git", "push", "origin", "HEAD")

	// Now use gitCmd to pull (simulating CloneOrPull's pull path)
	_, err = adapter.gitCmd(context.Background(), "pull", "--ff-only")
	if err != nil {
		t.Fatalf("git pull failed: %v", err)
	}

	// Verify new file appeared
	content, err = os.ReadFile(filepath.Join(workDir, "second.txt"))
	if err != nil {
		t.Fatalf("read file after pull: %v", err)
	}
	if string(content) != "world" {
		t.Errorf("content = %q, want %q", string(content), "world")
	}
}

// --- NewGitHub constructor test ---

func TestNewGitHub(t *testing.T) {
	adapter, err := NewGitHub("owner", "repo", "token", "secret", "")
	if err != nil {
		t.Fatalf("NewGitHub failed: %v", err)
	}

	if adapter.owner != "owner" {
		t.Errorf("owner = %q, want %q", adapter.owner, "owner")
	}
	if adapter.repo != "repo" {
		t.Errorf("repo = %q, want %q", adapter.repo, "repo")
	}
	if adapter.token != "token" {
		t.Errorf("token = %q, want %q", adapter.token, "token")
	}
	if adapter.secret != "secret" {
		t.Errorf("secret = %q, want %q", adapter.secret, "secret")
	}

	home, _ := os.UserHomeDir()
	expectedWorkspace := filepath.Join(home, ".rig", "workspaces", "owner", "repo")
	if adapter.workspace != expectedWorkspace {
		t.Errorf("workspace = %q, want %q", adapter.workspace, expectedWorkspace)
	}
}

func TestNewGitHubEnterprise(t *testing.T) {
	server := httptest.NewServer(http.NewServeMux())
	t.Cleanup(server.Close)

	adapter, err := NewGitHub("owner", "repo", "token", "secret", server.URL)
	if err != nil {
		t.Fatalf("NewGitHub with enterprise URL failed: %v", err)
	}

	if adapter.client == nil {
		t.Fatal("client should not be nil")
	}
}

// --- Verify interface compliance ---

var _ core.GitAdapter = (*GitHubAdapter)(nil)

// --- Verify verifySignature directly ---

func TestVerifySignature(t *testing.T) {
	secret := "super-secret"
	body := []byte("test payload data")

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	tests := []struct {
		name    string
		sig     string
		wantErr bool
	}{
		{name: "valid", sig: validSig},
		{name: "wrong signature", sig: "sha256=0000000000000000000000000000000000000000000000000000000000000000", wantErr: true},
		{name: "no prefix", sig: "abc123", wantErr: true},
		{name: "bad hex", sig: "sha256=xyz", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifySignature(body, tt.sig, secret)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// --- gitCmd timeout test ---

func TestGitCmdTimeout(t *testing.T) {
	workDir, _ := initBareRepo(t)
	adapter := &GitHubAdapter{workspace: workDir}

	// Use an already-cancelled context to trigger immediate timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	time.Sleep(5 * time.Millisecond) // ensure context is already expired

	_, err := adapter.gitCmd(ctx, "status")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}
