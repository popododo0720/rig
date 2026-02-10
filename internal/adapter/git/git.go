package git

import (
	"context"
	"time"
)

// GitAdapter defines the interface for source code management operations.
type GitAdapter interface {
	// ParseWebhook validates and parses a webhook payload with HMAC-SHA256 signature verification.
	ParseWebhook(body []byte, signature string) (*Issue, error)

	// GetIssue retrieves a single issue by number from the repository.
	GetIssue(ctx context.Context, owner, repo string, number int) (*Issue, error)

	// PostComment posts a comment on an issue or pull request.
	PostComment(ctx context.Context, owner, repo string, number int, body string) error

	// CreateBranch creates a new git branch in the local workspace.
	CreateBranch(ctx context.Context, branchName string) error

	// CommitAndPush stages file changes, commits, and pushes to the remote.
	CommitAndPush(ctx context.Context, changes []FileChange, message string) error

	// CreatePR creates a pull request on the remote repository.
	CreatePR(ctx context.Context, base, head, title, body string) (*PullRequest, error)

	// CloneOrPull clones a repository or pulls latest changes if already cloned.
	CloneOrPull(ctx context.Context, owner, repo, token string) error
}

// Issue represents a GitHub issue.
type Issue struct {
	ID        string
	Number    int
	Title     string
	Body      string
	Labels    []string
	Author    string
	CreatedAt time.Time
}

// FileChange represents a file modification to be committed.
type FileChange struct {
	Path    string
	Content string
	Action  string // "create", "update", "delete"
}

// PullRequest represents a created pull request.
type PullRequest struct {
	Number int
	URL    string
	Title  string
}
