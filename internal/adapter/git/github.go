package git

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/go-github/v60/github"
)

// GitHubAdapter implements GitAdapter using the GitHub REST API and local git CLI.
type GitHubAdapter struct {
	client    *github.Client
	owner     string
	repo      string
	token     string
	secret    string // webhook secret for HMAC verification
	workspace string // local workspace path
}

// NewGitHub creates a new GitHubAdapter.
// baseURL can be empty for github.com or a custom URL for GitHub Enterprise.
func NewGitHub(owner, repo, token, secret, baseURL string) (*GitHubAdapter, error) {
	client := github.NewClient(nil).WithAuthToken(token)

	if baseURL != "" {
		var err error
		client, err = github.NewClient(nil).WithAuthToken(token).WithEnterpriseURLs(baseURL, baseURL)
		if err != nil {
			return nil, fmt.Errorf("create github enterprise client: %w", err)
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get user home dir: %w", err)
	}
	workspace := filepath.Join(home, ".rig", "workspaces", owner, repo)

	return &GitHubAdapter{
		client:    client,
		owner:     owner,
		repo:      repo,
		token:     token,
		secret:    secret,
		workspace: workspace,
	}, nil
}

// ParseWebhook validates the HMAC-SHA256 signature and parses the webhook payload as an issue event.
func (g *GitHubAdapter) ParseWebhook(body []byte, signature string) (*Issue, error) {
	if g.secret != "" {
		if err := verifySignature(body, signature, g.secret); err != nil {
			return nil, err
		}
	}

	// Parse as issue event payload.
	var payload struct {
		Action string `json:"action"`
		Issue  struct {
			ID        int64  `json:"id"`
			Number    int    `json:"number"`
			Title     string `json:"title"`
			Body      string `json:"body"`
			CreatedAt string `json:"created_at"`
			User      struct {
				Login string `json:"login"`
			} `json:"user"`
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
		} `json:"issue"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse webhook payload: %w", err)
	}

	if payload.Issue.Number == 0 {
		return nil, fmt.Errorf("webhook payload does not contain an issue")
	}

	labels := make([]string, 0, len(payload.Issue.Labels))
	for _, l := range payload.Issue.Labels {
		labels = append(labels, l.Name)
	}

	createdAt, _ := time.Parse(time.RFC3339, payload.Issue.CreatedAt)

	return &Issue{
		ID:        fmt.Sprintf("%d", payload.Issue.ID),
		Number:    payload.Issue.Number,
		Title:     payload.Issue.Title,
		Body:      payload.Issue.Body,
		Labels:    labels,
		Author:    payload.Issue.User.Login,
		CreatedAt: createdAt,
	}, nil
}

// verifySignature checks the HMAC-SHA256 signature of a webhook payload.
func verifySignature(body []byte, signature, secret string) error {
	// GitHub sends the signature as "sha256=<hex>"
	sig := strings.TrimPrefix(signature, "sha256=")
	if sig == signature {
		return fmt.Errorf("invalid signature format: expected sha256= prefix")
	}

	decoded, err := hex.DecodeString(sig)
	if err != nil {
		return fmt.Errorf("decode signature hex: %w", err)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)

	if !hmac.Equal(decoded, expected) {
		return fmt.Errorf("webhook signature mismatch")
	}

	return nil
}

// GetIssue retrieves a single issue from the GitHub API.
func (g *GitHubAdapter) GetIssue(ctx context.Context, owner, repo string, number int) (*Issue, error) {
	issue, _, err := g.client.Issues.Get(ctx, owner, repo, number)
	if err != nil {
		return nil, fmt.Errorf("get issue #%d: %w", number, err)
	}

	labels := make([]string, 0, len(issue.Labels))
	for _, l := range issue.Labels {
		labels = append(labels, l.GetName())
	}

	return &Issue{
		ID:        fmt.Sprintf("%d", issue.GetID()),
		Number:    issue.GetNumber(),
		Title:     issue.GetTitle(),
		Body:      issue.GetBody(),
		Labels:    labels,
		Author:    issue.GetUser().GetLogin(),
		CreatedAt: issue.GetCreatedAt().Time,
	}, nil
}

// PostComment posts a comment on an issue or pull request.
func (g *GitHubAdapter) PostComment(ctx context.Context, owner, repo string, number int, body string) error {
	comment := &github.IssueComment{
		Body: github.String(body),
	}
	_, _, err := g.client.Issues.CreateComment(ctx, owner, repo, number, comment)
	if err != nil {
		return fmt.Errorf("post comment on #%d: %w", number, err)
	}
	return nil
}

// CreateBranch creates a new git branch in the local workspace.
func (g *GitHubAdapter) CreateBranch(ctx context.Context, branchName string) error {
	if _, err := g.gitCmd(ctx, "checkout", "-b", branchName); err != nil {
		return fmt.Errorf("create branch %q: %w", branchName, err)
	}
	return nil
}

// CommitAndPush stages file changes, commits, and pushes to the remote.
func (g *GitHubAdapter) CommitAndPush(ctx context.Context, changes []FileChange, message string) error {
	for _, change := range changes {
		absPath := filepath.Join(g.workspace, change.Path)

		switch change.Action {
		case "create", "update":
			dir := filepath.Dir(absPath)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("create directory for %q: %w", change.Path, err)
			}
			if err := os.WriteFile(absPath, []byte(change.Content), 0o644); err != nil {
				return fmt.Errorf("write file %q: %w", change.Path, err)
			}
			if _, err := g.gitCmd(ctx, "add", change.Path); err != nil {
				return fmt.Errorf("git add %q: %w", change.Path, err)
			}
		case "delete":
			if _, err := g.gitCmd(ctx, "rm", "-f", change.Path); err != nil {
				return fmt.Errorf("git rm %q: %w", change.Path, err)
			}
		default:
			return fmt.Errorf("unknown file action %q for %q", change.Action, change.Path)
		}
	}

	if _, err := g.gitCmd(ctx, "commit", "-m", message); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	if _, err := g.gitCmd(ctx, "push", "origin", "HEAD"); err != nil {
		return fmt.Errorf("git push: %w", err)
	}

	return nil
}

// CreatePR creates a pull request on the remote repository.
func (g *GitHubAdapter) CreatePR(ctx context.Context, base, head, title, body string) (*PullRequest, error) {
	pr := &github.NewPullRequest{
		Title: github.String(title),
		Body:  github.String(body),
		Head:  github.String(head),
		Base:  github.String(base),
	}

	created, _, err := g.client.PullRequests.Create(ctx, g.owner, g.repo, pr)
	if err != nil {
		return nil, fmt.Errorf("create pull request: %w", err)
	}

	return &PullRequest{
		Number: created.GetNumber(),
		URL:    created.GetHTMLURL(),
		Title:  created.GetTitle(),
	}, nil
}

// CloneOrPull clones a repository or pulls latest if already cloned.
func (g *GitHubAdapter) CloneOrPull(ctx context.Context, owner, repo, token string) error {
	if err := os.MkdirAll(filepath.Dir(g.workspace), 0o755); err != nil {
		return fmt.Errorf("create workspace parent dir: %w", err)
	}

	cloneURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s/%s.git", token, owner, repo)

	// Check if workspace already exists with a .git directory.
	gitDir := filepath.Join(g.workspace, ".git")
	if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
		// Already cloned — pull latest.
		if _, err := g.gitCmd(ctx, "pull", "--ff-only"); err != nil {
			return fmt.Errorf("git pull: %w", err)
		}
		return nil
	}

	// Clone fresh.
	c := exec.CommandContext(ctx, "git", "clone", cloneURL, g.workspace)
	c.WaitDelay = 500 * time.Millisecond
	c.Cancel = func() error {
		return c.Process.Kill()
	}

	output, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone: %w (output: %s)", err, string(output))
	}

	return nil
}

// gitCmd runs a git command in the workspace directory.
func (g *GitHubAdapter) gitCmd(ctx context.Context, args ...string) (string, error) {
	c := exec.CommandContext(ctx, "git", args...)
	c.Dir = g.workspace
	c.WaitDelay = 500 * time.Millisecond
	c.Cancel = func() error {
		return c.Process.Kill()
	}

	output, err := c.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w (output: %s)", strings.Join(args, " "), err, string(output))
	}

	return string(output), nil
}
