package git

import (
	"context"
	"time"

	"github.com/rigdev/rig/internal/core"
)

// WebhookGitAdapter includes webhook-specific Git operations.
// Concrete implementations should also satisfy core.GitAdapter.
type WebhookGitAdapter interface {
	// ParseWebhook validates and parses a webhook payload with HMAC-SHA256 signature verification.
	ParseWebhook(body []byte, signature string) (*Issue, error)

	// GetIssue retrieves a single issue by number from the repository.
	GetIssue(ctx context.Context, owner, repo string, number int) (*Issue, error)

	// PostComment posts a comment on an issue or pull request.
	PostComment(ctx context.Context, owner, repo string, number int, body string) error

	core.GitAdapter
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
