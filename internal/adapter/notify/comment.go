package notify

import (
	"context"
	"fmt"

	"github.com/rigdev/rig/internal/adapter/git"
)

// CommentNotifier sends notifications as issue comments via GitAdapter.
type CommentNotifier struct {
	adapter git.GitAdapter
	owner   string
	repo    string
	number  int
}

// NewCommentNotifier creates a new CommentNotifier.
func NewCommentNotifier(adapter git.GitAdapter, owner, repo string, number int) *CommentNotifier {
	return &CommentNotifier{
		adapter: adapter,
		owner:   owner,
		repo:    repo,
		number:  number,
	}
}

// Notify posts a comment on the configured issue/PR.
func (c *CommentNotifier) Notify(ctx context.Context, message string) error {
	body := fmt.Sprintf("**[rig]** %s", message)
	return c.adapter.PostComment(ctx, c.owner, c.repo, c.number, body)
}
